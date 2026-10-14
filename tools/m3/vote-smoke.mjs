// Desired-state voting on an isolated synthetic release, including restarts.
import assert from 'node:assert/strict'
import { readFile, writeFile } from 'node:fs/promises'
import { parseArgs } from 'node:util'

const { values } = parseArgs({ options: { base: { type: 'string' }, out: { type: 'string' }, resume: { type: 'string' } } })
if (!values.base || new URL(values.base).hostname !== '127.0.0.1') throw new Error('An isolated loopback --base is required')
const checks = []
async function request(path, { method = 'GET', body, status = 200, user = 'owner', origin = 'https://library.example.test', media = 'application/json' } = {}) {
  const response = await fetch(new URL(path, values.base), {
    method, headers: { ...(user ? { 'Remote-User': user } : {}), ...(method !== 'GET' ? { Origin: origin, 'Content-Type': media } : {}) },
    body: body === undefined ? undefined : JSON.stringify(body), signal: AbortSignal.timeout(5000),
  })
  const json = await response.json()
  assert.equal(response.status, status, `${method} ${path}: ${JSON.stringify(json).slice(0, 300)}`)
  assert.equal(response.headers.get('cache-control'), 'private, no-store')
  checks.push({ method, path, status })
  return json
}
const path = '/api/v1/books/5/vote'
const detail = await request('/api/v1/books/5')
assert.equal(detail.book.title, '合成作品 5', 'Only the synthetic M0 fixture may be mutated')
let expected
if (values.resume) {
  const prior = JSON.parse(await readFile(values.resume, 'utf8'))
  assert.equal(prior.synthetic, true)
  expected = prior.expected
  assert.deepEqual(detail.vote, expected.vote)
  assert.deepEqual(await request(path), expected.vote)
  assert.deepEqual(await request(path, { method: 'PUT', body: { type: expected.vote.myVote } }), expected.vote, 'same choice after restart cannot add a vote')
} else {
  const baseline = await request(path)
  assert.deepEqual(detail.vote, baseline)
  await request(path, { user: '', status: 401 })
  await request(path, { user: 'stranger', status: 403 })
  await request(path, { method: 'PUT', body: { type: 'XIANCAO' }, user: '', status: 401 })
  await request(path, { method: 'PUT', body: { type: 'XIANCAO' }, origin: 'https://other.example.test', status: 403 })
  await request(path, { method: 'PUT', body: { type: 'XIANCAO' }, media: 'text/plain', status: 415 })
  for (const body of [{}, null, { type: 'OTHER' }, { type: 'XIANCAO', readerId: 'other' }]) await request(path, { method: 'PUT', body, status: 400 })
  await request('/api/v1/books/9007199254740991/vote', { method: 'PUT', body: { type: 'XIANCAO' }, status: 404 })
  await request(`${path}?reader=other`, { status: 400 })
  const totalsWithoutMine = { ...baseline.current.counts }
  if (baseline.myVote) totalsWithoutMine[baseline.myVote]--
  function checkChoice(vote, type) {
    assert.equal(vote.myVote, type)
    assert.deepEqual(vote.source, baseline.source, 'source history must stay immutable')
    assert.deepEqual(vote.current.counts, { ...totalsWithoutMine, [type]: totalsWithoutMine[type] + 1 })
    assert.equal(vote.current.popularity, baseline.current.popularity + (baseline.myVote ? 0 : 1))
  }
  for (const type of ['XIANCAO', 'LIANGCAO', 'GANCAO', 'KUCAO', 'DUCAO']) {
    const vote = await request(path, { method: 'PUT', body: { type } })
    checkChoice(vote, type)
    assert.deepEqual(await request(path, { method: 'PUT', body: { type } }), vote, 'retry changed the tally')
  }
  const concurrent = await Promise.all(['XIANCAO', 'DUCAO', 'GANCAO', 'LIANGCAO', 'KUCAO'].map(async type => {
    const vote = await request(path, { method: 'PUT', body: { type } }); checkChoice(vote, type); return vote
  }))
  assert.equal(concurrent.length, 5)
  const vote = await request(path, { method: 'PUT', body: { type: 'GANCAO' } })
  checkChoice(vote, 'GANCAO')
  const after = await request('/api/v1/books/5')
  assert.deepEqual(after.vote, vote)
  assert.deepEqual(after.progress, detail.progress, 'voting must not create or alter reading progress')
  assert.equal(after.inShelf, detail.inShelf, 'voting must not change the shelf')
  assert.equal(after.book.score, detail.book.score, 'source discovery score must remain independent')
  expected = { vote }
}
const report = { synthetic: true, complete: true, resumed: !!values.resume, at: new Date().toISOString(), checks, expected }
if (values.out) await writeFile(values.out, `${JSON.stringify(report, null, 2)}\n`, { flag: 'wx' })
console.log(JSON.stringify({ complete: true, resumed: report.resumed, checks: checks.length }))
