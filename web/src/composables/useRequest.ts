import { onScopeDispose, shallowRef, toValue, watch } from 'vue'
import type { MaybeRefOrGetter } from 'vue'
import { APIError, getJSON } from '../api'

// The watcher owns cancellation. Even if an old response has already resolved,
// an aborted request cannot replace a newer route's data or loading state.
export function useRequest<T>(url: MaybeRefOrGetter<string>) {
  const data = shallowRef<T | null>(null)
  const error = shallowRef<APIError | null>(null)
  const pending = shallowRef(true)
  const attempt = shallowRef(0)
  let active: AbortController | null = null

  function authLost() {
    active?.abort()
    data.value = null
    pending.value = false
    error.value = new APIError('登录已过期，请重新登录后继续。', 401)
  }
  window.addEventListener('library:auth-lost', authLost)
  onScopeDispose(() => { active?.abort(); window.removeEventListener('library:auth-lost', authLost) })

  watch([() => toValue(url), attempt], async ([path], _previous, onCleanup) => {
    const controller = new AbortController()
    active = controller
    onCleanup(() => controller.abort())
    data.value = null
    error.value = null
    pending.value = true
    try {
      const result = await getJSON<T>(path, controller.signal)
      if (!controller.signal.aborted) data.value = result
    } catch (cause) {
      if (!controller.signal.aborted) {
        error.value = cause instanceof APIError ? cause : new APIError('连接中断，请检查网络后重试。', 0)
      }
    } finally {
      if (!controller.signal.aborted) pending.value = false
    }
  }, { immediate: true })

  return { data, error, pending, reload: () => { attempt.value++ } }
}
