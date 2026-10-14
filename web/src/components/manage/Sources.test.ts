import { flushPromises, mount } from '@vue/test-utils'
import { expect, it, vi } from 'vitest'
import { deferred, response } from '../../test/fixtures'
import type { BookSource, SourceConfig, SearchCandidate } from '../../sources'
import SourceEditor from './SourceEditor.vue'
import SourceManager from './SourceManager.vue'
import SourceSearchForm from './SourceSearchForm.vue'
import SearchResults from './SearchResults.vue'
import SourceCheckForm from './SourceCheckForm.vue'

const config: SourceConfig = { name: '测试来源', group: '男频', kind: 'html', baseUrl: 'https://example.com/', allowedHosts: ['example.com'], charset: 'utf-8', intervalMs: 5000, search: { url: 'https://example.com/search?q={query}', items: '.book', title: { selector: 'h2' }, author: { selector: '.author' }, link: { selector: 'a', attr: 'href' } }, book: { title: { selector: 'h1' }, author: { selector: '.author' } }, toc: { items: '.toc a', title: { selector: '.' }, link: { selector: '.', attr: 'href' } }, chapter: { title: { selector: 'h1' }, content: { selector: '#content' } } }
const source: BookSource = { id: 'test-source', version: 4, enabled: true, config, checkedAt: 0, checkVersion: 0, check: {}, cooldownUntil: 0 }
const templates = [{ id: 'test', note: '示例', config }]

it('adds native source rules and preserves server validation errors and draft for correction', async () => {
  let attempt = 0
  const fetch = vi.fn(async () => ++attempt === 1 ? response({ error: { message: 'CSS 选择器无效' } }, 400) : response(source))
  vi.stubGlobal('fetch', fetch)
  const wrapper = mount(SourceEditor, { props: { templates } })
  try {
    await wrapper.findAll('input')[0]!.setValue('我的仙侠书源')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(wrapper.text()).toContain('CSS 选择器无效')
    expect(wrapper.emitted('saved')).toBeUndefined()
    expect(wrapper.findAll('input')[0]!.element.value).toBe('我的仙侠书源')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(wrapper.emitted('saved')).toHaveLength(1)
    const body = JSON.parse((fetch.mock.calls[1] as unknown as [string, RequestInit])[1].body as string)
    expect(body).toMatchObject({ config: { name: '我的仙侠书源', chapter: { content: { selector: '#content' } } }, enabled: true, expectedVersion: 0 })
  } finally { wrapper.unmount() }
})

it('rejects unsupported source imports and malformed rule JSON before sending', async () => {
  const fetch = vi.fn(); vi.stubGlobal('fetch', fetch)
  const wrapper = mount(SourceEditor, { props: { source, templates } })
  try {
    await wrapper.get('textarea[placeholder]').setValue('{"bookSourceName":"旧阅读书源","ruleBookInfo":{}}')
    await wrapper.findAll('button').find(b => b.text() === '读取配置到表单')!.trigger('click')
    expect(wrapper.text()).toContain('不能直接导入')
    await wrapper.get('textarea.rule-input').setValue('{')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
    expect(fetch).not.toHaveBeenCalled()
  } finally { wrapper.unmount() }
})

it('selects only enabled searchable sources and searches without auto collection', async () => {
  const fetch = vi.fn(async () => response({ id: 'search-job', kind: 'search' }, 202)); vi.stubGlobal('fetch', fetch)
  const wrapper = mount(SourceSearchForm, { props: { sources: [source, { ...source, id: 'disabled', enabled: false }, { ...source, id: 'url-only', config: { ...config, search: undefined } }] } })
  try {
    expect(wrapper.findAll('input[type="checkbox"]')).toHaveLength(1)
    await wrapper.get('input[placeholder]').setValue('测试作品')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    const body = JSON.parse((fetch.mock.calls[0] as unknown as [string, RequestInit])[1].body as string)
    expect(body).toEqual({ kind: 'search', input: { query: '测试作品', author: '', sourceIds: [source.id] } })
    expect(wrapper.emitted('created')).toHaveLength(1)
    expect(fetch).toHaveBeenCalledTimes(1)
  } finally { wrapper.unmount() }
})

it('groups exact identities and keeps different or unknown authors separate, requiring explicit source selection', async () => {
  const base: SearchCandidate = { title: '测试小说', author: '作者甲', tag1: '玄幻', tag2: '', intro: '', sourceId: source.id, sourceName: '来源甲', sourceVersion: 4, url: 'https://example.com/book', latest: '', needsReview: false }
  const candidates = [base, { ...base, sourceId: 'second', sourceName: '来源乙' }, { ...base, author: '作者乙', url: 'https://example.com/other' }, { ...base, author: '', url: 'https://example.com/unknown', needsReview: true }]
  const fetch = vi.fn(async () => response({ id: 'collect-job' }, 202)); vi.stubGlobal('fetch', fetch)
  const wrapper = mount(SearchResults, { props: { candidates, sources: [{ id: 'offline', name: '坏源', count: 0, error: '来源返回 HTTP 500' }] } })
  try {
    expect(wrapper.findAll('.candidate-group')).toHaveLength(3)
    expect(wrapper.text()).toContain('来源返回 HTTP 500')
    expect(fetch).not.toHaveBeenCalled()
    await wrapper.findAll('button')[1]!.trigger('click')
    expect(wrapper.get('.selection').text()).toContain('来源乙')
    await wrapper.get('input[type="number"]').setValue('8')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    const body = JSON.parse((fetch.mock.calls[0] as unknown as [string, RequestInit])[1].body as string)
    expect(body).toMatchObject({ kind: 'collect', input: { sourceId: 'second', sourceVersion: 4, url: base.url, title: base.title, author: base.author, maxChapters: 8 } })
  } finally { wrapper.unmount() }
})

it('hides private source data and in-flight search results when authentication expires', async () => {
  const wait = deferred<Response>()
  const fetch = vi.fn(async (_path: string, options?: RequestInit) => options?.method === 'PUT' ? wait.promise : response({ sources: [source], templates })); vi.stubGlobal('fetch', fetch)
  const wrapper = mount(SourceManager)
  try {
    await flushPromises(); expect(wrapper.text()).toContain('测试来源')
    await wrapper.findAll('button').find(b => b.text() === '停用')!.trigger('click')
    wait.resolve(response({ error: { message: '失效' } }, 401)); await flushPromises()
    expect(wrapper.text()).toContain('重新登录')
    expect(wrapper.text()).not.toContain('测试来源')
    expect(wrapper.text()).not.toContain('添加书源')
  } finally { wrapper.unmount() }
})

it('checks a sample at the selected rule version without creating an import', async () => {
  const fetch = vi.fn(async () => response({ id: 'check-job' }, 202)); vi.stubGlobal('fetch', fetch)
  const wrapper = mount(SourceCheckForm, { props: { source } })
  try {
    await wrapper.get('input[type="url"]').setValue('https://example.com/book')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(JSON.parse((fetch.mock.calls[0] as unknown as [string, RequestInit])[1].body as string)).toEqual({ kind: 'source-check', input: { sourceId: source.id, sourceVersion: 4, url: 'https://example.com/book', query: '' } })
  } finally { wrapper.unmount() }
})
