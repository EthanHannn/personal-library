import { mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { expect, it, vi } from 'vitest'
import type { CatalogBook, CatalogPage } from '../../api'
import { deferred, response } from '../../test/fixtures'
import CatalogBrowser from './CatalogBrowser.vue'

const book: CatalogBook = { id: 42, title: '星河', author: '甲/乙%中文?#', tag1: '奇幻', tag2: '仙侠', wordCount: 100000, chapterCount: 9, score: 9, hasContent: true, popularity: 100, xiancaoCount: 80 }
const categories = { categories: [
  { name: '奇幻', count: 40, subcategories: [{ name: '仙侠', count: 30 }] },
  { name: '科幻', count: 20, subcategories: [{ name: '太空', count: 20 }] },
] }
const page = (current = 1): CatalogPage => ({ books: [book], pagination: { page: current, limit: 24, total: 60, totalPages: 3 }, summary: { authors: 5, readable: 50 } })

async function render(path = '/library') {
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/library', component: CatalogBrowser },
    { path: '/category/:tag', component: CatalogBrowser, props: route => ({ kind: 'category', name: route.params.tag }) },
    { path: '/author/:name', component: CatalogBrowser, props: route => ({ kind: 'author', name: route.params.name }) },
    { path: '/book/:id', component: { template: '<p>详情</p>' } },
  ] })
  await router.push(path); await router.isReady()
  const wrapper = mount(defineComponent({ setup: () => () => h(RouterView) }), { attachTo: document.body, global: { plugins: [router] } })
  return { wrapper, router }
}
const params = (url: string) => new URL(url, 'https://library.test').searchParams

it('starts with high-score content and submits filters together with a fresh page', async () => {
  const fetch = vi.fn(async (path: string) => response(path === '/api/v1/categories' ? categories : page(Number(params(path).get('page')))))
  vi.stubGlobal('fetch', fetch)
  const { wrapper, router } = await render('/library?page=3')
  try {
    await vi.waitFor(() => expect(wrapper.findAll('.book-card')).toHaveLength(1))
    const initial = params(fetch.mock.calls[0]![0])
    expect(initial.get('minScore')).toBe('7.5'); expect(initial.get('onlyContent')).toBe('1')
    const calls = fetch.mock.calls.length
    await wrapper.get('#catalog-search').setValue(' 星 ')
    await wrapper.get('#catalog-sort').setValue('words')
    await wrapper.get('#catalog-words').setValue('500001,1000000')
    expect(fetch).toHaveBeenCalledTimes(calls)
    await wrapper.get('form').trigger('submit')
    await vi.waitFor(() => expect(router.currentRoute.value.query.search).toBe('星'))
    expect(router.currentRoute.value.query.page).toBeUndefined()
    const last = params(fetch.mock.calls.at(-1)![0])
    expect(last.get('page')).toBe('1'); expect(last.get('sortBy')).toBe('words')
    expect(last.get('minWords')).toBe('500001'); expect(last.get('maxWords')).toBe('1000000')
    await wrapper.get('.presets button:nth-child(2)').trigger('click')
    await vi.waitFor(() => expect(router.currentRoute.value.query.minScore).toBe('0'))
    expect(router.currentRoute.value.query.onlyContent).toBe('0')
    expect(router.currentRoute.value.query.search).toBe('星')
  } finally { wrapper.unmount() }
})

it('preserves filters through pagination, history navigation and a fresh render', async () => {
  vi.stubGlobal('fetch', vi.fn(async (path: string) => response(path === '/api/v1/categories' ? categories : page(Number(params(path).get('page'))))))
  const path = '/library?search=星&minScore=0&onlyContent=0&sortBy=latest'
  const { wrapper, router } = await render(path)
  let saved = ''
  try {
    await vi.waitFor(() => expect(wrapper.find('a[aria-label="第 2 页"]').exists()).toBe(true))
    await wrapper.get('a[aria-label="第 2 页"]').trigger('click')
    await vi.waitFor(() => expect(router.currentRoute.value.query.page).toBe('2'))
    expect(router.currentRoute.value.query.search).toBe('星')
    saved = router.currentRoute.value.fullPath
    await wrapper.get('#catalog-search').setValue('海')
    await wrapper.get('form').trigger('submit')
    await vi.waitFor(() => expect(router.currentRoute.value.query.search).toBe('海'))
    router.back()
    await vi.waitFor(() => expect((wrapper.get('#catalog-search').element as HTMLInputElement).value).toBe('星'))
    await vi.waitFor(() => expect(wrapper.get('[aria-current="page"]').text()).toBe('2'))
    expect(wrapper.findAll('.pagination [aria-current="page"]')).toHaveLength(1)
    expect((wrapper.get('#catalog-sort').element as HTMLSelectElement).value).toBe('latest')
  } finally { wrapper.unmount() }
  const fresh = await render(saved)
  try {
    await vi.waitFor(() => expect(fresh.wrapper.text()).toContain('第 2 / 3 页'))
    expect((fresh.wrapper.get('#catalog-search').element as HTMLInputElement).value).toBe('星')
    expect(fresh.wrapper.get('.presets button:nth-child(2)').attributes('aria-pressed')).toBe('true')
  } finally { fresh.wrapper.unmount() }
})

it('resets an unapplied draft even when the URL is already at its defaults', async () => {
  vi.stubGlobal('fetch', vi.fn(async (path: string) => response(path === '/api/v1/categories' ? categories : page())))
  const { wrapper } = await render()
  try {
    await wrapper.get('#catalog-search').setValue('尚未提交')
    await wrapper.get('.submit-actions .text-button').trigger('click')
    expect((wrapper.get('#catalog-search').element as HTMLInputElement).value).toBe('')
    expect((wrapper.get('#catalog-min-score').element as HTMLInputElement).value).toBe('7.5')
  } finally { wrapper.unmount() }
})

it('clears the old subcategory when selecting a different main category', async () => {
  const fetch = vi.fn(async (path: string) => response(path === '/api/v1/categories' ? categories : page()))
  vi.stubGlobal('fetch', fetch)
  const { wrapper, router } = await render('/library?tag1=奇幻&tag2=仙侠')
  try {
    await vi.waitFor(() => expect(wrapper.get('#catalog-tag1').text()).toContain('科幻'))
    await wrapper.get('#catalog-tag1').setValue('科幻')
    expect((wrapper.get('#catalog-tag2').element as HTMLSelectElement).value).toBe('')
    expect(wrapper.get('#catalog-tag2').text()).not.toContain('仙侠')
    await wrapper.get('form').trigger('submit')
    await vi.waitFor(() => expect(router.currentRoute.value.query.tag1).toBe('科幻'))
    expect(router.currentRoute.value.query.tag2).toBeUndefined()
  } finally { wrapper.unmount() }
})

it('follows encoded author/category links without double decoding or nested anchors', async () => {
  const fetch = vi.fn(async (path: string) => response(path === '/api/v1/categories' ? categories : page()))
  vi.stubGlobal('fetch', fetch)
  const { wrapper, router } = await render()
  try {
    await vi.waitFor(() => expect(wrapper.find('.author a').exists()).toBe(true))
    expect(wrapper.find('a a').exists()).toBe(false)
    await wrapper.get('.author a').trigger('click')
    await vi.waitFor(() => expect(wrapper.get('h1').text()).toBe(book.author))
    expect(params(fetch.mock.calls.at(-1)![0]).get('author')).toBe(book.author)
    await router.push(`/author/${encodeURIComponent('%2F')}?author=wrong`)
    await vi.waitFor(() => expect(params(fetch.mock.calls.at(-1)![0]).get('author')).toBe('%2F'))
    expect(wrapper.get('h1').text()).toBe('%2F')
    expect(wrapper.text()).not.toContain('限定范围：wrong')
    await vi.waitFor(() => expect(wrapper.find('.category a').exists()).toBe(true))
    await wrapper.get('.category a').trigger('click')
    await vi.waitFor(() => expect(wrapper.get('h1').text()).toBe('奇幻'))
    expect(params(fetch.mock.calls.at(-1)![0]).get('tag')).toBe('奇幻')
  } finally { wrapper.unmount() }
})

it('keeps the archive scope when clearing an empty result and includes unrated holdings', async () => {
  const fetch = vi.fn(async (path: string) => response(path === '/api/v1/categories' ? categories : { ...page(), books: [], pagination: { page: 1, limit: 24, total: 0, totalPages: 0 } }))
  vi.stubGlobal('fetch', fetch)
  const { wrapper, router } = await render('/category/仙侠?search=不存在&page=2')
  try {
    await vi.waitFor(() => expect(wrapper.find('.empty-state button').exists()).toBe(true))
    await wrapper.get('.empty-state button').trigger('click')
    await vi.waitFor(() => expect(router.currentRoute.value.query.search).toBeUndefined())
    const query = params(fetch.mock.calls.at(-1)![0])
    expect(query.get('tag')).toBe('仙侠'); expect(query.get('minScore')).toBe('0'); expect(query.get('onlyContent')).toBe('0')
    expect(query.get('page')).toBe('1')
  } finally { wrapper.unmount() }
})

it('ignores an old response after changing search and aborts it', async () => {
  const old = deferred<Response>()
  let oldSignal: AbortSignal | undefined
  vi.stubGlobal('fetch', vi.fn(async (path: string, options: RequestInit) => {
    if (path === '/api/v1/categories') return response(categories)
    if (params(path).get('search') === '旧') { oldSignal = options.signal as AbortSignal; return old.promise }
    return response({ ...page(), books: [{ ...book, title: '新结果' }] })
  }))
  const { wrapper, router } = await render('/library?search=旧')
  try {
    await router.push('/library?search=新')
    await vi.waitFor(() => expect(wrapper.text()).toContain('新结果'))
    old.resolve(response({ ...page(), books: [{ ...book, title: '过时结果' }] }))
    expect(oldSignal?.aborted).toBe(true)
    await Promise.resolve()
    expect(wrapper.text()).not.toContain('过时结果')
  } finally { wrapper.unmount() }
})

it('retries failures and clears catalog and category data when the session expires', async () => {
  let status = 503
  vi.stubGlobal('fetch', vi.fn(async (path: string) => path === '/api/v1/categories' ? response(categories)
    : status === 200 ? response(page()) : response({ error: { message: '测试服务暂时不可用' } }, status)))
  const { wrapper, router } = await render()
  try {
    await vi.waitFor(() => expect(wrapper.find('[role="alert"]').exists()).toBe(true))
    status = 200
    await wrapper.get('[role="alert"] button').trigger('click')
    await vi.waitFor(() => expect(wrapper.findAll('.book-card')).toHaveLength(1))
    status = 401
    await router.push('/library?search=失效')
    await vi.waitFor(() => expect(wrapper.text()).toContain('重新登录'))
    expect(wrapper.find('.book-card').exists()).toBe(false)
    expect(wrapper.find('.category-directory').exists()).toBe(false)
  } finally { wrapper.unmount() }
})
