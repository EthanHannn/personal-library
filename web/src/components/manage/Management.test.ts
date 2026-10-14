import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { expect, it, vi } from 'vitest'
import { deferred, response } from '../../test/fixtures'
import ImportForm from './ImportForm.vue'
import CollectForm from './CollectForm.vue'
import JobDetail from './JobDetail.vue'
import BookCover from '../book/BookCover.vue'
import type { Job } from '../../manage'

const job: Job = { id: 'sample-job', kind: 'import', status: 'review', input: {}, result: { title: '测试书', author: '作者', file: { path: 'sample.txt', encoding: 'utf-8', size: 123 }, chapterCount: 2, words: 12, chapters: [{ idx: 1, title: '第一章' }], warnings: ['部分正文'] }, message: '', attempts: 1, nextAt: 0, createdAt: 1, updatedAt: 1 }

it('creates a preview without publishing and preserves metadata while filling missing text', async () => {
  const fetch = vi.fn(async () => response(job, 202)); vi.stubGlobal('fetch', fetch)
  const wrapper = mount(ImportForm, { props: { bookId: 42, initialTitle: '测试书', initialAuthor: '作者', metadata: { title: '测试书', author: '作者', tag1: '玄幻', tag2: '东方', intro: '已有简介' } } })
  try {
    await wrapper.get('input[placeholder]').setValue('新书/test.txt')
    await wrapper.get('form').trigger('submit')
    await vi.waitFor(() => expect(wrapper.emitted('created')).toHaveLength(1))
    const body = JSON.parse((fetch.mock.calls[0] as unknown as [string, RequestInit])[1].body as string)
    expect(body).toMatchObject({ kind: 'import', input: { bookId: 42, title: '测试书', tag1: '玄幻', intro: '已有简介', path: '新书/test.txt' } })
    expect(fetch).toHaveBeenCalledTimes(1)
  } finally { wrapper.unmount() }
})

it('does not offer paid crawling and preserves a failed collection request for explicit retry', async () => {
  let calls = 0; vi.stubGlobal('fetch', vi.fn(async (path: string) => path === '/api/v1/manage/sources' ? response({ sources: [{ id: 'qidian', version: 1, enabled: true, config: { name: '起点', group: '男频', baseUrl: 'https://www.qidian.com/' } }], templates: [] }) : ++calls === 1 ? response({ error: { message: '暂时不可用' } }, 503) : response({ ...job, kind: 'collect', status: 'queued' }, 202)))
  const wrapper = mount(CollectForm)
  try {
    await flushPromises()
    expect(wrapper.text()).toContain('付费章节不采集')
    await wrapper.get('input[type="url"]').setValue('https://www.qidian.com/book/123/')
    await wrapper.get('form').trigger('submit'); await flushPromises()
    expect(wrapper.text()).toContain('暂时不可用')
    expect(wrapper.emitted('created')).toBeUndefined()
    await wrapper.findAll('button').find(b => b.text() === '重试')!.trigger('click')
    await vi.waitFor(() => expect(wrapper.emitted('created')).toHaveLength(1))
  } finally { wrapper.unmount() }
})

it('requires confirmation of a preview and clears it on authentication loss', async () => {
  const pending = deferred<Response>()
  vi.stubGlobal('fetch', vi.fn(async (_path: string, options?: RequestInit) => options?.method === 'POST' ? pending.promise : response(job)))
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/:pathMatch(.*)*', component: { template: '<p />' } }] }); await router.push('/manage'); await router.isReady()
  const wrapper = mount(JobDetail, { props: { id: job.id }, global: { plugins: [router] } })
  try {
    await vi.waitFor(() => expect(wrapper.text()).toContain('确认入库'))
    expect(wrapper.text()).toContain('部分正文')
    await wrapper.findAll('button').find(b => b.text() === '确认入库')!.trigger('click')
    expect(wrapper.findAll('button').find(b => b.text() === '确认入库')!.attributes('disabled')).toBeDefined()
    pending.resolve(response({ error: { message: '登录失效' } }, 401))
    await vi.waitFor(() => expect(wrapper.text()).toContain('重新登录'))
    expect(wrapper.text()).not.toContain('测试书')
    expect(wrapper.text()).not.toContain('确认入库')
  } finally { wrapper.unmount() }
})

it('uses local real covers and falls back when the asset is unavailable', async () => {
  const wrapper = mount(BookCover, { props: { title: '书名', src: '/api/v1/books/42/cover' } })
  try {
    expect(wrapper.get('img').attributes('src')).toBe('/api/v1/books/42/cover')
    await wrapper.get('img').trigger('error')
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.text()).toContain('书名')
    await wrapper.setProps({ src: '/api/v1/books/43/cover' })
    expect(wrapper.find('img').exists()).toBe(true)
  } finally { wrapper.unmount() }
})
