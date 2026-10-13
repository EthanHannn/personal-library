import { mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { expect, it, vi } from 'vitest'
import type { Book, HomeData } from '../../api'
import { response } from '../../test/fixtures'
import HomeDashboard from './HomeDashboard.vue'

const book: Book = { id: 42, title: '第一本', author: '作者', tag1: '小说', tag2: '', wordCount: 1000, chapterCount: 9, score: 8.5, hasContent: true }
const home: HomeData = { stats: { books: 120, readable: 60, authors: 80, categories: 6 },
  featured: [{ ...book, xiancaoCount: 4 }, { ...book, id: 43, title: '第二本', xiancaoCount: 8 }], highRated: [book], latest: [book],
  reading: [{ book, inShelf: false, addedAt: null, progress: { chapterIdx: 7, percent: .4, updatedAt: 123 } }],
}

it('opens recent reading without forcing the server chapter over a newer local position', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => response(home)))
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: HomeDashboard }] })
  await router.push('/'); await router.isReady()
  const wrapper = mount(HomeDashboard, { global: { plugins: [router] } })
  try {
    await vi.waitFor(() => expect(wrapper.text()).toContain('接着上次读'))
    expect(wrapper.get('.continue-card').attributes('href')).toBe('/read/42')
    expect(wrapper.text()).toContain('第 7 章 · 章内 40%')
    await wrapper.get('button[aria-label="下一本精选"]').trigger('click')
    expect(wrapper.get('h1').text()).toBe('第二本')
    expect(wrapper.get('.featured-actions a').attributes('href')).toBe('/read/43')
    expect(wrapper.text()).toContain('8 张仙草票')
  } finally { wrapper.unmount() }
})

it('keeps navigation useful when no book is eligible for a homepage recommendation', async () => {
  vi.stubGlobal('fetch', vi.fn(async () => response({ ...home, featured: [], highRated: [], latest: [], reading: [] })))
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/', component: HomeDashboard }] })
  await router.push('/'); await router.isReady()
  const wrapper = mount(HomeDashboard, { global: { plugins: [router] } })
  try {
    await vi.waitFor(() => expect(wrapper.text()).toContain('还没有阅读记录'))
    expect(wrapper.get('h1').text()).toBe('知轩书房')
    expect(wrapper.find('a[href="/library"]').exists()).toBe(true)
    expect(wrapper.find('a[href^="/read/"]').exists()).toBe(false)
    expect(wrapper.text()).not.toContain('第一本')
  } finally { wrapper.unmount() }
})
