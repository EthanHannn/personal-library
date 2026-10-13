// Catalog release checks against either the five-book CI fixture or the full
// metadata rehearsal. No writes, real account or novel text search required.
import assert from 'node:assert/strict'
import { writeFile } from 'node:fs/promises'
import { parseArgs } from 'node:util'

const { values } = parseArgs({ options: { base: { type: 'string' }, out: { type: 'string' } } })
if (!values.base || new URL(values.base).hostname !== '127.0.0.1') throw new Error('A loopback --base is required')
const checks = []
async function request(path, status = 200, user = 'owner') {
  const res = await fetch(new URL(path, values.base), { headers: user ? { 'Remote-User': user } : {}, redirect: 'manual', signal: AbortSignal.timeout(5000) })
  assert.equal(res.status, status, path)
  assert.equal(res.headers.get('cache-control'), 'private, no-store')
  checks.push({ path, status })
  return res
}
async function json(query) { return (await request(`/api/v1/books?page=1&${query}`)).json() }
const sample = (await (await request('/api/v1/books/5')).json()).book
assert.equal(sample.title, '合成作品 5', 'Synthetic fixture required')
const all = await json('limit=2')
assert.equal(all.books.length, 2)
assert.ok(all.pagination.total >= 5)
const second = await (await request('/api/v1/books?page=2&limit=2')).json()
assert.equal(second.pagination.page, 2)
assert.ok(second.books.every(book => !all.books.some(first => first.id === book.id)))
for (const sort of ['score', 'xiancao', 'popularity', 'latest', 'words', 'title']) {
  const result = await json(`sortBy=${sort}&limit=48`)
  assert.ok(result.books.length > 0 && result.books.length <= 48)
  assert.equal(result.pagination.total, all.pagination.total)
  const field = { score: 'score', xiancao: 'xiancaoCount', popularity: 'popularity', words: 'wordCount' }[sort]
  if (field) for (let i = 1; i < result.books.length; i++) {
    const previous = result.books[i - 1], book = result.books[i]
    assert.ok(previous[field] > book[field] || previous[field] === book[field] && previous.id > book.id, `${sort} ordering`)
  }
}
for (const search of ['合', '合成', '%', '_', "' OR 1=1 --"]) {
  const result = await json(`search=${encodeURIComponent(search)}`)
  assert.ok(result.books.every(book => book.title.includes(search) || book.author.includes(search)))
  if (search === '合' || search === '合成') assert.ok(result.books.length > 0)
}
const exact = await json(`author=${encodeURIComponent(sample.author)}&tag1=${encodeURIComponent(sample.tag1)}&tag2=${encodeURIComponent(sample.tag2)}&minWords=${sample.wordCount}&maxWords=${sample.wordCount}`)
assert.ok(exact.books.some(book => book.id === sample.id))
assert.ok(exact.books.every(book => book.author === sample.author && book.tag1 === sample.tag1 && book.tag2 === sample.tag2 && book.wordCount === sample.wordCount))
const featured = await json('minScore=7.5&onlyContent=1')
assert.ok(featured.books.every(book => book.score >= 7.5 && book.hasContent))
const low = await json('minScore=0&maxScore=7.4')
assert.ok(low.books.every(book => book.score < 7.5))
const empty = await json('search=不存在的合成查询_29ac8')
assert.deepEqual(empty.books, [])
assert.equal(empty.pagination.total, 0)
const last = await (await request('/api/v1/books?page=10000&limit=2')).json()
assert.equal(last.pagination.page, last.pagination.totalPages)
const categories = (await (await request('/api/v1/categories')).json()).categories
assert.ok(categories.length > 0)
assert.ok(categories.some(category => category.name === sample.tag1))
for (const query of ['status=PENDING', 'search=a&search=b', 'minScore=NaN', 'maxScore=11', 'minWords=10&maxWords=1', 'sortBy=bad', 'onlyContent=2', 'after=1', 'limit=49']) {
  await request(`/api/v1/books?page=1&${query}`, 400)
}
for (const prefix of ['author', 'category']) {
  const path = `/${prefix}/${encodeURIComponent("O'Neil/中文? #")}`
  assert.match((await request(path)).headers.get('content-type'), /text\/html/)
  await request(path, 401, '')
  await request(`/${prefix}/`, 404)
}
await request('/api/v1/categories', 401, '')
await request('/api/v1/books?page=1', 403, 'stranger')
const report = { complete: true, synthetic: true, at: new Date().toISOString(), total: all.pagination.total, checks }
if (values.out) await writeFile(values.out, `${JSON.stringify(report, null, 2)}\n`, { flag: 'wx' })
console.log(JSON.stringify({ complete: true, checks: checks.length, total: report.total }))
