import { onScopeDispose, shallowRef } from 'vue'
import { APIError, requestJSON } from '../api'

export function useMutation() {
  const pending = shallowRef(false)
  const error = shallowRef<APIError | null>(null)
  let active: AbortController | null = null
  function lost() { active?.abort(); pending.value = false; error.value = new APIError('登录已过期，请重新登录。', 401) }
  window.addEventListener('library:auth-lost', lost)
  onScopeDispose(() => { active?.abort(); window.removeEventListener('library:auth-lost', lost) })
  async function run<T>(path: string, method: string, body: unknown): Promise<T | null> {
    if (pending.value) return null
    const controller = new AbortController(); active = controller
    pending.value = true; error.value = null
    try {
      const result = await requestJSON<T>(path, { method, body: JSON.stringify(body), headers: { 'Content-Type': 'application/json' }, signal: controller.signal })
      return controller.signal.aborted ? null : result
    } catch (cause) {
      if (!controller.signal.aborted) error.value = cause instanceof APIError ? cause : new APIError('连接中断，请重试。', 0)
      return null
    } finally { if (!controller.signal.aborted) pending.value = false }
  }
  return { pending, error, run }
}
