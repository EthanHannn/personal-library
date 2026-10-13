// Loopback API measurements, not a browser or nginx/Authelia benchmark.
// Uses only synthetic fixtures. Node 20+ and Docker CLI are required.
import { spawnSync } from 'node:child_process'
import { mkdir, readFile, writeFile } from 'node:fs/promises'
import { dirname } from 'node:path'
import { parseArgs } from 'node:util'

const { values } = parseArgs({ options: {
  target: { type: 'string' }, base: { type: 'string' }, container: { type: 'string' },
  out: { type: 'string' }, fixture: { type: 'string' },
  seconds: { type: 'string', default: '1800' }, samples: { type: 'string', default: '300' },
  concurrency: { type: 'string', default: '1' },
  collector: { type: 'string', default: '/library' },
} })
if (!['legacy', 'go'].includes(values.target) || !values.base || !values.container || !values.out || !values.fixture)
  throw new Error('Required: --target legacy|go --base URL --container NAME --out FILE --fixture SUMMARY')
const base = new URL(values.base)
if (base.protocol !== 'http:' || !['127.0.0.1', 'localhost', '[::1]'].includes(base.hostname))
  throw new Error('Benchmarks must target an isolated loopback server')
const seconds = Number(values.seconds)
const samples = Number(values.samples)
const concurrency = Number(values.concurrency)
if (!Number.isInteger(seconds) || seconds < 0 || seconds > 7200 || !Number.isInteger(samples) || samples < 1 || samples > 10000)
  throw new Error('Invalid duration or sample count')
if (!Number.isInteger(concurrency) || concurrency < 1 || concurrency > 8)
  throw new Error('Concurrency must be between 1 and 8')
const fixture = JSON.parse(await readFile(values.fixture, 'utf8'))
if (fixture.synthetic !== true) throw new Error('Only generated synthetic fixtures are supported')
await mkdir(dirname(values.out), { recursive: true })

function docker(args) {
  const result = spawnSync('docker', args, { encoding: 'utf8', windowsHide: true, timeout: 15000 })
  if (result.status !== 0) throw new Error(result.stderr || result.error?.message || 'docker failed')
  return result.stdout.trim()
}
function metrics(label) {
  return { label, ...JSON.parse(docker(['exec', values.container, values.collector, 'metrics'])) }
}
function route(chapter, book = 1) {
  return values.target === 'legacy'
    ? `/api/books/${book}/content?chapter=${chapter}`
    : `/api/v1/books/${book}/chapters/${chapter}/content`
}
async function request(path) {
  const started = performance.now()
  const response = await fetch(new URL(path, base), {
    headers: { 'Remote-User': 'owner' }, signal: AbortSignal.timeout(15000),
  })
  const bytes = await response.arrayBuffer()
  if (response.status !== 200) throw new Error(`${path}: HTTP ${response.status}: ${new TextDecoder().decode(bytes).slice(0, 300)}`)
  const body = JSON.parse(new TextDecoder().decode(bytes))
  const text = body.text ?? body.content
  return { ms: performance.now() - started, bytes: bytes.byteLength,
    textBytes: typeof text === 'string' ? Buffer.byteLength(text) : undefined,
    chapterCount: body.chapters?.length }
}
function summary(records) {
  const times = records.map(r => r.ms).sort((a, b) => a - b)
  const percentile = p => times[Math.max(0, Math.ceil(times.length * p) - 1)] ?? null
  return { count: records.length, p50Ms: percentile(0.50), p95Ms: percentile(0.95), p99Ms: percentile(0.99),
    maxMs: times.at(-1) ?? null, meanBytes: records.length ? records.reduce((n, r) => n + r.bytes, 0) / records.length : null }
}
const inspect = JSON.parse(docker(['inspect', values.container]))[0]
const report = {
  target: values.target, startedAt: new Date().toISOString(), complete: false,
  measurement: 'isolated loopback API; synthetic data; no browser/portal overhead',
  cache: 'application first request recorded separately; OS page cache uncontrolled; no global cache eviction',
  collector: 'same static metrics helper in each container; PID 1 RSS excludes helper, cgroup includes it',
  runtime: { clientNode: process.version, image: inspect.Image,
    revision: inspect.Config.Labels?.['org.opencontainers.image.revision'],
    memoryLimitBytes: inspect.HostConfig.Memory, nanoCPUs: inspect.HostConfig.NanoCpus },
  fixture, concurrency, plannedSoakSeconds: seconds, metrics: [], queries: {}, errors: [],
}
try {
  report.metrics.push(metrics('idle'))
  report.firstRead = await request(route(1))
  report.metrics.push(metrics('after first large-file chapter'))
  const chapterRecords = []
  for (let i = 0; i < samples; i += concurrency)
    chapterRecords.push(...await Promise.all(Array.from({ length: Math.min(concurrency, samples - i) },
      (_, k) => request(route(1 + (((i + k) * 137) % fixture.largeFileChapters))))))
  report.queries.chapter = summary(chapterRecords)
  const catalogRecords = []
  for (let i = 0; i < Math.min(samples, 30); i++) {
    const path = values.target === 'legacy' ? '/api/books/1/chapters'
      : `/api/v1/books/1/chapters?after=${(i * 137) % (fixture.largeFileChapters - 200)}&limit=100`
    catalogRecords.push(await request(path))
  }
  report.queries.directory = { ...summary(catalogRecords), returnedRows: catalogRecords[0]?.chapterCount,
    note: 'legacy returns the entire directory; Go requests a 100-row window' }
  report.encodingSamples = { bom: await request(route(1, 2)), gb18030: await request(route(1, 3)),
    longLine: await request(route(1, 4)) }
  report.metrics.push(metrics('after query series and long line'))
  console.log(JSON.stringify({ phase: 'query series', target: values.target, chapter: report.queries.chapter,
    directory: report.queries.directory, metrics: report.metrics.at(-1) }))
  const soakStarted = performance.now()
  let nextMetric = soakStarted + 10000
  const soak = []
  let i = 0
  while (performance.now() - soakStarted < seconds * 1000) {
    soak.push(...await Promise.all(Array.from({ length: concurrency },
      () => request(route(1 + ((i++ * 137) % fixture.largeFileChapters))))))
    if (performance.now() >= nextMetric) {
      report.metrics.push(metrics(`soak ${Math.round((performance.now() - soakStarted) / 1000)}s`))
      nextMetric = performance.now() + 10000
      if (report.metrics.length % 6 === 0) console.log(JSON.stringify(report.metrics.at(-1)))
    }
    await new Promise(resolve => setTimeout(resolve, 250))
  }
  report.soak = { ...summary(soak), elapsedSeconds: (performance.now() - soakStarted) / 1000 }
  report.metrics.push(metrics('final'))
  report.complete = true
} catch (error) {
  report.errors.push(error.message)
  process.exitCode = 1
} finally {
  report.finishedAt = new Date().toISOString()
  await writeFile(values.out, JSON.stringify(report, null, 2) + '\n', { encoding: 'utf8', flag: 'wx' })
  console.log(JSON.stringify({ report: values.out, complete: report.complete, errors: report.errors, soak: report.soak }))
}
