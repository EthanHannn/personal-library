// Smoke-test the actual release image against the synthetic M0 fixture.
import assert from 'node:assert/strict'
import { writeFile } from 'node:fs/promises'
import { parseArgs } from 'node:util'

const { values } = parseArgs({ options: { base: { type: 'string' }, out: { type: 'string' } } })
if (!values.base) throw new Error('--base is required')
const checks = []
async function request(path, status, authenticated = true, method = 'GET') {
  const response = await fetch(new URL(path, values.base), {
    method, redirect: 'manual', signal: AbortSignal.timeout(5000),
    headers: authenticated ? { 'Remote-User': 'owner' } : {},
  })
  assert.equal(response.status, status, `${method} ${path}`)
  assert.equal(response.headers.get('cache-control'), 'private, no-store', path)
  checks.push({ method, path, authenticated, status })
  return response
}

assert.equal((await (await request('/api/health', 200, false)).json()).status, 'ok')
const html = await (await request('/library', 200)).text()
const asset = html.match(/src="([^"]+\.js)"/)?.[1]
assert.ok(asset?.startsWith('/assets/'), 'embedded Vite entry is missing')
await request(asset, 200)
for (const path of ['/library', '/book/2', '/read/2?c=2', asset, '/api/v1/books']) {
  const response = await request(path, 401, false)
  assert.match(response.headers.get('content-type'), /application\/json/)
  assert.equal((await response.json()).error.code, 'unauthenticated')
}
for (const path of ['/assets/missing.js', '/api/missing', '/covers/missing.jpg', '/missing']) {
  assert.equal((await (await request(path, 404)).json()).error.code, 'not_found')
}
for (const path of ['/book/2', '/read/2?c=2']) {
  assert.match((await request(path, 200)).headers.get('content-type'), /text\/html/)
}
assert.equal(await (await request('/read/2?c=2', 200, true, 'HEAD')).text(), '')
assert.equal((await (await request('/api/v1/me', 200)).json()).user.id, 'default-reader')
const books = await (await request('/api/v1/books?limit=2', 200)).json()
assert.equal(books.books.length, 2)
assert.equal(books.nextAfter, 2)
const second = await (await request('/api/v1/books?limit=2&after=2', 200)).json()
assert.deepEqual(second.books.map(book => book.id), [3, 4])
await request('/api/v1/books?limit=49', 400)
await request('/api/v1/books?after=-1', 400)
const directory = await (await request('/api/v1/books/1/chapters?limit=1', 200)).json()
assert.equal(directory.chapters.length, 1)
assert.equal(directory.nextAfter, 1)
const bom = await (await request('/api/v1/books/2/chapters/1/content', 200)).json()
const gb = await (await request('/api/v1/books/3/chapters/1/content', 200)).json()
assert.equal(bom.text, gb.text)
assert.match(bom.text, /中文𠀀😀/)
const long = await (await request('/api/v1/books/4/chapters/1/content', 200)).json()
assert.ok(Buffer.byteLength(long.text) <= 256 * 1024)
assert.ok(long.nextCursor)
await request(`/api/v1/books/4/chapters/1/content?cursor=${encodeURIComponent(long.nextCursor + 'bad')}`, 400)
await request(`/api/v1/books/4/chapters/1/content?cursor=${encodeURIComponent(long.nextCursor)}`, 200)
assert.equal((await request('/login?return=https://example.test', 303)).headers.get('location'), '/')
const report = { complete: true, synthetic: true, at: new Date().toISOString(), checks }
if (values.out) await writeFile(values.out, `${JSON.stringify(report, null, 2)}\n`, { flag: 'wx' })
console.log(JSON.stringify({ complete: true, checks: checks.length }))
