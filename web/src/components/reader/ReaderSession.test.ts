import { mount } from '@vue/test-utils'
import { defineComponent, h } from 'vue'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { expect, it, vi } from 'vitest'
import ReaderSession from './ReaderSession.vue'
import { position, reading, response } from '../../test/fixtures'
import { updateLocal } from '../../reader/localProgress'

it('updates the deep link to the chosen conflict position so a refresh keeps the chosen chapter', async () => {
  localStorage.setItem('zx_reading_302', JSON.stringify({ chapterIdx: 1, percent: 0.8, pending: true }))
  const remote = { ...position(2), revision: 9, updatedAt: 123 }
  vi.stubGlobal('fetch', vi.fn(async (path: string, options?: RequestInit) => {
    if (options?.method === 'PUT') return response({ progress: { ...JSON.parse(String(options.body)), revision: 10, updatedAt: 124 } })
    const chapter = path.includes('chapter=2') ? 2 : 1
    return response({ ...reading(chapter), progress: remote })
  }))
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/read/:id', component: ReaderSession, props: route => ({ bookId: Number(route.params.id) }) }] })
  await router.push('/read/302?c=1'); await router.isReady()
  const wrapper = mount(defineComponent({ setup: () => () => h(RouterView) }), { global: { plugins: [router] } })
  await vi.waitFor(() => expect(wrapper.text()).toContain('发现另一处阅读位置'))
  const remoteButton = wrapper.findAll('button').find(button => button.text().startsWith('服务端：'))!
  await remoteButton.trigger('click')
  await vi.waitFor(() => expect(router.currentRoute.value.query.c).toBe('2'))
  await vi.waitFor(() => expect(wrapper.get('h1').text()).toBe('第 2 章'))
  expect(wrapper.find('.conflict-actions').exists()).toBe(false)
  await vi.waitFor(() => expect(wrapper.text()).toContain('进度已恢复'))
  wrapper.unmount()
  // Leaving deliberately flushes local progress. Let that durable operation
  // finish before the test environment removes its network mock.
  await vi.waitFor(async () => {
    const record = await updateLocal(302, () => {})
    expect(record.draft).toBeNull(); expect(record.pending).toBeNull()
  })
})

it('retries a pending local position after a network failure without replacing it with the server opening', async () => {
  localStorage.setItem('zx_reading_303', JSON.stringify({ chapterIdx: 2, percent: 0.5, pending: true }))
  let attempts = 0
  vi.stubGlobal('fetch', vi.fn(async (path: string, options?: RequestInit) => {
    if (options?.method === 'PUT') return response({ progress: { ...JSON.parse(String(options.body)), revision: 1, updatedAt: 124 } })
    if (path.includes('chapter=2') && ++attempts === 1) return response({ error: { message: '测试网络中断' } }, 503)
    return response({ ...reading(path.includes('chapter=2') ? 2 : 1), progress: null })
  }))
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/read/:id', component: ReaderSession, props: route => ({ bookId: Number(route.params.id) }) }] })
  await router.push('/read/303'); await router.isReady()
  const wrapper = mount(defineComponent({ setup: () => () => h(RouterView) }), { global: { plugins: [router] } })
  await vi.waitFor(() => expect(wrapper.text()).toContain('测试网络中断'))
  await wrapper.findAll('button').find(button => button.text() === '重试恢复')!.trigger('click')
  await vi.waitFor(() => expect(wrapper.get('h1').text()).toBe('第 2 章'))
  await vi.waitFor(() => expect(wrapper.text()).toContain('进度已恢复'))
  expect(attempts).toBe(2)
  wrapper.unmount()
  await vi.waitFor(async () => {
    const record = await updateLocal(303, () => {})
    expect(record.draft).toBeNull(); expect(record.pending).toBeNull()
  })
})
