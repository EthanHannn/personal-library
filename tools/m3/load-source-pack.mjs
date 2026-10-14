// Load reviewed, native source rules into a local library and verify live samples.
// Upstream Legado/so-novel files are not executable or directly importable here.
import { readFile, writeFile, mkdir } from 'node:fs/promises'
import { dirname, resolve } from 'node:path'
import { parseArgs } from 'node:util'
import { setTimeout as delay } from 'node:timers/promises'

const { values } = parseArgs({ options: {
  pack: { type: 'string' }, out: { type: 'string' },
  base: { type: 'string', default: 'http://127.0.0.1:16871' },
  user: { type: 'string', default: 'owner' },
  origin: { type: 'string', default: 'https://library.example.test' },
} })
if (!values.pack || !values.out) throw Error('Usage: node tools/m3/load-source-pack.mjs --pack <reviewed.json> --out <report.json> [--base http://127.0.0.1:16871]')
if (resolve(values.pack) === resolve(values.out)) throw Error('The report must not overwrite the source pack')
const base = new URL(values.base)
if (base.protocol !== 'http:' || base.hostname !== '127.0.0.1' || base.username || base.password || base.pathname !== '/' || base.search || base.hash) throw Error('Use an isolated library on http://127.0.0.1; this tool sends a synthetic reader header')
const raw = await readFile(values.pack, 'utf8')
if (Buffer.byteLength(raw) > 2 * 1024 * 1024) throw Error('Pack exceeds 2 MiB')
const pack = JSON.parse(raw)
if (pack.format !== 'personal-library/source-pack@1' || !Array.isArray(pack.sources) || !pack.sources.length || pack.sources.length > 49) throw Error('Expected 1–49 reviewed native sources, not a Legado or so-novel file')
const names = new Set()
for (const entry of pack.sources) {
  const { config, sample, provenance } = entry
  if (!config?.name || !['html', 'json'].includes(config.kind) || names.has(config.name)) throw Error('Source names must be unique and rules must use html/json')
  names.add(config.name)
  if (!sample?.url || !sample.title || !sample.author || (config.search && !sample.query)) throw Error(`${config.name}: sample URL, expected title/author and search query (when supported) are required`)
  if (!provenance?.repository || !provenance.revision || !provenance.path) throw Error(`${config.name}: record the upstream repository, revision and path`)
}

// Go returns omitted optional fields as zero values. Compare their meaning,
// without overwriting an existing source with a different reviewed rule set.
function canonical(value) {
  if (Array.isArray(value)) return value.map(canonical)
  if (value && typeof value === 'object') return Object.fromEntries(Object.keys(value).sort().flatMap(key => {
    const child = canonical(value[key])
    return child == null || child === '' || child === false || (typeof child === 'object' && !Object.keys(child).length) ? [] : [[key, child]]
  }))
  return value
}
const identity = value => String(value ?? '').normalize('NFKC').replace(/\s+/gu, '').toLowerCase()
async function request(path, method = 'GET', body) {
  const response = await fetch(new URL(path, base), {
    method, redirect: 'error', signal: AbortSignal.timeout(15000),
    headers: { 'Remote-User': values.user, ...(body ? { Origin: values.origin, 'Content-Type': 'application/json' } : {}) },
    body: body ? JSON.stringify(body) : undefined,
  })
  const data = await response.json()
  if (!response.ok) throw Error(`${method} ${path}: ${response.status} ${JSON.stringify(data)}`)
  return data
}
async function check(source, sample) {
  const job = await request('/api/v1/manage/jobs', 'POST', { kind: 'source-check', input: { sourceId: source.id, sourceVersion: source.version, url: sample.url, query: sample.query ?? '' } })
  const deadline = Date.now() + 300000
  while (Date.now() < deadline) {
    const latest = await request(`/api/v1/manage/jobs/${encodeURIComponent(job.id)}`)
    if (!['queued', 'running'].includes(latest.status)) return latest
    await delay(1000)
  }
  await request(`/api/v1/manage/jobs/${encodeURIComponent(job.id)}/cancel`, 'POST', {})
  throw Error(`Sample check ${job.id} exceeded 5 minutes and was cancelled`)
}
const report = { format: pack.format, startedAt: new Date().toISOString(), base: base.origin, sources: [] }
await mkdir(dirname(values.out), { recursive: true })
const checkpoint = () => writeFile(values.out, JSON.stringify(report, null, 2) + '\n')
let { sources } = await request('/api/v1/manage/sources')
for (const entry of pack.sources) {
  const result = { name: entry.config.name, provenance: entry.provenance, sample: entry.sample, passed: false }
  report.sources.push(result)
  let source
  let matched = false
  try {
    const existing = sources.filter(s => s.config.name === entry.config.name && s.id !== 'qidian')
    if (existing.length > 1) throw Error('Multiple existing sources have this name; resolve them in the editor first')
    source = existing[0]
    if (source && JSON.stringify(canonical(source.config)) !== JSON.stringify(canonical(entry.config))) throw Error('An existing source has different rules; edit it explicitly before reloading this pack')
    matched = true
    if (!source) source = await request('/api/v1/manage/sources', 'POST', { config: entry.config, enabled: true, expectedVersion: 0 })
    else if (!source.enabled) source = await request(`/api/v1/manage/sources/${encodeURIComponent(source.id)}`, 'PUT', { config: source.config, enabled: true, expectedVersion: source.version })
    result.sourceId = source.id
    result.version = source.version
    await checkpoint()
    const job = await check(source, entry.sample)
    result.jobId = job.id
    result.jobStatus = job.status
    result.check = job.result?.sourceCheck
    const c = result.check
    result.passed = job.status === 'completed' && c?.passed === true && c.sourceId === source.id && c.version === source.version && identity(c.title) === identity(entry.sample.title) && identity(c.author) === identity(entry.sample.author) && c.chapters > 0 && c.sampleWords > 0 && (!entry.config.search || c.searchCount > 0)
    if (!result.passed) result.error = c?.error || (job.status !== 'completed' && job.message) || 'Sample metadata or check version does not match the reviewed expectations'
  } catch (error) { result.error = error.message }
  // Only touch the source whose rules matched this pack. Never disable another
  // existing rule set merely because an import name conflicted.
  if (matched && source?.enabled && !result.passed) {
    try {
      source = await request(`/api/v1/manage/sources/${encodeURIComponent(source.id)}`, 'PUT', { config: source.config, enabled: false, expectedVersion: source.version })
    } catch (error) { result.disableError = error.message }
  }
  if (source) result.saved = { id: source.id, version: source.version, enabled: source.enabled }
  await checkpoint()
  console.log(JSON.stringify({ name: result.name, passed: result.passed, error: result.error, saved: result.saved }))
}
report.finishedAt = new Date().toISOString()
report.passed = report.sources.filter(s => s.passed).length
report.failed = report.sources.length - report.passed
await checkpoint()
console.log(JSON.stringify({ passed: report.passed, failed: report.failed, report: values.out }))
if (report.failed) process.exitCode = 1
