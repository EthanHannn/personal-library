import { mount } from '@vue/test-utils'
import { computed, defineComponent, shallowRef } from 'vue'
import { expect, it, vi } from 'vitest'
import { useReaderTts } from './useReaderTts'
import { paragraphs } from '../reader/text'
import { content, deferred } from '../test/fixtures'
import { defaultPreferences } from '../reader/themes'

it('ignores stale speech callbacks, pauses/resumes, highlights paragraphs and releases the engine', () => {
  class Utterance { onstart?: () => void; onend?: () => void; onerror?: (event: { error: string }) => void; constructor(public text: string) {} }
  const engine = { cancel: vi.fn(), pause: vi.fn(), resume: vi.fn(), speak: vi.fn(), paused: true, getVoices: () => [{ voiceURI: 'zh-test', name: '中文', lang: 'zh-CN' }], addEventListener: vi.fn(), removeEventListener: vi.fn() }
  vi.stubGlobal('SpeechSynthesisUtterance', Utterance); vi.stubGlobal('speechSynthesis', engine)
  const follow = vi.fn()
  let tts!: ReturnType<typeof useReaderTts>
  const wrapper = mount(defineComponent({ setup() { tts = useReaderTts(computed(() => paragraphs(content())), shallowRef({ ...defaultPreferences }), follow); return () => null } }))
  tts.start(0)
  const first = engine.speak.mock.calls[0][0] as Utterance
  first.onstart?.(); expect(follow).toHaveBeenCalledWith(0)
  tts.toggle(); expect(tts.status.value).toBe('paused'); expect(engine.pause).toHaveBeenCalledOnce()
  tts.toggle(); expect(tts.status.value).toBe('playing')
  tts.start(1)
  const second = engine.speak.mock.calls[1][0] as Utterance
  first.onend?.(); expect(engine.speak).toHaveBeenCalledTimes(2)
  second.onstart?.(); expect(follow).toHaveBeenLastCalledWith(1)
  second.onerror?.({ error: 'audio-busy' })
  expect(tts.status.value).toBe('idle'); expect(tts.error.value).toContain('检查设备音色')
  window.dispatchEvent(new Event('library:auth-lost'))
  expect(tts.status.value).toBe('idle')
  second.onend?.(); expect(engine.speak).toHaveBeenCalledTimes(2)
  wrapper.unmount()
  expect(engine.removeEventListener).toHaveBeenCalledWith('voiceschanged', expect.any(Function))
})

it('reports missing device voices without starting a silent sequence', () => {
  const engine = { cancel: vi.fn(), resume: vi.fn(), speak: vi.fn(), getVoices: () => [], addEventListener: vi.fn(), removeEventListener: vi.fn() }
  vi.stubGlobal('SpeechSynthesisUtterance', class {}); vi.stubGlobal('speechSynthesis', engine)
  let tts!: ReturnType<typeof useReaderTts>
  const wrapper = mount(defineComponent({ setup() { tts = useReaderTts(computed(() => paragraphs(content())), shallowRef({ ...defaultPreferences }), () => {}); return () => null } }))
  tts.start(); expect(tts.error.value).toContain('可用音色'); expect(engine.speak).not.toHaveBeenCalled()
  wrapper.unmount()
})

it('continues at the next block and respects a pause while that block is loading', async () => {
  class Utterance { onend?: () => void; constructor(public text: string) {} }
  const engine = { cancel: vi.fn(), pause: vi.fn(), resume: vi.fn(), speak: vi.fn(), paused: true, getVoices: () => [{ voiceURI: 'zh', lang: 'zh-CN' }], addEventListener: vi.fn(), removeEventListener: vi.fn() }
  vi.stubGlobal('SpeechSynthesisUtterance', Utterance); vi.stubGlobal('speechSynthesis', engine)
  const more = deferred<number | null>()
  const list = shallowRef(paragraphs(content(1, '第一块')))
  let tts!: ReturnType<typeof useReaderTts>
  const wrapper = mount(defineComponent({ setup() { tts = useReaderTts(list, shallowRef({ ...defaultPreferences }), () => {}, () => more.promise); return () => null } }))
  tts.start(); (engine.speak.mock.calls[0][0] as Utterance).onend?.()
  tts.toggle(); expect(tts.status.value).toBe('paused')
  list.value = [...list.value, ...paragraphs(content(1, '第二块'))]
  more.resolve(1); await more.promise; await Promise.resolve()
  expect(engine.speak).toHaveBeenCalledOnce()
  tts.toggle(); expect((engine.speak.mock.calls[1][0] as Utterance).text).toBe('第二块')
  wrapper.unmount()
})
