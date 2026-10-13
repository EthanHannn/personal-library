import { computed, onScopeDispose, shallowRef } from 'vue'
import { APIError, requestJSON } from '../api'
import type { Position, Progress } from '../api'
import { acknowledge, claim, OwnershipError, prepareMutation, resolveLocal, saveDraft, updateLocal } from '../reader/localProgress'
import type { LocalProgress } from '../reader/localProgress'
import { samePosition } from '../reader/text'

export function useReadingProgress(bookId: number) {
  const owner = crypto.randomUUID()
  const server = shallowRef<Progress | null>(null)
  const local = shallowRef<LocalProgress | null>(null)
  const conflict = shallowRef<{ local: Position; remote: Progress | null } | null>(null)
  const ready = shallowRef(false)
  const expired = shallowRef(false)
  const taken = shallowRef(false)
  const message = shallowRef('正在恢复阅读位置…')
  const storageError = shallowRef(false)
  const syncing = shallowRef(false)
  let latest: Position | null = null
  let dirty = false
  let writing: Promise<void> | null = null
  let disposed = false
  const channel = typeof BroadcastChannel === 'undefined' ? null : new BroadcastChannel('library-reading-v1')
  const writable = computed(() => ready.value && !expired.value && !taken.value && !conflict.value && !storageError.value)

  function storageFailure(error: unknown) {
    if (error instanceof OwnershipError) {
      taken.value = true; ready.value = false; message.value = '另一标签页正在阅读此书'
    } else {
      storageError.value = true; message.value = '无法在本机暂存进度，请检查浏览器存储权限'
    }
  }

  async function persist() {
    if (!dirty || !latest || taken.value || conflict.value) return
    const snapshot = latest
    try {
      local.value = await saveDraft(bookId, owner, snapshot)
      if (samePosition(latest, snapshot)) dirty = false
      if (!expired.value) message.value = '已暂存本机 · 等待同步'
    } catch (error) { storageFailure(error) }
  }

  function capture(position: Position) {
    if (!writable.value || samePosition(latest, position)) return
    latest = position; dirty = true
  }

  async function flush(keepalive = false): Promise<void> {
    if (writing) { await writing; return }
    if (taken.value || expired.value || conflict.value || storageError.value || !local.value) return
    writing = (async () => {
      await persist()
      if (taken.value || storageError.value) return
      let record: LocalProgress
      try { record = await prepareMutation(bookId, owner); local.value = record } catch (error) { storageFailure(error); return }
      const mutation = record.pending
      if (!mutation) return
      syncing.value = true
      try {
        const result = await requestJSON<{ progress: Progress }>(`/api/v1/books/${bookId}/progress`, {
          method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(mutation), keepalive, signal: AbortSignal.timeout(5000),
        })
        local.value = await acknowledge(bookId, mutation, result.progress)
        if (!server.value || result.progress.revision >= server.value.revision) server.value = result.progress
        message.value = dirty || local.value.draft ? '已暂存本机 · 等待同步' : '进度已同步'
        if (!disposed) channel?.postMessage({ bookId, owner, type: 'saved', revision: result.progress.revision })
      } catch (error) {
        if (error instanceof APIError && ['progress_conflict', 'stale_sequence', 'mutation_reused'].includes(error.code)) {
          server.value = error.progress
          conflict.value = { local: latest ?? record.draft ?? mutation, remote: error.progress }
          ready.value = false
          message.value = '有另一处阅读进度，等待选择'
        } else if (error instanceof APIError && [400, 404, 409, 422].includes(error.status)) {
          ready.value = false
          message.value = `${error.message}；本机位置仍保留`
        } else if (!expired.value) {
          message.value = '尚未同步 · 本机位置已保留，可重试'
        }
      } finally { syncing.value = false }
    })()
    try { await writing } finally { writing = null }
  }

  async function start(remote: Progress | null): Promise<Position | null> {
    ready.value = false; server.value = remote
    try {
      local.value = await claim(bookId, owner)
      channel?.postMessage({ bookId, owner, type: 'claim' })
      // Retry a durable immutable request first: a lost ACK may already have
      // committed. Comparing only revisions would mislabel it as a conflict.
      if (local.value.pending) await flush()
      const r = local.value
      const position = r.draft ?? r.pending ?? (r.legacyPending || !remote ? r.legacy : null)
      if (position && !conflict.value && ((r.legacyPending && remote) || ((r.draft || r.pending) && r.revision !== (server.value?.revision ?? 0)))) {
        conflict.value = { local: position, remote: server.value }
        message.value = '本机与服务端位置不同，等待选择'
      }
      if (!position && !conflict.value) {
        local.value = await updateLocal(bookId, r => { if (r.owner !== owner) throw new OwnershipError(); r.revision = server.value?.revision ?? 0 })
      }
      return position ?? server.value
    } catch (error) { storageFailure(error); return remote }
  }

  function restored(position: Position | null) {
    ready.value = !conflict.value && !taken.value && !expired.value
    latest = null
    if (position && samePosition(position, server.value)) latest = position
    else if (position) capture(position)
    if (ready.value && !storageError.value) message.value = local.value?.draft || local.value?.pending ? '已暂存本机 · 等待同步' : '进度已恢复'
  }

  async function choose(which: 'local' | 'remote'): Promise<Position | null> {
    const c = conflict.value
    if (!c) return null
    const position = which === 'local' ? c.local : c.remote
    try {
      local.value = await resolveLocal(bookId, owner, c.remote?.revision ?? 0, which === 'local' ? position : null)
      conflict.value = null; latest = position; dirty = false
    } catch (error) { storageFailure(error) }
    return position
  }

  async function restartConfirmed() {
    try { local.value = await resolveLocal(bookId, owner, server.value?.revision ?? 0, null); latest = null; dirty = false }
    catch (error) { storageFailure(error) }
  }

  async function revalidate() {
    const { progress } = await requestJSON<{ progress: Progress | null }>(`/api/v1/books/${bookId}/progress`, { signal: AbortSignal.timeout(5000) })
    if (progress && progress.revision !== (local.value?.revision ?? 0) && !taken.value) {
      if (local.value?.pending) await flush()
      if (progress.revision !== local.value?.revision) {
        conflict.value = { local: latest ?? local.value?.draft ?? progress, remote: progress }; ready.value = false
      }
    }
  }

  function authLost() {
    void persist()
    expired.value = true; ready.value = false
    message.value = '登录已失效 · 本机位置保留，重新登录后恢复'
  }
  window.addEventListener('library:auth-lost', authLost)
  if (channel) channel.onmessage = ({ data }) => {
    if (data.bookId === bookId && data.owner !== owner && data.type === 'claim') {
      taken.value = true; ready.value = false; message.value = '另一标签页正在阅读此书'
    }
  }
  const localTimer = window.setInterval(() => { void persist() }, 1000)
  const serverTimer = window.setInterval(() => { void flush() }, 7000)
  onScopeDispose(() => {
    disposed = true
    clearInterval(localTimer); clearInterval(serverTimer)
    window.removeEventListener('library:auth-lost', authLost); channel?.close()
  })
  return { server, local, conflict, ready, expired, taken, message, syncing, writable, start, capture, restored, persist, flush, choose, restartConfirmed, revalidate }
}
