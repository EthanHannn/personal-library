import { onScopeDispose, shallowRef } from 'vue'
import { APIError, getJSON } from '../api'
import type { ContentBlock, Position, ReadingState } from '../api'

const CACHE_BYTES = 2 * 1024 * 1024
const WINDOW_BYTES = 1536 * 1024

export function useChapterLoader(bookId: number) {
  const state = shallowRef<ReadingState | null>(null)
  const blocks = shallowRef<ContentBlock[]>([])
  const loading = shallowRef(false)
  const error = shallowRef('')
  const cache = new Map<number, ReadingState>()
  let controller: AbortController | null = null
  let prefetchController: AbortController | null = null
  let generation = 0
  let disposed = false
  let prefetchTimer = 0
  const bytes = (value: ReadingState) => (value.content?.text.length ?? 0) * 2

  function remember(value: ReadingState) {
    if (!value.content) return
    cache.delete(value.content.chapter.idx); cache.set(value.content.chapter.idx, value)
    while (cache.size > 3 || [...cache.values()].reduce((n, v) => n + bytes(v), 0) > CACHE_BYTES - blocks.value.reduce((n, b) => n + b.text.length * 2, 0)) {
      const first = cache.keys().next().value
      if (first === undefined) break
      cache.delete(first)
    }
  }

  function pausePrefetch() {
    clearTimeout(prefetchTimer); prefetchController?.abort(); prefetchController = null
  }

  function prefetch() {
    pausePrefetch()
    if (disposed) return
    const next = state.value?.next?.idx
    const connection = (navigator as Navigator & { connection?: { saveData?: boolean; effectiveType?: string } }).connection
    if (!next || cache.has(next) || document.hidden || connection?.saveData || ['slow-2g', '2g'].includes(connection?.effectiveType ?? '')) return
    prefetchTimer = window.setTimeout(async () => {
      const ownGeneration = generation
      const ownController = new AbortController(); prefetchController = ownController
      try {
        const result = await getJSON<ReadingState>(`/api/v1/books/${bookId}/reader?chapter=${next}`, ownController.signal)
        if (!disposed && generation === ownGeneration && !ownController.signal.aborted) remember(result)
      } catch { /* Prefetch never replaces readable content or its error state. */ }
    }, 300)
  }

  async function load(chapter?: number, position?: Position | null, initial = false): Promise<ReadingState | null> {
    if (disposed) return null
    const ownGeneration = ++generation
    controller?.abort(); pausePrefetch()
    const ownController = new AbortController(); controller = ownController
    loading.value = true; error.value = ''
    try {
      const cached = chapter !== undefined && !position && !initial ? cache.get(chapter) : undefined
      const result = cached ?? await getJSON<ReadingState>(`/api/v1/books/${bookId}/reader${chapter !== undefined ? `?chapter=${chapter}` : ''}`, ownController.signal)
      let block = result.content
      if (position?.anchor && block && block.blockToken !== position.anchor.block) {
        block = await getJSON<ContentBlock>(`/api/v1/books/${bookId}/chapters/${position.chapterIdx}/content?cursor=${encodeURIComponent(position.anchor.block)}`, ownController.signal)
      }
      // Imported percentages are only an approximate fallback. Walk bounded
      // blocks without retaining skipped text; exact anchors need one request.
      if (position && !position.anchor && block) {
        while (block.nextCursor && (block.blockEnd - block.chapterStart) / (block.chapterEnd - block.chapterStart) < position.percent) {
          block = await getJSON<ContentBlock>(`/api/v1/books/${bookId}/chapters/${position.chapterIdx}/content?cursor=${encodeURIComponent(block.nextCursor)}`, ownController.signal)
        }
      }
      if (ownGeneration !== generation || disposed) return null
      state.value = { ...result, content: block }
      blocks.value = block ? [block] : []
      remember(state.value)
      return state.value
    } catch (cause) {
      if (ownGeneration === generation && !ownController.signal.aborted) error.value = cause instanceof APIError ? cause.message : '网络暂时不可用，请检查连接后重试'
      return null
    } finally { if (ownGeneration === generation) loading.value = false }
  }

  async function append(): Promise<boolean> {
    if (disposed) return false
    const last = blocks.value.at(-1)
    if (!last?.nextCursor || loading.value) return false
    // Replacing an oversized window requires a deliberate button action. Never
    // discard text while the reader has a live selection.
    const replace = blocks.value.reduce((n, b) => n + b.text.length * 2, 0) >= WINDOW_BYTES
    if (replace && window.getSelection()?.toString()) { error.value = '请先结束文本选择，再继续本章'; return false }
    const ownGeneration = generation
    controller?.abort(); pausePrefetch()
    const ownController = new AbortController(); controller = ownController
    loading.value = true; error.value = ''
    try {
      const block = await getJSON<ContentBlock>(`/api/v1/books/${bookId}/chapters/${last.chapter.idx}/content?cursor=${encodeURIComponent(last.nextCursor)}`, ownController.signal)
      if (ownGeneration !== generation || disposed) return false
      blocks.value = replace ? [block] : [...blocks.value, block]
      if (state.value) remember(state.value)
      return replace
    } catch (cause) {
      if (!ownController.signal.aborted) error.value = cause instanceof APIError ? cause.message : '网络暂时不可用，请检查连接后重试'
      return false
    } finally { if (ownGeneration === generation) loading.value = false }
  }

  function clear() {
    generation++; controller?.abort(); pausePrefetch(); cache.clear(); blocks.value = []; state.value = null; loading.value = false
  }
  window.addEventListener('library:auth-lost', clear)
  onScopeDispose(() => { disposed = true; clear(); window.removeEventListener('library:auth-lost', clear) })
  return { state, blocks, loading, error, load, append, prefetch, pausePrefetch, clear }
}
