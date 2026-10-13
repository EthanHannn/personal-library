// Run beside tools/m0/measure.mjs to include durable writes in the read load.
import assert from 'node:assert/strict'
import { randomUUID } from 'node:crypto'
import { writeFile } from 'node:fs/promises'
import { parseArgs } from 'node:util'
const { values } = parseArgs({ options: { base: { type: 'string' }, out: { type: 'string' }, seconds: { type: 'string', default: '60' } } })
if (!values.base || !values.out) throw new Error('--base and --out are required')
const seconds = Number(values.seconds)
if (!Number.isFinite(seconds) || seconds < 1 || seconds > 1800) throw new Error('invalid duration')
const headers = { 'Remote-User': 'owner', Origin: 'https://library.example.test', 'Content-Type': 'application/json' }
const initial = await (await fetch(new URL('/api/v1/books/2/reader?chapter=1', values.base), { headers })).json()
assert.equal(initial.book.title, '合成作品 2')
const block = initial.content
let revision = initial.progress?.revision ?? 0
const deviceId = randomUUID()
const times = []
const started = performance.now()
while (performance.now() - started < seconds * 1000) {
  const mutation = { chapterIdx: 1, percent: (times.length % 100) / 100, contentVersion: block.contentVersion, anchor: { block: block.blockToken, paragraph: 0, character: 0 }, deviceId, clientSeq: times.length + 1, mutationId: randomUUID(), baseRevision: revision }
  const before = performance.now()
  const response = await fetch(new URL('/api/v1/books/2/progress', values.base), { method: 'PUT', headers, body: JSON.stringify(mutation), signal: AbortSignal.timeout(5000) })
  const result = await response.json()
  assert.equal(response.status, 200, JSON.stringify(result))
  revision = result.progress.revision
  times.push(performance.now() - before)
  await new Promise(resolve => setTimeout(resolve, 500))
}
times.sort((a, b) => a - b)
const report = { synthetic: true, complete: true, count: times.length, seconds: (performance.now() - started) / 1000, p50Ms: times[Math.ceil(times.length * 0.5) - 1], p95Ms: times[Math.ceil(times.length * 0.95) - 1], maxMs: times.at(-1) }
await writeFile(values.out, `${JSON.stringify(report, null, 2)}\n`, { flag: 'wx' })
console.log(JSON.stringify(report))
