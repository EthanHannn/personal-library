import test from 'node:test'
import assert from 'node:assert/strict'
import http from 'node:http'
import { once } from 'node:events'
import { execFile } from 'node:child_process'
import { promisify } from 'node:util'
import { mkdtemp, readFile, writeFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

test('loader verifies completed checks, preserves conflicting rules and avoids duplicates', async t => {
  const dir = await mkdtemp(join(tmpdir(), 'library-source-pack-'))
  t.after(() => rm(dir, { recursive: true, force: true }))
  const config = name => ({ name, kind: 'html', baseUrl: 'https://example.com/', book: { title: { selector: 'h1' } } })
  const conflict = { id: 'conflict', version: 1, enabled: true, config: { ...config('conflict'), intervalMs: 9000 } }
  const sources = [conflict]
  const jobs = new Map()
  const server = http.createServer(async (req, res) => {
    assert.equal(req.headers['remote-user'], 'owner')
    let body = ''
    for await (const chunk of req) body += chunk
    body = body ? JSON.parse(body) : undefined
    if (body) assert.equal(req.headers.origin, 'https://library.example.test')
    let response
    if (req.url === '/api/v1/manage/sources' && req.method === 'GET') response = { sources }
    else if (req.url === '/api/v1/manage/sources' && req.method === 'POST') {
      response = { id: `src_${sources.length}`, version: 1, ...body }
      sources.push(response)
    } else if (req.url.startsWith('/api/v1/manage/sources/') && req.method === 'PUT') {
      response = sources.find(s => s.id === req.url.split('/').at(-1))
      assert.equal(body.expectedVersion, response.version)
      Object.assign(response, { enabled: body.enabled, config: body.config, version: response.version + 1 })
    } else if (req.url === '/api/v1/manage/jobs' && req.method === 'POST') {
      assert.equal(body.kind, 'source-check')
      const source = sources.find(s => s.id === body.input.sourceId)
      const name = source.config.name
      const id = `job_${jobs.size}`
      jobs.set(id, { id, status: 'completed', result: { sourceCheck: {
        sourceId: source.id, version: source.version, passed: name !== 'failed', error: name === 'failed' ? 'HTTP 403' : undefined,
        title: name === 'wrong-book' ? '另一本书' : '样本书', author: '样本作者', chapters: 12, sampleWords: 2000,
      } } })
      response = { id, status: 'queued' }
    } else if (req.url.startsWith('/api/v1/manage/jobs/') && req.method === 'GET') response = jobs.get(req.url.split('/').at(-1))
    else { res.writeHead(404); res.end('{}'); return }
    res.writeHead(200, { 'Content-Type': 'application/json' })
    res.end(JSON.stringify(response))
  }).listen(0, '127.0.0.1')
  t.after(() => { server.closeAllConnections(); server.close() })
  await once(server, 'listening')
  const packPath = join(dir, 'pack.json'), reportPath = join(dir, 'report.json')
  const pack = { format: 'personal-library/source-pack@1', sources: ['good', 'failed', 'wrong-book', 'conflict'].map(name => ({
    config: config(name), sample: { url: 'https://example.com/book', title: '样本书', author: '样本作者' },
    provenance: { repository: 'https://github.com/example/sources', revision: 'fixture', path: 'sources.json' },
  })) }
  await writeFile(packPath, JSON.stringify(pack))
  const run = async () => {
    try {
      await promisify(execFile)(process.execPath, ['tools/m3/load-source-pack.mjs', '--pack', packPath, '--out', reportPath, '--base', `http://127.0.0.1:${server.address().port}`])
      return 0
    } catch (error) { return error.code }
  }
  assert.equal(await run(), 1)
  let report = JSON.parse(await readFile(reportPath, 'utf8'))
  assert.equal(report.passed, 1)
  assert.equal(report.failed, 3)
  assert.equal(sources.length, 4)
  assert.equal(sources.find(s => s.config.name === 'good').enabled, true)
  assert.equal(sources.find(s => s.config.name === 'failed').enabled, false)
  assert.equal(sources.find(s => s.config.name === 'wrong-book').enabled, false)
  assert.equal(conflict.enabled, true)
  assert.equal(conflict.version, 1)
  assert.equal(jobs.size, 3)
  assert.match(report.sources.find(s => s.name === 'failed').error, /403/)
  assert.match(report.sources.find(s => s.name === 'conflict').error, /different rules/)
  // A second import reuses the same source; server-filled optional zero fields
  // do not turn a semantically identical config into a name conflict.
  sources.find(s => s.config.name === 'good').config.book.author = { selector: '' }
  pack.sources = [pack.sources[0]]
  await writeFile(packPath, JSON.stringify(pack))
  assert.equal(await run(), 0)
  report = JSON.parse(await readFile(reportPath, 'utf8'))
  assert.equal(report.passed, 1)
  assert.equal(sources.length, 4)
})
