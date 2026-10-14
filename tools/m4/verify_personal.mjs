// Read-only migration/restore check. Never send mutation requests to real data.
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { readFile, writeFile } from 'node:fs/promises'
import { DatabaseSync } from 'node:sqlite'
import { parseArgs } from 'node:util'

const { values } = parseArgs({ options: {
  db: { type: 'string' }, base: { type: 'string' }, out: { type: 'string' }, baseline: { type: 'string' },
} })
for (const key of ['db', 'base', 'out']) assert.ok(values[key], `--${key} is required`)
assert.equal(new URL(values.base).hostname, '127.0.0.1')
const db = new DatabaseSync(values.db, { readOnly: true })
const report = { complete: false, at: new Date().toISOString(), requests: 0, issues: [], personalDigest: '' }
const observed = []
const sha = value => createHash('sha256').update(value).digest('hex')
const millis = value => typeof value === 'number' ? value : /^\d+$/.test(value) ? Number(value)
  : Date.parse(/(?:Z|[+-]\d\d:\d\d)$/.test(value) ? value : `${value.replace(' ', 'T')}Z`)
async function request(route, binary = false) {
  const response = await fetch(new URL(route, values.base), {
    headers: { 'Remote-User': 'owner' }, signal: AbortSignal.timeout(15000),
  })
  assert.equal(response.status, 200, `HTTP failure: ${route}`)
  report.requests++
  return binary ? Buffer.from(await response.arrayBuffer()) : response.json()
}
async function page(route) {
  const all = []
  let cursor
  do {
    const result = await request(`${route}?limit=48${cursor ? `&cursor=${encodeURIComponent(cursor)}` : ''}`)
    all.push(...result.entries)
    cursor = result.nextCursor
    assert.ok(all.length <= 100000, 'Unexpected collection cursor cycle')
  } while (cursor)
  return all
}
try {
  const me = await request('/api/v1/me')
  assert.equal(me.user.id, 'default-reader')
  observed.push(me)
  const shelf = db.prepare("SELECT bookId,addedAt FROM bookshelf_entries WHERE userId='default-reader' ORDER BY bookId").all()
  const progress = db.prepare("SELECT bookId,chapterIdx,percent,updatedAt FROM reading_progress WHERE userId='default-reader' ORDER BY bookId").all()
  const votes = db.prepare("SELECT bookId,type,createdAt FROM votes WHERE userId='default-reader' ORDER BY bookId").all()
  const profile = await request('/api/v1/profile')
  assert.deepEqual(profile.counts, { shelf: shelf.length, reading: progress.length, votes: votes.length })
  observed.push(profile.counts)
  const shelfPage = await page('/api/v1/shelf')
  assert.deepEqual(shelfPage.map(x => x.book.id).sort((a, b) => a - b), shelf.map(x => x.bookId))
  for (const entry of shelf) assert.equal(shelfPage.find(x => x.book.id === entry.bookId).addedAt, millis(entry.addedAt))
  const readingPage = await page('/api/v1/reading')
  assert.deepEqual(readingPage.map(x => x.book.id).sort((a, b) => a - b), progress.map(x => x.bookId))
  const votePage = await page('/api/v1/votes')
  assert.deepEqual(votePage.map(x => [x.book.id, x.type, x.createdAt]).sort((a, b) => a[0] - b[0]),
    votes.map(x => [x.bookId, x.type, millis(x.createdAt)]))
  // Include books belonging only to historical readers to catch implicit merging.
  const ids = db.prepare('SELECT bookId FROM bookshelf_entries UNION SELECT bookId FROM reading_progress UNION SELECT bookId FROM votes ORDER BY bookId').all()
  for (const { bookId } of ids) {
    const detail = await request(`/api/v1/books/${bookId}`)
    const expected = progress.find(x => x.bookId === bookId)
    assert.equal(detail.inShelf, shelf.some(x => x.bookId === bookId))
    assert.equal(detail.vote.myVote, votes.find(x => x.bookId === bookId)?.type ?? null)
    assert.deepEqual(detail.vote.current.counts, detail.vote.source.counts, 'Imported votes were counted twice')
    const value = detail.progress
    if (expected) {
      assert.equal(value.chapterIdx, expected.chapterIdx)
      assert.equal(value.percent, expected.percent)
      assert.equal(value.updatedAt, millis(expected.updatedAt))
      assert.deepEqual((await request(`/api/v1/books/${bookId}/progress`)).progress, value)
    } else assert.equal(value, null)
    observed.push({ bookId, inShelf: detail.inShelf, progress: value, vote: detail.vote })
  }
  const sample = db.prepare("SELECT id FROM books WHERE hasContent=1 AND status='APPROVED' AND (coverPath LIKE '%.jpg' OR coverPath LIKE '%.webp' OR coverPath LIKE '%.png') ORDER BY id LIMIT 1").get()
  assert.ok(sample, 'A real readable book with a cover is required')
  const block = await request(`/api/v1/books/${sample.id}/chapters/1/content`)
  assert.ok(block.text.length > 0)
  observed.push({ sampleBook: sample.id, text: sha(block.text), cover: sha(await request(`/api/v1/books/${sample.id}/cover`, true)),
    start: block.blockStart, end: block.blockEnd, chapterStart: block.chapterStart, chapterEnd: block.chapterEnd })
  report.defaultReaderCounts = profile.counts
  report.totalReaders = db.prepare('SELECT count(*) AS n FROM users').get().n
  report.personalDigest = sha(JSON.stringify(observed))
  if (values.baseline) {
    const baseline = JSON.parse(await readFile(values.baseline, 'utf8'))
    assert.equal(baseline.complete, true)
    assert.equal(report.personalDigest, baseline.personalDigest, 'Restored/restarted service differs from candidate')
  }
  report.complete = true
} catch (error) {
  report.issues.push(error.message?.split('\n')[0] ?? 'Personal verification failed')
  process.exitCode = 1
} finally {
  db.close()
  await writeFile(values.out, `${JSON.stringify(report, null, 2)}\n`, { flag: 'wx' })
  console.log(JSON.stringify(report))
}
