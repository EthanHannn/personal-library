// Small mixed read benchmark for the homepage/shelf increment, not M4 acceptance.
import { spawnSync } from 'node:child_process'
import { readFile, writeFile } from 'node:fs/promises'
import { parseArgs } from 'node:util'

const { values } = parseArgs({ options: { base: { type: 'string' }, container: { type: 'string' }, fixture: { type: 'string' }, out: { type: 'string' }, seconds: { type: 'string', default: '30' }, catalog: { type: 'boolean', default: false } } })
const seconds = Number(values.seconds)
if (!values.base || new URL(values.base).hostname !== '127.0.0.1' || !values.container || !values.fixture || !values.out || !Number.isInteger(seconds) || seconds < 1 || seconds > 1800) throw new Error('Required: loopback --base, --container, --fixture, --out, --seconds 1..1800')
const fixture = JSON.parse(await readFile(values.fixture, 'utf8'))
if (fixture.synthetic !== true) throw new Error('Synthetic fixtures only')
function docker(args) {
  const result = spawnSync('docker', args, { encoding: 'utf8', windowsHide: true, timeout: 10000 })
  if (result.status !== 0) throw new Error(result.stderr || result.error?.message || 'Docker failed')
  return JSON.parse(result.stdout)
}
const inspect = docker(['inspect', values.container])[0]
const metrics = () => docker(['exec', values.container, '/library', 'metrics'])
const paths = values.catalog ? {
  catalogSearch: '/api/v1/books?page=200&search=合&sortBy=score',
  catalogFiltered: '/api/v1/books?page=1&tag1=科幻&minScore=7.5&minWords=500001&maxWords=2000000&onlyContent=1',
  catalogAuthor: '/api/v1/books?page=1&author=合成作者%207&sortBy=words',
  catalogCategory: '/api/v1/books?page=8&tag=武侠&sortBy=popularity',
  categories: '/api/v1/categories',
} : { home: '/api/v1/home', shelf: '/api/v1/shelf?limit=24', reading: '/api/v1/reading?limit=24', detail: '/api/v1/books/1', content: '/api/v1/books/1/chapters/3/content' }
const records = Object.fromEntries(Object.keys(paths).map(key => [key, []]))
const report = { synthetic: true, complete: false, startedAt: new Date().toISOString(), fixture,
  measurement: `five concurrent loopback requests per round; ${values.catalog ? 'deep-page Chinese single-character search/combined filters/author/category/category counts' : 'home/shelf/recent/detail/content'}; no writes or background jobs`,
  paths,
  cache: 'OS page cache uncontrolled; first round included',
  runtime: { image: inspect.Image, revision: inspect.Config.Labels?.['org.opencontainers.image.revision'], memoryLimitBytes: inspect.HostConfig.Memory, nanoCPUs: inspect.HostConfig.NanoCpus },
  metrics: [metrics()], queries: {}, errors: [],
}
const start = performance.now()
let nextMetric = start + 10000
try {
  while (performance.now() - start < seconds * 1000) {
    await Promise.all(Object.entries(paths).map(async ([key, path]) => {
      const at = performance.now()
      const response = await fetch(new URL(path, values.base), { headers: { 'Remote-User': 'owner' }, signal: AbortSignal.timeout(5000) })
      const bytes = await response.arrayBuffer()
      if (!response.ok) throw new Error(`${path}: ${response.status}`)
      const json = JSON.parse(new TextDecoder().decode(bytes))
      if (key === 'home' && (!json.featured || json.featured.length > 5)) throw new Error('Unexpected homepage payload')
      if (key === 'shelf' && (!json.entries || json.entries.length > 24)) throw new Error('Unbounded shelf payload')
      if (key.startsWith('catalog') && (!json.books || json.books.length > 24 || !json.pagination)) throw new Error('Unexpected catalog payload')
      records[key].push({ ms: performance.now() - at, bytes: bytes.byteLength })
    }))
    if (performance.now() >= nextMetric) { report.metrics.push(metrics()); nextMetric = performance.now() + 10000 }
    await new Promise(resolve => setTimeout(resolve, 250))
  }
  report.complete = true
} catch (error) {
  report.errors.push(error.message)
  process.exitCode = 1
} finally {
  report.elapsedSeconds = (performance.now() - start) / 1000
  report.metrics.push(metrics())
  for (const [key, samples] of Object.entries(records)) {
    const times = samples.map(sample => sample.ms).sort((a, b) => a - b)
    const percentile = value => times[Math.max(0, Math.ceil(times.length * value) - 1)] ?? null
    report.queries[key] = { count: times.length, p50Ms: percentile(.5), p95Ms: percentile(.95), maxMs: times.at(-1) ?? null, meanBytes: samples.length ? samples.reduce((n, sample) => n + sample.bytes, 0) / samples.length : null }
  }
  report.finishedAt = new Date().toISOString()
  await writeFile(values.out, JSON.stringify(report, null, 2) + '\n', { flag: 'wx' })
  console.log(JSON.stringify({ complete: report.complete, errors: report.errors, queries: report.queries, finalMetrics: report.metrics.at(-1) }))
}
