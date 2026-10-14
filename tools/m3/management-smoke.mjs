// Run only against an independent synthetic library. No external sites are used.
import assert from 'node:assert/strict'
import { readFile, writeFile } from 'node:fs/promises'
import { parseArgs } from 'node:util'
const { values } = parseArgs({ options: { base: { type: 'string', default: 'http://127.0.0.1:16871' }, out: { type: 'string', default: '.local/management-smoke.json' }, resume: { type: 'string' }, path: { type: 'string', default: 'management-test.txt' } } })
const checks = []
if (new URL(values.base).hostname !== '127.0.0.1') throw Error('An isolated loopback library is required')
async function request(path, method = 'GET', body, status = 200, auth = true, origin = 'https://library.example.test') {
  const response = await fetch(values.base + path, { method, headers: { ...(auth ? { 'Remote-User': 'owner' } : {}), ...(body ? { Origin: origin, 'Content-Type': 'application/json' } : {}) }, body: body ? JSON.stringify(body) : undefined, signal: AbortSignal.timeout(10000) })
  const data = await response.json()
  assert.equal(response.status, status, `${method} ${path}: ${JSON.stringify(data)}`)
  checks.push({ path, method, status }); return data
}
async function until(id, status) {
  for (let i = 0; i < 80; i++) {
    const job = await request(`/api/v1/manage/jobs/${id}`)
    if (job.status === status) return job
    assert.notEqual(job.status, 'failed', job.message)
    await new Promise(resolve => setTimeout(resolve, 250))
  }
  throw Error(`Job ${id} did not reach ${status}`)
}
assert.equal((await request('/api/v1/books/5')).book.title, '合成作品 5')
if (values.resume) {
  const saved = JSON.parse(await readFile(values.resume, 'utf8'))
  const job = await request(`/api/v1/manage/jobs/${saved.importJob}`)
  assert.equal(job.status, 'completed'); assert.equal(job.result.bookId, saved.bookId)
  assert.equal((await request(`/api/v1/books/${saved.bookId}/reader`)).book.chapterCount, 2)
  assert.equal((await request(`/api/v1/books/${saved.bookId}`)).book.title, '管理合成作品')
  console.log(JSON.stringify({ complete: true, resumed: true, checks: checks.length })); process.exit(0)
}
for (const path of ['/api/v1/profile', '/api/v1/votes', '/api/v1/manage/books', '/api/v1/manage/jobs']) await request(path, 'GET', undefined, 401, false)
const profile = await request('/api/v1/profile'); assert.equal(typeof profile.counts.votes, 'number')
await request('/api/v1/books/5/recommendations')
await request('/api/v1/manage/books', 'POST', { title: 'bad', status: 'APPROVED', hasContent: true }, 400)
await request('/api/v1/manage/jobs', 'POST', { kind: 'scan', input: {} }, 403, true, 'https://other.test')
await request('/api/v1/manage/jobs', 'POST', { kind: 'collect', input: { url: 'https://127.0.0.1/', maxChapters: 1 } }, 400)
const created = await request('/api/v1/manage/books', 'POST', { title: '管理待审合成作品', author: '合成作者', status: 'PENDING' })
await request(`/api/v1/books/${created.bookId}`, 'GET', undefined, 404)
const pending = await request('/api/v1/manage/books?filter=pending&search=' + encodeURIComponent('管理待审'))
assert.equal(pending.books[0].id, created.bookId)
const edit = { title: '管理待审合成作品', author: '合成作者', status: 'APPROVED', expectedVersion: pending.books[0].updatedAt }
await request(`/api/v1/manage/books/${created.bookId}`, 'PUT', edit)
await request(`/api/v1/manage/books/${created.bookId}`, 'PUT', edit, 409)
const input = { path: values.path, encoding: 'utf-8', title: '管理合成作品', author: '合成作者', tag1: '测试分类' }
const job = await request('/api/v1/manage/jobs', 'POST', { kind: 'import', input }, 202)
const duplicate = await request('/api/v1/manage/jobs', 'POST', { kind: 'import', input }, 202); assert.equal(duplicate.id, job.id)
const preview = await until(job.id, 'review'); assert.equal(preview.result.chapterCount, 2)
assert.equal((await request('/api/v1/books?search=' + encodeURIComponent('管理合成作品'))).pagination.total, 0)
await request(`/api/v1/manage/jobs/${job.id}/confirm`, 'POST', {})
const imported = await until(job.id, 'completed'); const bookId = imported.result.bookId
await request(`/api/v1/manage/jobs/${job.id}/confirm`, 'POST', {})
const content = await request(`/api/v1/books/${bookId}/chapters/2/content`); assert.match(content.text, /第二章/)
const scan = await request('/api/v1/manage/jobs', 'POST', { kind: 'scan', input: {} }, 202)
const report = await until(scan.id, 'completed'); assert.ok(report.result.checked > 0)
await request('/api/v1/manage/jobs?cursor=' + job.id)
await writeFile(values.out, JSON.stringify({ synthetic: true, complete: true, at: new Date().toISOString(), importJob: job.id, bookId, checks }, null, 2))
console.log(JSON.stringify({ complete: true, checks: checks.length, bookId, out: values.out }))
