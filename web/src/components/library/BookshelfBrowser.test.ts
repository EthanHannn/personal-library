import { mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { expect, it, vi } from 'vitest'
import type { Book, CollectionPage } from '../../api'
import { response } from '../../test/fixtures'
import BookshelfBrowser from './BookshelfBrowser.vue'
import BookDetail from '../book/BookDetail.vue'

const book: Book = { id: 42, title: '有进度的作品', author: '测试作者', tag1: '小说', tag2: '', wordCount: 10000, chapterCount: 9, score: 6, hasContent: true }
const entry = { book, inShelf: true, addedAt: 1000, progress: { chapterIdx: 3, percent: 0.4, updatedAt: 1000 } }
const page = (): CollectionPage => ({ entries: [entry], counts: { shelf: 1, reading: 1 }, nextCursor: null })

async function render(path = '/bookshelf') {
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/bookshelf', component: BookshelfBrowser }, { path: '/book/:id', component: BookDetail },
    { path: '/read/:id', component: { template: '<p>阅读器</p>' } }, { path: '/library', component: { template: '<p>书库</p>' } },
  ] })
  await router.push(path); await router.isReady()
  const wrapper = mount(defineComponent({ setup: () => () => h(RouterView) }), { attachTo: document.body, global: { plugins: [router] } })
  return { wrapper, router }
}

it('keeps a failed removal visible, retries DELETE and preserves the separate reading entry', async () => {
  let attempts = 0
  const fetch = vi.fn(async (path: string, options?: RequestInit) => {
    if (options?.method === 'DELETE') {
      expect(options.body).toBe('{}')
      return ++attempts === 1 ? response({ error: { message: '测试网络中断' } }, 503) : response({ inShelf: false })
    }
    return response(path.startsWith('/api/v1/reading') ? { ...page(), entries: [{ ...entry, inShelf: false }], counts: { shelf: 0, reading: 1 } } : page())
  })
  vi.stubGlobal('fetch', fetch)
  const { wrapper, router } = await render()
  try {
    await vi.waitFor(() => expect(wrapper.text()).toContain(book.title))
    expect(wrapper.find('.collection-actions a').attributes('href')).toBe('/read/42')
    await wrapper.findAll('button').find(b => b.text() === '移出书架')!.trigger('click')
    await vi.waitFor(() => expect(wrapper.text()).toContain('测试网络中断'))
    expect(wrapper.findAll('.collection-card')).toHaveLength(1)
    await wrapper.findAll('button').find(b => b.text() === '重试移出')!.trigger('click')
    await vi.waitFor(() => expect(wrapper.findAll('.collection-card')).toHaveLength(0))
    expect(wrapper.text()).toContain('阅读进度已保留')
    expect(document.activeElement).toBe(wrapper.get('h1').element)
    expect(attempts).toBe(2)
    await router.push('/bookshelf?view=reading')
    await vi.waitFor(() => expect(wrapper.findAll('.collection-card')).toHaveLength(1))
    expect(wrapper.text()).toContain('第 3 章 · 章内 40%')
    expect(wrapper.find('.collection-actions a').attributes('href')).toBe('/read/42')
    expect(wrapper.findAll('.shelf-tabs [aria-current="page"]')).toHaveLength(1)
    expect(wrapper.get('.shelf-tabs [aria-current="page"]').text()).toBe('最近阅读')
  } finally { wrapper.unmount() }
})

it('can collect an unread book without text and never creates progress on its behalf', async () => {
  const fetch = vi.fn(async (_path: string, options?: RequestInit) => options?.method === 'PUT'
    ? response({ inShelf: true })
    : response({ book: { ...book, hasContent: false, chapterCount: 0 }, inShelf: false, progress: null }))
  vi.stubGlobal('fetch', fetch)
  const { wrapper } = await render('/book/42')
  try {
    await vi.waitFor(() => expect(wrapper.text()).toContain('加入书架'))
    await wrapper.findAll('button').find(b => b.text() === '加入书架')!.trigger('click')
    await vi.waitFor(() => expect(wrapper.text()).toContain('已加入书架'))
    expect(wrapper.find('a[href="/read/42"]').exists()).toBe(false)
    expect(wrapper.get('.shelf-button').attributes('aria-pressed')).toBe('true')
    expect(fetch.mock.calls.some(([path]) => path.includes('/progress'))).toBe(false)
  } finally { wrapper.unmount() }
})

it('clears private collection data when a mutation discovers an expired session', async () => {
  vi.stubGlobal('fetch', vi.fn(async (_path: string, options?: RequestInit) => options?.method === 'DELETE'
    ? response({ error: { message: '登录已失效' } }, 401) : response(page())))
  const { wrapper } = await render()
  try {
    await vi.waitFor(() => expect(wrapper.text()).toContain(book.title))
    await wrapper.findAll('button').find(b => b.text() === '移出书架')!.trigger('click')
    await vi.waitFor(() => expect(wrapper.text()).toContain('重新登录'))
    expect(wrapper.findAll('.collection-card')).toHaveLength(0)
    expect(wrapper.text()).not.toContain(book.title)
    expect(wrapper.text()).not.toContain('已移出')
  } finally { wrapper.unmount() }
})

it('ignores a late collection response after switching lists and retains cursor navigation', async () => {
  let finish!: (response: Response) => void
  vi.stubGlobal('fetch', vi.fn((path: string) => path.startsWith('/api/v1/shelf')
    ? new Promise<Response>(resolve => { finish = resolve })
    : Promise.resolve(response({ ...page(), entries: [{ ...entry, book: { ...book, title: '最近在读' } }], nextCursor: 'test-cursor' }))))
  const { wrapper, router } = await render()
  try {
    await vi.waitFor(() => expect(finish).toBeTypeOf('function'))
    await router.push('/bookshelf?view=reading')
    await vi.waitFor(() => expect(wrapper.text()).toContain('最近在读'))
    finish(response(page()))
    await new Promise(resolve => setTimeout(resolve, 0))
    expect(wrapper.text()).not.toContain(book.title)
    const next = wrapper.findAll('.collection-pagination a').find(a => a.text().startsWith('下一页'))!
    expect(next.attributes('href')).toContain('view=reading')
    expect(next.attributes('href')).toContain('cursor=test-cursor')
  } finally { wrapper.unmount() }
})
