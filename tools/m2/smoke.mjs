// Exercise the actual release service using only the generated M0 fixture.
import assert from 'node:assert/strict'
import { randomUUID } from 'node:crypto'
import { readFile, writeFile } from 'node:fs/promises'
import { parseArgs } from 'node:util'

const { values } = parseArgs({ options: { base: { type: 'string' }, out: { type: 'string' }, resume: { type: 'string' } } })
if (!values.base) throw new Error('--base is required')
const checks = []
async function request(path, { method = 'GET', body, status = 200, authenticated = true, origin = 'https://library.example.test', media = 'application/json' } = {}) {
  const headers = { ...(authenticated ? { 'Remote-User': 'owner' } : {}), ...(body ? { 'Content-Type': media, Origin: origin } : {}) }
  const response = await fetch(new URL(path, values.base), { method, headers, body: body ? JSON.stringify(body) : undefined, signal: AbortSignal.timeout(5000) })
  const json = await response.json()
  assert.equal(response.status, status, `${method} ${path}: ${JSON.stringify(json).slice(0, 300)}`)
  assert.equal(response.headers.get('cache-control'), 'private, no-store')
  checks.push({ method, path, status })
  return json
}

let expected
if (values.resume) {
  const report = JSON.parse(await readFile(values.resume, 'utf8'))
  assert.equal(report.synthetic, true)
  expected = report.expected
  const state = await request('/api/v1/books/4/reader')
  assert.deepEqual(state.progress, expected.progress)
  assert.equal(state.recoveryRequired, false)
  assert.equal(state.content.blockToken, expected.progress.anchor.block)
  assert.equal(state.content.text.slice(0, 100), expected.textPrefix)
  const replay = await request('/api/v1/books/4/progress', { method: 'PUT', body: expected.mutation })
  assert.deepEqual(replay.progress, expected.progress)
  assert.equal((await request('/api/v1/books/4/progress')).progress.revision, expected.progress.revision)
} else {
  const initial = await request('/api/v1/books/4/reader?chapter=1')
  assert.ok(initial.content.nextCursor)
  assert.equal(initial.content.blockStart, initial.content.chapterStart)
  assert.equal(initial.inShelf, false)
  assert.equal('chapters' in initial, false)
  await request('/api/v1/books/4/reader', { authenticated: false, status: 401 })
  const block = await request(`/api/v1/books/4/chapters/1/content?cursor=${encodeURIComponent(initial.content.nextCursor)}`)
  assert.ok(block.blockStart > block.chapterStart)
  const position = { chapterIdx: 1, percent: 0.2, contentVersion: block.contentVersion, anchor: { block: block.blockToken, paragraph: 0, character: 5 } }
  const mutation = { ...position, baseRevision: initial.progress?.revision ?? 0, deviceId: randomUUID(), clientSeq: 1, mutationId: randomUUID() }
  const path = '/api/v1/books/4/progress'
  await request(path, { method: 'PUT', body: mutation, authenticated: false, status: 401 })
  await request(path, { method: 'PUT', body: mutation, origin: 'https://sibling.example.test', status: 403 })
  await request(path, { method: 'PUT', body: mutation, media: 'text/plain', status: 415 })
  await request(path, { method: 'PUT', body: { ...mutation, anchor: { ...position.anchor, character: 9999 } }, status: 400 })
  const first = await request(path, { method: 'PUT', body: mutation })
  assert.equal(first.progress.revision, mutation.baseRevision + 1)
  assert.deepEqual(await request(path, { method: 'PUT', body: mutation }), first)
  const conflict = await request(path, { method: 'PUT', body: { ...mutation, deviceId: randomUUID(), mutationId: randomUUID() }, status: 409 })
  assert.deepEqual(conflict.progress, first.progress)
  assert.deepEqual(conflict.submitted, position)
  await request(path, { method: 'PUT', body: { ...mutation, baseRevision: first.progress.revision, mutationId: randomUUID() }, status: 409 })
  await request(path, { method: 'PUT', body: { ...mutation, percent: 0.9 }, status: 409 })
  const next = { ...mutation, percent: 0.1, anchor: { ...position.anchor, character: 6 }, mutationId: randomUUID(), clientSeq: 2, baseRevision: first.progress.revision }
  const saved = await request(path, { method: 'PUT', body: next })
  assert.equal(saved.progress.percent, 0.1, 'intentional rereading is supported')
  assert.deepEqual(await request(path, { method: 'PUT', body: mutation }), first, 'old replay must return its original receipt')
  assert.deepEqual((await request(path)).progress, saved.progress)
  const restored = await request('/api/v1/books/4/reader')
  assert.equal(restored.content.blockStart, block.blockStart)
  assert.deepEqual(restored.progress, saved.progress)
  const explicit = await request('/api/v1/books/4/reader?chapter=1')
  assert.equal(explicit.content.blockStart, block.chapterStart)
  assert.deepEqual(explicit.progress, saved.progress, 'initialization is read only')
  const directory = await request('/api/v1/books/1/directory?after=0&q=%E5%90%88%E6%88%90')
  assert.ok(directory.chapters.length > 0 && directory.chapters.length <= 100)
  expected = { progress: saved.progress, mutation: next, textPrefix: block.text.slice(0, 100) }
}
const report = { synthetic: true, complete: true, resumed: !!values.resume, at: new Date().toISOString(), checks, expected }
if (values.out) await writeFile(values.out, `${JSON.stringify(report, null, 2)}\n`, { flag: 'wx' })
console.log(JSON.stringify({ complete: true, resumed: report.resumed, checks: checks.length }))
