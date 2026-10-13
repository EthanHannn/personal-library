import { onScopeDispose, shallowRef, toValue } from 'vue'
import type { MaybeRefOrGetter } from 'vue'
import { APIError, requestJSON } from '../api'

// A mutation is always an explicit desired state. The same PUT/DELETE is safe
// to retry if the server committed but the browser never received its response.
export function useShelfAction(bookId: MaybeRefOrGetter<number>) {
  const pending = shallowRef(false)
  const desired = shallowRef<boolean | null>(null)
  const error = shallowRef<APIError | null>(null)
  let controller: AbortController | null = null
  let disposed = false
  onScopeDispose(() => { disposed = true; controller?.abort() })

  async function set(added: boolean): Promise<boolean | null> {
    if (pending.value || disposed) return null
    desired.value = added; pending.value = true; error.value = null
    const request = new AbortController()
    controller = request
    try {
      const result = await requestJSON<{ inShelf: boolean }>(`/api/v1/books/${toValue(bookId)}/shelf`, {
        method: added ? 'PUT' : 'DELETE', headers: { 'Content-Type': 'application/json' }, body: '{}',
        signal: AbortSignal.any([request.signal, AbortSignal.timeout(10000)]),
      })
      return disposed ? null : result.inShelf
    } catch (cause) {
      if (!disposed) error.value = cause instanceof APIError ? cause : new APIError('未能确认书架是否已更新，请重试。', 0)
      return null
    } finally {
      if (!disposed) pending.value = false
    }
  }

  return { pending, desired, error, set }
}
