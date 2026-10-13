import { effectScope } from 'vue'
import { afterEach, expect, it, vi } from 'vitest'
import { useChapterLoader } from './useChapterLoader'
import { content, deferred, reading, response } from '../test/fixtures'

const scopes: ReturnType<typeof effectScope>[] = []
function setup() { const scope = effectScope(); scopes.push(scope); return scope.run(() => useChapterLoader(42))! }
afterEach(() => { for (const scope of scopes) scope.stop(); scopes.length = 0 })

it('cancels old requests and ignores responses arriving after a later chapter', async () => {
  const first = deferred<Response>(); const second = deferred<Response>()
  const fetch = vi.fn().mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise); vi.stubGlobal('fetch', fetch)
  const loader = setup()
  const a = loader.load(1); const b = loader.load(2)
  expect(fetch.mock.calls[0][1].signal.aborted).toBe(true)
  second.resolve(response(reading(2))); await b
  first.resolve(response(reading(1))); await a
  expect(loader.state.value?.content?.chapter.idx).toBe(2)
})

it('keeps the current chapter on navigation errors and clears text on authentication loss', async () => {
  const fetch = vi.fn().mockResolvedValueOnce(response(reading(1))).mockRejectedValueOnce(new TypeError('offline')); vi.stubGlobal('fetch', fetch)
  const loader = setup(); await loader.load(1); await loader.load(2)
  expect(loader.blocks.value[0].chapter.idx).toBe(1)
  expect(loader.error.value).toContain('检查连接')
  window.dispatchEvent(new Event('library:auth-lost'))
  expect(loader.blocks.value).toEqual([]); expect(loader.state.value).toBeNull()
})

it('appends bounded windows and preserves selected text before replacing a full window', async () => {
  let offset = 0
  const make = () => ({ ...content(1, '汉'.repeat(87000)), blockStart: offset++, nextCursor: `next-${offset}` })
  const fetch = vi.fn().mockResolvedValueOnce(response({ ...reading(1), content: make() })).mockImplementation(async () => response(make())); vi.stubGlobal('fetch', fetch)
  const loader = setup(); await loader.load(1)
  for (let i = 0; i < 10; i++) await loader.append()
  expect(loader.blocks.value.reduce((n, b) => n + b.text.length * 2, 0)).toBeLessThanOrEqual(2 * 1024 * 1024)
  while (loader.blocks.value.reduce((n, b) => n + b.text.length * 2, 0) < 1536 * 1024) await loader.append()
  const old = loader.blocks.value
  vi.spyOn(window, 'getSelection').mockReturnValue({ toString: () => '正在选择的正文' } as Selection)
  await loader.append()
  expect(loader.blocks.value).toBe(old); expect(loader.error.value).toContain('结束文本选择')
})

it('restores a signed block directly and discards skipped blocks for legacy percentages', async () => {
  const fetch = vi.fn().mockResolvedValueOnce(response(reading(2))).mockResolvedValueOnce(response({ ...content(2), blockToken: 'saved-block' })); vi.stubGlobal('fetch', fetch)
  const loader = setup()
  await loader.load(2, { chapterIdx: 2, percent: 0.6, contentVersion: 'v'.repeat(64), anchor: { block: 'saved-block', paragraph: 4, character: 0 } })
  expect(String(fetch.mock.calls[1][0])).toContain('cursor=saved-block')
  expect(loader.blocks.value[0].blockToken).toBe('saved-block')
})
