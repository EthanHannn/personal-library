import type { Mutation, Position, Progress } from '../api'
import { cleanPosition, samePosition } from './text'

export interface LocalProgress {
  bookId: number
  owner: string
  deviceId: string
  seq: number
  revision: number
  draft: Position | null
  pending: Mutation | null
  legacy: Position | null
  legacyPending: boolean
}

export class OwnershipError extends Error {}
const DB_NAME = 'library-reader-v1'
let database: Promise<IDBDatabase> | undefined

function open(): Promise<IDBDatabase> {
  database ??= new Promise((resolve, reject) => {
    const request = indexedDB.open(DB_NAME, 1)
    request.onupgradeneeded = () => request.result.createObjectStore('progress', { keyPath: 'bookId' })
    request.onsuccess = () => {
      request.result.onversionchange = () => { request.result.close(); database = undefined }
      resolve(request.result)
    }
    request.onerror = () => { database = undefined; reject(request.error) }
    request.onblocked = () => { database = undefined; reject(new Error('阅读记录存储被其他页面占用')) }
  })
  return database
}

function legacy(bookId: number): Pick<LocalProgress, 'legacy' | 'legacyPending'> {
  try {
    const p = JSON.parse(localStorage.getItem(`zx_reading_${bookId}`) ?? 'null')
    if (p && Number.isSafeInteger(p.chapterIdx) && p.chapterIdx > 0 && Number.isFinite(p.percent) && p.percent >= 0 && p.percent <= 1) {
      return { legacy: { chapterIdx: p.chapterIdx, percent: p.percent }, legacyPending: p.pending === true }
    }
  } catch { /* A corrupt legacy record must not prevent reading. */ }
  return { legacy: null, legacyPending: false }
}

// Each update is one IndexedDB read/write transaction shared by all tabs. No
// async network work is performed inside the transaction.
export async function updateLocal(bookId: number, update: (record: LocalProgress) => void): Promise<LocalProgress> {
  const db = await open()
  return new Promise((resolve, reject) => {
    const tx = db.transaction('progress', 'readwrite', { durability: 'strict' })
    const store = tx.objectStore('progress')
    const request = store.get(bookId)
    let record: LocalProgress
    let failure: unknown
    request.onsuccess = () => {
      record = request.result ?? { bookId, owner: '', deviceId: crypto.randomUUID(), seq: 0, revision: 0, draft: null, pending: null, ...legacy(bookId) }
      try { update(record); store.put(record) } catch (error) { failure = error; tx.abort() }
    }
    tx.oncomplete = () => resolve(record)
    tx.onerror = tx.onabort = () => reject(failure ?? tx.error ?? new Error('无法暂存阅读位置'))
  })
}

export function claim(bookId: number, owner: string) {
  return updateLocal(bookId, r => { r.owner = owner })
}

function owned(r: LocalProgress, owner: string) {
  if (r.owner !== owner) throw new OwnershipError('另一标签页正在阅读此书')
}

export function saveDraft(bookId: number, owner: string, position: Position) {
  return updateLocal(bookId, r => { owned(r, owner); r.draft = cleanPosition(position); r.legacy = null; r.legacyPending = false })
}

export function prepareMutation(bookId: number, owner: string) {
  return updateLocal(bookId, r => {
    owned(r, owner)
    if (!r.pending && r.draft?.anchor) {
      r.pending = { ...r.draft, deviceId: r.deviceId, clientSeq: ++r.seq, mutationId: crypto.randomUUID(), baseRevision: r.revision }
    }
  })
}

export function acknowledge(bookId: number, mutation: Mutation, progress: Progress) {
  return updateLocal(bookId, r => {
    if (r.pending?.mutationId !== mutation.mutationId) return
    // Only the acknowledged snapshot is removed. Scrolling during a request
    // leaves the newer draft intact for the following mutation.
    const { baseRevision: _base, deviceId: _device, clientSeq: _seq, mutationId: _id, ...position } = mutation
    if (samePosition(r.draft, position)) r.draft = null
    r.pending = null
    r.revision = progress.revision
  })
}

export function resolveLocal(bookId: number, owner: string, revision: number, position: Position | null) {
  return updateLocal(bookId, r => {
    owned(r, owner)
    r.pending = null; r.draft = position ? cleanPosition(position) : null; r.revision = revision
    r.legacy = null; r.legacyPending = false
  })
}
