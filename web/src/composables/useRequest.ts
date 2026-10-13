import { shallowRef, toValue, watch } from 'vue'
import type { MaybeRefOrGetter } from 'vue'
import { APIError, getJSON } from '../api'

// The watcher owns cancellation. Even if an old response has already resolved,
// an aborted request cannot replace a newer route's data or loading state.
export function useRequest<T>(url: MaybeRefOrGetter<string>) {
  const data = shallowRef<T | null>(null)
  const error = shallowRef<APIError | null>(null)
  const pending = shallowRef(true)
  const attempt = shallowRef(0)

  watch([() => toValue(url), attempt], async ([path], _previous, onCleanup) => {
    const controller = new AbortController()
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
