import { onScopeDispose, shallowRef, watch } from 'vue'
import { APIError, getJSON } from '../api'
import type { Job } from '../manage'

export function useJob(id: () => string) {
  const data = shallowRef<Job | null>(null), error = shallowRef<APIError | null>(null), pending = shallowRef(false)
  let active: AbortController | null = null
  let timer: ReturnType<typeof setTimeout> | undefined
  let disposed = false
  function stop() { active?.abort(); clearTimeout(timer) }
  function lost() { stop(); data.value = null; pending.value = false; error.value = new APIError('登录已过期，请重新登录。', 401) }
  window.addEventListener('library:auth-lost', lost)
  async function reload() {
    stop(); const controller = new AbortController(); active = controller
    pending.value = !data.value; error.value = null
    try {
      const job = await getJSON<Job>(`/api/v1/manage/jobs/${encodeURIComponent(id())}`, controller.signal)
      if (disposed || controller.signal.aborted) return
      data.value = job
      if (job.status === 'queued' || job.status === 'running') timer = setTimeout(reload, document.hidden ? 15000 : 3000)
    } catch (cause) { if (!controller.signal.aborted) error.value = cause instanceof APIError ? cause : new APIError('任务状态读取失败，请重试。', 0) }
    finally { if (!controller.signal.aborted) pending.value = false }
  }
  watch(id, () => { data.value = null; void reload() }, { immediate: true })
  onScopeDispose(() => { disposed = true; stop(); window.removeEventListener('library:auth-lost', lost) })
  return { data, error, pending, reload }
}
