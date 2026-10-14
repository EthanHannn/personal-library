// Read-only comparison of every readable book's first/middle/last chapter.
// Uses the old release's WHATWG decoder against the actual new HTTP service.
import assert from 'node:assert/strict'
import { createHash } from 'node:crypto'
import { open, readFile, writeFile } from 'node:fs/promises'
import path from 'node:path'
import { DatabaseSync } from 'node:sqlite'
import { parseArgs } from 'node:util'

const { values } = parseArgs({ options: {
  db: { type: 'string' }, novels: { type: 'string' }, manifest: { type: 'string' }, covers: { type: 'string' },
  base: { type: 'string' }, out: { type: 'string' },
} })
for (const key of ['db', 'novels', 'manifest', 'covers', 'base', 'out']) assert.ok(values[key], `--${key} is required`)
assert.equal(new URL(values.base).hostname, '127.0.0.1', 'Only isolated loopback services may be checked')
const manifest = JSON.parse(await readFile(values.manifest, 'utf8'))
const files = new Map(manifest.files.map(file => [file.path, file]))
const db = new DatabaseSync(values.db, { readOnly: true })
const books = db.prepare('SELECT id,filePath,chapterCount FROM books WHERE hasContent=1 ORDER BY id').all()
const chapter = db.prepare('SELECT idx,title,startOffset,endOffset FROM chapters WHERE bookId=? AND idx=?')
const report = { complete: false, at: new Date().toISOString(), node: process.version, icu: process.versions.icu,
  books: 0, chapters: 0, blocks: 0, bytes: 0, coverReferences: 0, encodingCorrections: [], issues: [], contentDigest: '' }
const digest = createHash('sha256')
const started = performance.now()
try {
  for (const book of books) {
    const name = book.filePath.replaceAll('\\', '/')
    const file = files.get(name)
    assert.ok(file, `Missing manifest for book ${book.id}`)
    const source = await open(path.join(values.novels, name), 'r')
    try {
      const sample = Buffer.alloc(Math.min(8192, file.size))
      await source.read(sample, 0, sample.length, 0)
      let oldEncoding = 'utf-8'
      try { new TextDecoder('utf-8', { fatal: true }).decode(sample, { stream: sample.length < file.size }) }
      catch { oldEncoding = 'gb18030' }
      if (file.encoding === 'utf-16le' || file.encoding === 'utf-16be') {
        const bom = file.encoding === 'utf-16le' ? 'fffe' : 'feff'
        assert.equal(sample.subarray(0, 2).toString('hex'), bom, 'UTF-16 requires an evidenced BOM')
        // The old detector treated these files as GB18030 and displayed corrupt
        // text. Preserve original bytes/index; compare against the correct BOM.
        report.encodingCorrections.push({ bookId: book.id, previous: oldEncoding, current: file.encoding })
      } else assert.equal(oldEncoding, file.encoding, `Legacy encoding differs for book ${book.id}`)
      for (const idx of new Set([1, Math.max(1, Math.ceil(book.chapterCount / 2)), book.chapterCount])) {
        const expected = chapter.get(book.id, idx)
        assert.ok(expected, `Missing chapter ${book.id}/${idx}`)
        let cursor = null
        let offset = expected.startOffset
        let blocks = 0
        do {
          const url = new URL(`/api/v1/books/${book.id}/chapters/${idx}/content`, values.base)
          if (cursor) url.searchParams.set('cursor', cursor)
          const response = await fetch(url, { headers: { 'Remote-User': 'owner' }, signal: AbortSignal.timeout(15000) })
          assert.equal(response.status, 200, `HTTP failure for book ${book.id}, chapter ${idx}`)
          const block = await response.json()
          assert.equal(block.chapter.idx, idx)
          assert.equal(block.chapter.title, expected.title)
          assert.equal(block.chapterStart, expected.startOffset)
          assert.equal(block.chapterEnd, expected.endOffset)
          assert.equal(block.blockStart, offset)
          assert.ok(block.blockEnd > offset && block.blockEnd <= expected.endOffset)
          const raw = Buffer.alloc(block.blockEnd - offset)
          const read = await source.read(raw, 0, raw.length, offset)
          assert.equal(read.bytesRead, raw.length)
          const text = new TextDecoder(file.encoding, { fatal: true, ignoreBOM: offset !== 0 }).decode(raw)
          assert.equal(block.text, text, `Decoded content differs for book ${book.id}, chapter ${idx}, offset ${offset}`)
          digest.update(JSON.stringify([book.id, idx, offset, block.blockEnd]))
          digest.update(text)
          offset = block.blockEnd
          cursor = block.nextCursor
          report.blocks++
          report.bytes += raw.length
          assert.ok(++blocks < 10000, 'Unexpected cursor cycle')
        } while (cursor)
        assert.equal(offset, expected.endOffset)
        report.chapters++
      }
    } finally { await source.close() }
    report.books++
    if (report.books % 250 === 0) console.log(`Compared ${report.books}/${books.length} books (${report.chapters} chapters)`)
  }
  // Historical raster covers must be decodable by the service and byte-identical.
  const covers = db.prepare("SELECT id,coverPath FROM books WHERE coverPath IS NOT NULL AND coverPath<>'' AND status='APPROVED'").all()
  for (const cover of covers) {
    if (!/\.(png|jpe?g|gif|webp)$/i.test(cover.coverPath)) continue
    const response = await fetch(new URL(`/api/v1/books/${cover.id}/cover`, values.base), { headers: { 'Remote-User': 'owner' } })
    assert.equal(response.status, 200, `Historical cover ${cover.id} is not usable`)
    assert.ok(response.headers.get('content-type')?.startsWith('image/'))
    const bytes = Buffer.from(await response.arrayBuffer())
    const filename = path.resolve(values.covers, cover.coverPath.replace(/^\/covers\//, ''))
    assert.ok(filename.startsWith(path.resolve(values.covers) + path.sep), 'Cover escapes the rehearsal root')
    const original = await readFile(filename)
    assert.equal(createHash('sha256').update(bytes).digest('hex'), createHash('sha256').update(original).digest('hex'))
    report.coverReferences++
  }
  report.contentDigest = digest.digest('hex')
  report.complete = true
} catch (error) {
  // Assertion messages do not include novel text, request bodies or personal data.
  report.issues.push(error.message?.split('\n')[0] ?? 'Content verification failed')
  process.exitCode = 1
} finally {
  db.close()
  report.elapsedSeconds = Math.round((performance.now() - started) / 10) / 100
  await writeFile(values.out, `${JSON.stringify(report, null, 2)}\n`, { flag: 'wx' })
  console.log(JSON.stringify(report))
}
