import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { expect, it, vi } from 'vitest'
import type { BookState, VoteType } from '../../api'
import { deferred, response, voting } from '../../test/fixtures'
import BookDetail from './BookDetail.vue'

function detail(type: VoteType | null = null, id = 42): BookState {
  return { book: { id, title: `作品 ${id}`, author: '作者', tag1: '小说', tag2: '', wordCount: 0, chapterCount: 0, hasContent: false, score: 8.5 }, inShelf: false, progress: null, vote: voting(type, id) }
}
async function render() {
  const router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/book/:id', component: BookDetail }, { path: '/:pathMatch(.*)*', component: { template: '<p>其他页面</p>' } },
  ] })
  await router.push('/book/42'); await router.isReady()
  const wrapper = mount(BookDetail, { global: { plugins: [router] } })
  await vi.waitFor(() => expect(wrapper.text()).toContain('我的评价'))
  return { wrapper, router }
}

it('saves all five choices on a book without text and restores the selection after reopening', async () => {
  let selected: VoteType | null = null
  const fetch = vi.fn(async (_path: string, options?: RequestInit) => {
    if (options?.method === 'PUT') { selected = JSON.parse(options.body as string).type; return response(voting(selected)) }
    return response(detail(selected))
  })
  vi.stubGlobal('fetch', fetch)
  const { wrapper } = await render()
  try {
    expect(wrapper.text()).toContain('尚未评价')
    for (const label of ['仙草', '粮草', '干草', '枯草', '毒草']) {
      await wrapper.findAll('button').find(b => b.text().startsWith(label))!.trigger('click')
      await vi.waitFor(() => expect(wrapper.text()).toContain(`已保存为${label}`))
      expect(wrapper.get('[aria-pressed="true"]').text()).toContain(label)
      expect(wrapper.text()).toContain('来源评分 8.5 分')
      expect(wrapper.text()).toContain('票数分布 · 3 票')
    }
    expect(fetch.mock.calls.filter(([, options]) => options?.method === 'PUT')).toHaveLength(5)
    expect(fetch.mock.calls.every(([path]) => !path.includes('/progress') && !path.includes('/comments'))).toBe(true)
  } finally { wrapper.unmount() }
  const reopened = await render()
  try { expect(reopened.wrapper.get('[aria-pressed="true"]').text()).toContain('毒草') }
  finally { reopened.wrapper.unmount() }
})

it('keeps the confirmed choice on failure and retries the same desired choice', async () => {
  let attempts = 0
  vi.stubGlobal('fetch', vi.fn(async (_path: string, options?: RequestInit) => {
    if (options?.method !== 'PUT') return response(detail('XIANCAO'))
    expect(JSON.parse(options.body as string)).toEqual({ type: 'LIANGCAO' })
    return ++attempts === 1 ? response({ error: { message: '暂时不可用' } }, 503) : response(voting('LIANGCAO'))
  }))
  const { wrapper } = await render()
  try {
    await wrapper.findAll('button').find(b => b.text().startsWith('粮草'))!.trigger('click')
    await vi.waitFor(() => expect(wrapper.get('[role="alert"]').text()).toContain('暂时不可用'))
    expect(wrapper.get('[aria-pressed="true"]').text()).toContain('仙草')
    expect(wrapper.text()).not.toContain('已保存')
    await wrapper.findAll('button').find(b => b.text() === '重试保存粮草')!.trigger('click')
    await vi.waitFor(() => expect(wrapper.get('[aria-pressed="true"]').text()).toContain('粮草'))
    expect(attempts).toBe(2)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  } finally { wrapper.unmount() }
})

it.each([401, 403])('clears private book and vote data when saving returns %s', async status => {
  vi.stubGlobal('fetch', vi.fn(async (_path: string, options?: RequestInit) => options?.method === 'PUT'
    ? response({ error: { message: '登录失效' } }, status) : response(detail('XIANCAO'))))
  const { wrapper } = await render()
  try {
    await wrapper.findAll('button').find(b => b.text().startsWith('毒草'))!.trigger('click')
    await vi.waitFor(() => expect(wrapper.text()).toContain('重新登录'))
    expect(wrapper.text()).not.toContain('作品 42')
    expect(wrapper.text()).not.toContain('已选仙草')
    expect(wrapper.find('[aria-label="五档评价"]').exists()).toBe(false)
  } finally { wrapper.unmount() }
})

it('prevents duplicate in-flight votes and permits an explicit same-choice retry', async () => {
  const pending = deferred<Response>()
  const fetch = vi.fn(async (_path: string, options?: RequestInit) => options?.method === 'PUT' ? pending.promise : response(detail()))
  vi.stubGlobal('fetch', fetch)
  const { wrapper } = await render()
  try {
    const button = wrapper.findAll('button').find(b => b.text().startsWith('仙草'))!
    await button.trigger('click'); await button.trigger('click')
    expect(wrapper.get('[aria-label="五档评价"]').attributes('aria-busy')).toBe('true')
    expect(wrapper.findAll('[aria-label="五档评价"] button').every(b => b.attributes('disabled') !== undefined)).toBe(true)
    expect(fetch.mock.calls.filter(([, options]) => options?.method === 'PUT')).toHaveLength(1)
    pending.resolve(response(voting('XIANCAO')))
    await vi.waitFor(() => expect(wrapper.text()).toContain('已选仙草'))
    await button.trigger('click'); await flushPromises()
    expect(wrapper.text()).toContain('票数分布 · 3 票')
  } finally { wrapper.unmount() }
})

it('ignores a late save after navigation to another book', async () => {
  const late = deferred<Response>()
  let signal: AbortSignal | null | undefined
  vi.stubGlobal('fetch', vi.fn(async (path: string, options?: RequestInit) => {
    if (options?.method === 'PUT') { signal = options.signal; return late.promise }
    return response(detail(null, path.endsWith('/43') ? 43 : 42))
  }))
  const { wrapper, router } = await render()
  try {
    await wrapper.findAll('button').find(b => b.text().startsWith('仙草'))!.trigger('click')
    await router.push('/book/43')
    await vi.waitFor(() => expect(wrapper.text()).toContain('作品 43'))
    expect(signal?.aborted).toBe(true)
    late.resolve(response(voting('XIANCAO'))); await flushPromises()
    expect(wrapper.text()).toContain('尚未评价')
    expect(wrapper.text()).not.toContain('已保存')
    expect(wrapper.find('[aria-pressed="true"]').exists()).toBe(false)
  } finally { wrapper.unmount() }
})
