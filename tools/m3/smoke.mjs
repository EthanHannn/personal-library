// Collection and homepage checks against an isolated M0-derived release database.
import assert from 'node:assert/strict'
import { readFile, writeFile } from 'node:fs/promises'
import { parseArgs } from 'node:util'

const { values } = parseArgs({ options: { base: { type: 'string' }, out: { type: 'string' }, resume: { type: 'string' } } })
if (!values.base || new URL(values.base).hostname !== '127.0.0.1') throw new Error('An isolated loopback --base is required')
const checks = []
async function request(path, { method = 'GET', body, status = 200, authenticated = true, origin = 'https://library.example.test', media = 'application/json' } = {}) {
  const response = await fetch(new URL(path, values.base), {
    method, headers: { ...(authenticated ? { 'Remote-User': 'owner' } : {}), ...(method !== 'GET' ? { Origin: origin, 'Content-Type': media } : {}) },
    body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(5000),
  })
  const json = await response.json()
  assert.equal(response.status, status, `${method} ${path}: ${JSON.stringify(json).slice(0, 300)}`)
  assert.equal(response.headers.get('cache-control'), 'private, no-store')
  checks.push({ method, path, status })
  return json
}

const detail = await request('/api/v1/books/5')
assert.equal(detail.book.title, '合成作品 5', 'Only the synthetic M0 fixture may be mutated')
let expected
if (values.resume) {
  const prior = JSON.parse(await readFile(values.resume, 'utf8'))
  assert.equal(prior.synthetic, true)
  expected = prior.expected
  assert.equal(detail.inShelf, true)
  assert.deepEqual(detail.progress, expected.progress)
  const page = await request('/api/v1/shelf?limit=48')
  const entry = page.entries.find(entry => entry.book.id === 5)
  assert.ok(entry)
  assert.equal(entry.addedAt, expected.addedAt)
  const home = await request('/api/v1/home')
  assert.equal(home.stats.books, expected.books)
} else {
  await request('/api/v1/home', { authenticated: false, status: 401 })
  await request('/api/v1/shelf', { authenticated: false, status: 401 })
  const home = await request('/api/v1/home')
  assert.ok(home.stats.books >= 5)
  assert.ok(home.featured.length <= 5 && home.highRated.length <= 6 && home.latest.length <= 6 && home.reading.length <= 3)
  for (const book of [...home.featured, ...home.highRated, ...home.latest]) {
    assert.equal(book.hasContent, true)
    assert.ok(book.score >= 7.5)
  }
  const path = '/api/v1/books/5/shelf'
  const beforeReading = await request('/api/v1/reading?limit=48')
  await request(path, { method: 'PUT', body: {}, authenticated: false, status: 401 })
  await request(path, { method: 'PUT', body: {}, origin: 'https://sibling.example.test', status: 403 })
  await request(path, { method: 'PUT', body: {}, media: 'text/plain', status: 415 })
  await request(path, { method: 'PUT', body: { readerId: 'other' }, status: 400 })
  await request(path, { method: 'DELETE', body: {} })
  assert.equal((await request(path, { method: 'PUT', body: {} })).inShelf, true)
  const first = await request('/api/v1/shelf?limit=1')
  assert.equal(first.entries[0].book.id, 5)
  await request(path, { method: 'PUT', body: {} })
  assert.deepEqual((await request('/api/v1/shelf?limit=1')).entries, first.entries, 'repeated add must not move or duplicate an entry')
  assert.deepEqual((await request('/api/v1/reading?limit=48')).entries, beforeReading.entries, 'collection must not start reading')
  assert.deepEqual((await request('/api/v1/books/5')).progress, detail.progress)

  const progressBefore = (await request('/api/v1/books/1')).progress
  await request('/api/v1/books/1/shelf', { method: 'DELETE', body: {} })
  await request('/api/v1/books/1/shelf', { method: 'PUT', body: {} })
  const page = await request('/api/v1/shelf?limit=1')
  assert.equal(page.entries[0].book.id, 1)
  assert.ok(page.nextCursor)
  const next = await request(`/api/v1/shelf?limit=1&cursor=${encodeURIComponent(page.nextCursor)}`)
  assert.equal(next.entries[0].book.id, 5)
  await request(`/api/v1/reading?cursor=${encodeURIComponent(page.nextCursor)}`, { status: 400 })
  await request('/api/v1/books/1/shelf', { method: 'DELETE', body: {} })
  await request('/api/v1/books/1/shelf', { method: 'DELETE', body: {} })
  assert.deepEqual((await request('/api/v1/books/1')).progress, progressBefore, 'removal must preserve progress including its anchor and revision')
  expected = { books: home.stats.books, progress: detail.progress, addedAt: first.entries[0].addedAt }
}
const report = { synthetic: true, complete: true, resumed: !!values.resume, at: new Date().toISOString(), checks, expected }
if (values.out) await writeFile(values.out, `${JSON.stringify(report, null, 2)}\n`, { flag: 'wx' })
console.log(JSON.stringify({ complete: true, resumed: report.resumed, checks: checks.length }))
