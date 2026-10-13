// Synthetic, loopback-only browser harness. This is not Authelia and must never
// be deployed. It exercises real HTTP failures without modifying application JS.
import http from 'node:http'
import { parseArgs } from 'node:util'

const { values } = parseArgs({ options: { port: { type: 'string', default: '16872' }, upstream: { type: 'string', default: 'http://127.0.0.1:16871' } } })
const origin = `http://127.0.0.1:${values.port}`
const upstream = new URL(values.upstream)
if (upstream.hostname !== '127.0.0.1') throw new Error('Synthetic gateway only accepts a loopback upstream')
const json = (res, status, message) => { res.writeHead(status, { 'Content-Type': 'application/json', 'Cache-Control': 'private, no-store' }); res.end(JSON.stringify({ error: { code: 'synthetic_failure', message } })) }

http.createServer((req, res) => {
  const url = new URL(req.url, origin)
  const cookies = Object.fromEntries((req.headers.cookie ?? '').split(';').map(p => p.trim().split('=')))
  if (url.pathname.startsWith('/__test__/')) {
    if (req.method === 'POST' && req.headers.origin !== origin) return json(res, 403, 'Wrong test origin')
    if (url.pathname === '/__test__/session' && req.method === 'POST') {
      const state = url.searchParams.get('state') === 'expired' ? 'expired' : 'valid'
      res.setHeader('Set-Cookie', `test-session=${state}; Path=/; SameSite=Strict; HttpOnly`)
      res.writeHead(204); return res.end()
    }
    if (url.pathname === '/__test__/offline' && req.method === 'POST') {
      res.setHeader('Set-Cookie', `test-offline=${url.searchParams.get('state') === 'on' ? 'on' : 'off'}; Path=/; SameSite=Strict; HttpOnly`)
      res.writeHead(204); return res.end()
    }
    if (url.pathname === '/__test__/login') {
      const raw = url.searchParams.get('next') ?? '/library'
      let next = '/'
      try {
        const requested = new URL(raw, origin)
        if (requested.origin === origin && /^\/(?:library|bookshelf|book\/\d+|read\/\d+)?$/.test(requested.pathname)) next = requested.pathname + requested.search
      } catch { /* Keep a fixed same-origin fallback for malformed return URLs. */ }
      if (req.method === 'POST') {
        res.writeHead(303, { 'Set-Cookie': 'test-session=valid; Path=/; SameSite=Strict; HttpOnly', Location: next }); return res.end()
      }
      res.writeHead(200, { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' })
      return res.end('<!doctype html><html lang="zh-CN"><title>书屋本地测试登录</title><main><h1>合成测试会话</h1><form method="post"><button>登录测试书屋</button></form></main></html>')
    }
    return json(res, 404, 'Unknown harness command')
  }
  if (cookies['test-session'] !== 'valid') {
    if (url.pathname.startsWith('/api/')) return json(res, 401, '测试会话已失效')
    res.writeHead(303, { Location: `/__test__/login?next=${encodeURIComponent(url.pathname + url.search)}` }); return res.end()
  }
  if (cookies['test-offline'] === 'on' && url.pathname.startsWith('/api/')) return json(res, 503, '测试网络暂时不可用')
  const headers = { ...req.headers, host: upstream.host, 'remote-user': 'owner' }
  if (headers.origin === origin) headers.origin = 'https://library.example.test'
  const proxy = http.request(new URL(req.url, upstream), { method: req.method, headers }, response => { res.writeHead(response.statusCode, response.headers); response.pipe(res) })
  proxy.on('error', () => { if (!res.headersSent) json(res, 503, '测试服务未启动'); else res.destroy() })
  req.pipe(proxy)
}).listen(Number(values.port), '127.0.0.1', () => console.log(`Synthetic browser gateway: ${origin}`))
