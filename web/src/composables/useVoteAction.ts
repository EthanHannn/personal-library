import { onScopeDispose, shallowRef, toValue, watch } from 'vue'
import type { MaybeRefOrGetter } from 'vue'
import { APIError, requestJSON } from '../api'
import type { VoteSummary, VoteType } from '../api'

export function useVoteAction(bookId: MaybeRefOrGetter<number>) {
  const pending = shallowRef(false)
  const desired = shallowRef<VoteType | null>(null)
  const error = shallowRef<APIError | null>(null)
  let controller: AbortController | null = null
  let disposed = false

  function cancel() { controller?.abort(); pending.value = false }
  function authLost() {
    cancel()
    error.value = new APIError('登录已失效，请重新登录。', 401)
  }
  window.addEventListener('library:auth-lost', authLost)
  onScopeDispose(() => {
    disposed = true; cancel()
    window.removeEventListener('library:auth-lost', authLost)
  })
  watch(() => toValue(bookId), () => { cancel(); desired.value = null; error.value = null }, { flush: 'sync' })

  async function set(type: VoteType): Promise<VoteSummary | null> {
    if (pending.value || disposed) return null
    const id = toValue(bookId)
    desired.value = type; pending.value = true; error.value = null
    const request = new AbortController()
    controller = request
    try {
      const result = await requestJSON<VoteSummary>(`/api/v1/books/${id}/vote`, {
        method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ type }),
        signal: AbortSignal.any([request.signal, AbortSignal.timeout(10000)]),
      })
      return disposed || request.signal.aborted || id !== toValue(bookId) ? null : result
    } catch (cause) {
      if (!disposed && !request.signal.aborted) {
        error.value = cause instanceof APIError ? cause : new APIError('未能确认评价是否已保存，请重试。', 0)
      }
      return null
    } finally {
      if (!disposed && controller === request) pending.value = false
    }
  }

  return { pending, desired, error, set }
}
