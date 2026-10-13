import { onMounted, onScopeDispose, shallowRef } from 'vue'
import type { ShallowRef } from 'vue'
import type { Paragraph } from '../reader/text'
import type { Preferences } from '../reader/themes'

export function speechChunks(text: string): string[] {
  const chars = Array.from(text)
  const chunks: string[] = []
  let current = ''
  let size = 0
  for (const ch of chars) {
    current += ch; size++
    if (size >= 180 || (size >= 80 && /[。！？!?；;]/.test(ch))) { chunks.push(current); current = ''; size = 0 }
  }
  if (current) chunks.push(current)
  return chunks
}

export function useReaderTts(paragraphs: ShallowRef<Paragraph[]>, preferences: ShallowRef<Preferences>, follow: (index: number) => void, continueBlock?: () => Promise<number | null>) {
  const supported = shallowRef(typeof speechSynthesis !== 'undefined' && typeof SpeechSynthesisUtterance !== 'undefined')
  const voices = shallowRef<SpeechSynthesisVoice[]>([])
  const status = shallowRef<'idle' | 'playing' | 'paused'>('idle')
  const currentIndex = shallowRef(0)
  const error = shallowRef('')
  let generation = 0
  let utterance: SpeechSynthesisUtterance | null = null
  let paragraphChunks: string[] = []
  let chunkIndex = 0
  let queuedNext: number | null = null

  function stop() {
    generation++; utterance = null; queuedNext = null
    if (supported.value) { speechSynthesis.cancel(); speechSynthesis.resume() }
    status.value = 'idle'
  }

  function speak(index: number, chunk: number, token: number) {
    if (token !== generation || !paragraphs.value[index]) { status.value = 'idle'; return }
    if (chunk === 0) paragraphChunks = speechChunks(paragraphs.value[index].text)
    chunkIndex = chunk
    currentIndex.value = index
    const next = new SpeechSynthesisUtterance(paragraphChunks[chunk])
    utterance = next
    const voice = voices.value.find(v => v.voiceURI === preferences.value.voice) ?? voices.value.find(v => /^zh[-_]/i.test(v.lang)) ?? voices.value[0]
    if (voice) next.voice = voice
    next.lang = voice?.lang ?? 'zh-CN'; next.rate = preferences.value.rate
    status.value = 'playing'
    next.onstart = () => { if (token === generation) follow(index) }
    next.onend = () => {
      if (token !== generation) return
      if (chunk + 1 < paragraphChunks.length) speak(index, chunk + 1, token)
      else if (index + 1 < paragraphs.value.length) speak(index + 1, 0, token)
      else {
        utterance = null
        if (!continueBlock) { status.value = 'idle'; return }
        void continueBlock().then(nextIndex => {
          if (token !== generation) return
          if (nextIndex !== null && status.value === 'paused') queuedNext = nextIndex
          else if (nextIndex !== null) speak(nextIndex, 0, token)
          else status.value = 'idle'
        }).catch(() => { if (token === generation) { status.value = 'idle'; error.value = '下一段暂时无法读取，请重试' } })
      }
    }
    next.onerror = event => {
      if (token !== generation || ['canceled', 'interrupted'].includes(event.error)) return
      status.value = 'idle'; error.value = '朗读未能继续，请检查设备音色后重试'
    }
    speechSynthesis.speak(next)
  }

  function start(index = currentIndex.value) {
    stop(); error.value = ''
    if (!supported.value || !paragraphs.value.length) return
    if (!voices.value.length) { error.value = '此设备尚未提供可用音色'; return }
    speak(Math.max(0, Math.min(index, paragraphs.value.length - 1)), 0, generation)
  }

  function toggle(startIndex?: number) {
    if (!supported.value) return
    if (status.value === 'playing') { speechSynthesis.pause(); status.value = 'paused' }
    else if (status.value === 'paused') {
      if (!utterance) {
        status.value = 'playing'; speechSynthesis.resume()
        if (queuedNext !== null) { const index = queuedNext; queuedNext = null; speak(index, 0, generation) }
      } else if (speechSynthesis.paused) { speechSynthesis.resume(); status.value = 'playing' }
      else { const index = currentIndex.value; const chunk = chunkIndex; stop(); speak(index, chunk, generation) }
    } else start(startIndex)
  }

  function refresh() { if (supported.value) voices.value = speechSynthesis.getVoices() }
  function background() { if (document.hidden && status.value === 'playing') { speechSynthesis.pause(); status.value = 'paused' } }
  onMounted(() => {
    refresh()
    if (supported.value) speechSynthesis.addEventListener('voiceschanged', refresh)
    document.addEventListener('visibilitychange', background)
    window.addEventListener('library:auth-lost', stop)
  })
  onScopeDispose(() => {
    stop()
    if (supported.value) speechSynthesis.removeEventListener('voiceschanged', refresh)
    document.removeEventListener('visibilitychange', background)
    window.removeEventListener('library:auth-lost', stop)
  })
  return { supported, voices, status, currentIndex, error, start, stop, toggle }
}
