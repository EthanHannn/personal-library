// Configuration/restart smoke against an independent synthetic release only.
// No live websites, custom DNS routing, or SSRF guard exceptions are needed.
import assert from 'node:assert/strict'
import { readFile, writeFile } from 'node:fs/promises'
import { parseArgs } from 'node:util'
const { values } = parseArgs({ options: { base: { type: 'string', default: 'http://127.0.0.1:16871' }, out: { type: 'string', default: '.local/sources-smoke.json' }, resume: { type: 'string' } } })
if (new URL(values.base).hostname !== '127.0.0.1') throw Error('An isolated loopback library is required')
const checks = []
async function request(path, method = 'GET', body, status = 200, auth = true, origin = 'https://library.example.test') {
  const response = await fetch(values.base + path, { method, headers: { ...(auth ? { 'Remote-User': 'owner' } : {}), ...(body ? { Origin: origin, 'Content-Type': 'application/json' } : {}) }, body: body ? JSON.stringify(body) : undefined, signal: AbortSignal.timeout(10000) })
  const data = await response.json(); assert.equal(response.status, status, `${method} ${path}: ${JSON.stringify(data)}`)
  checks.push({ path, method, status }); return data
}
assert.equal((await request('/api/v1/books/5')).book.title, '合成作品 5')
if (values.resume) {
  const saved = JSON.parse(await readFile(values.resume, 'utf8'))
  const { sources } = await request('/api/v1/manage/sources')
  const source = sources.find(s => s.id === saved.sourceId)
  assert.equal(source.version, saved.version); assert.equal(source.enabled, false); assert.equal(source.config.name, '合成书源配置')
  console.log(JSON.stringify({ complete: true, resumed: true, checks: checks.length })); process.exit(0)
}
await request('/api/v1/manage/sources', 'GET', undefined, 401, false)
const page = await request('/api/v1/manage/sources'); assert.ok(page.sources.some(s => s.id === 'qidian')); assert.ok(page.templates.length >= 4)
const config = { ...page.templates.find(t => t.id === 'html').config, name: '合成书源配置' }
await request('/api/v1/manage/sources', 'POST', { config, enabled: true }, 403, true, 'https://other.test')
await request('/api/v1/manage/sources', 'POST', { config: { ...config, js: 'fetch()' }, enabled: true }, 400)
await request('/api/v1/manage/sources', 'POST', { config: { ...config, baseUrl: 'http://127.0.0.1/' }, enabled: true }, 400)
await request('/api/v1/manage/sources', 'POST', { config: { ...config, chapter: { ...config.chapter, content: { selector: '[' } } }, enabled: true }, 400)
const source = await request('/api/v1/manage/sources', 'POST', { config, enabled: true })
await request('/api/v1/manage/jobs', 'POST', { kind: 'collect', input: { sourceId: source.id, sourceVersion: 999, url: 'https://example.com/book', maxChapters: 1 } }, 409)
await request('/api/v1/manage/jobs', 'POST', { kind: 'collect', input: { sourceId: source.id, url: 'http://169.254.169.254/', maxChapters: 1 } }, 400)
const disabled = await request(`/api/v1/manage/sources/${source.id}`, 'PUT', { config, expectedVersion: source.version, enabled: false })
await request(`/api/v1/manage/sources/${source.id}`, 'PUT', { config, expectedVersion: source.version, enabled: true }, 409)
await request('/api/v1/manage/jobs', 'POST', { kind: 'search', input: { sourceIds: [source.id], query: '测试' } }, 400)
await writeFile(values.out, JSON.stringify({ synthetic: true, complete: true, at: new Date().toISOString(), sourceId: source.id, version: disabled.version, checks }, null, 2))
console.log(JSON.stringify({ complete: true, checks: checks.length, out: values.out }))
