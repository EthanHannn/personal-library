import { effectScope } from 'vue'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { useReadingProgress } from './useReadingProgress'
import { claim, prepareMutation, saveDraft } from '../reader/localProgress'
import { deferred, position, response } from '../test/fixtures'
import type { Mutation } from '../api'

const scopes: ReturnType<typeof effectScope>[] = []
function setup(book: number) { const scope = effectScope(); scopes.push(scope); return scope.run(() => useReadingProgress(book))! }
afterEach(() => { for (const scope of scopes) scope.stop(); scopes.length = 0 })

describe('reading progress recovery', () => {
  it('does not save a zero position before restoration and keeps an offline mutation for retry', async () => {
    const fetch = vi.fn().mockRejectedValue(new TypeError('offline')); vi.stubGlobal('fetch', fetch)
    const p = setup(201)
    await p.start({ ...position(3, 0.8), revision: 4, updatedAt: 1 })
    p.capture(position(1, 0)); await p.flush()
    expect(fetch).not.toHaveBeenCalled()
    p.restored(position(3, 0.8)); p.capture(position(3, 0.9)); await p.flush()
    const request = JSON.parse(fetch.mock.calls[0][1].body) as Mutation
    expect(request.chapterIdx).toBe(3); expect(request.baseRevision).toBe(4)
    expect(p.message.value).toContain('本机位置已保留')
    fetch.mockResolvedValue(response({ progress: { ...position(3, 0.9), revision: 5, updatedAt: 2 } }))
    await p.flush()
    expect(JSON.parse(fetch.mock.calls[1][1].body)).toEqual(request)
    expect(p.local.value?.pending).toBeNull()
  })

  it('resolves a lost acknowledgement using the original mutation before comparing revisions', async () => {
    await claim(202, 'previous'); await saveDraft(202, 'previous', position(2))
    const pending = (await prepareMutation(202, 'previous')).pending!
    const fetch = vi.fn().mockResolvedValue(response({ progress: { ...position(2), revision: 1, updatedAt: 10 } })); vi.stubGlobal('fetch', fetch)
    const p = setup(202)
    const restored = await p.start({ ...position(2), revision: 1, updatedAt: 10 })
    expect(JSON.parse(fetch.mock.calls[0][1].body)).toEqual(pending)
    expect(p.conflict.value).toBeNull()
    expect(restored?.chapterIdx).toBe(2)
    expect(p.local.value?.pending).toBeNull()
  })

  it('shows both positions on conflict and rebases only after the user chooses local', async () => {
    const remote = { ...position(1, 0.1), revision: 9, updatedAt: 123 }
    const fetch = vi.fn().mockResolvedValue(response({ error: { code: 'progress_conflict', message: '选择位置' }, progress: remote }, 409)); vi.stubGlobal('fetch', fetch)
    const p = setup(203); await p.start(null); p.restored(position(3)); await p.flush()
    expect(p.conflict.value?.remote).toEqual(remote)
    expect(p.conflict.value?.local.chapterIdx).toBe(3)
    const previous = JSON.parse(fetch.mock.calls[0][1].body)
    await p.flush(); expect(fetch).toHaveBeenCalledTimes(1)
    await p.choose('local')
    fetch.mockResolvedValue(response({ progress: { ...position(3), revision: 10, updatedAt: 124 } }))
    await p.flush()
    const chosen = JSON.parse(fetch.mock.calls[1][1].body)
    expect(chosen.baseRevision).toBe(9); expect(chosen.chapterIdx).toBe(3)
    expect(chosen.mutationId).not.toBe(previous.mutationId)
  })

  it('retains pending data after 401 and never sends again until a new session is initialized', async () => {
    const fetch = vi.fn().mockResolvedValue(response({ error: { message: '请重新登录' } }, 401)); vi.stubGlobal('fetch', fetch)
    const p = setup(204); await p.start(null); p.restored(position(2)); await p.flush()
    expect(p.expired.value).toBe(true); expect(p.local.value?.pending?.chapterIdx).toBe(2)
    await p.flush(); expect(fetch).toHaveBeenCalledTimes(1)
  })

  it('keeps scrolling changes made while a request is in flight', async () => {
    const pending = deferred<Response>(); const fetch = vi.fn().mockReturnValue(pending.promise); vi.stubGlobal('fetch', fetch)
    const p = setup(205); await p.start(null); p.restored(position(1))
    const save = p.flush()
    await vi.waitFor(() => expect(fetch).toHaveBeenCalledOnce())
    p.capture(position(2)); await p.persist()
    pending.resolve(response({ progress: { ...position(1), revision: 1, updatedAt: 1 } })); await save
    expect(p.local.value?.draft?.chapterIdx).toBe(2)
  })

  it('requires a choice for legacy pending data when server progress exists', async () => {
    localStorage.setItem('zx_reading_206', JSON.stringify({ chapterIdx: 2, percent: 0.9, pending: true }))
    const p = setup(206)
    await p.start({ ...position(1), revision: 0, updatedAt: 1 })
    expect(p.conflict.value?.local.chapterIdx).toBe(2)
    expect(p.ready.value).toBe(false)
  })

  it('reports unavailable local storage without claiming that an unsaved position is safe', async () => {
    const fetch = vi.fn(); vi.stubGlobal('fetch', fetch)
    vi.spyOn(IDBDatabase.prototype, 'transaction').mockImplementation(() => { throw new DOMException('Storage blocked', 'SecurityError') })
    const p = setup(207)
    await p.start({ ...position(2), revision: 7, updatedAt: 1 })
    p.restored(position(2)); p.capture(position(3)); await p.flush()
    expect(p.writable.value).toBe(false)
    expect(p.message.value).toContain('无法在本机暂存')
    expect(fetch).not.toHaveBeenCalled()
  })
})
