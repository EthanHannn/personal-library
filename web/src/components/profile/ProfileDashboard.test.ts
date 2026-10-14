import { mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import { expect, it, vi } from 'vitest'
import { response } from '../../test/fixtures'
import ProfileDashboard from './ProfileDashboard.vue'

async function render() {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/profile', component: ProfileDashboard }, { path: '/:pathMatch(.*)*', component: { template: '<p>其他页面</p>' } }] })
  await router.push('/profile'); await router.isReady()
  const wrapper = mount(ProfileDashboard, { global: { plugins: [router] } })
  await vi.waitFor(() => expect(wrapper.text()).toContain('我的评价'))
  return { wrapper, router }
}
const profile = { nickname: '我的昵称', accountUrl: 'https://auth.test', counts: { shelf: 2, reading: 1, votes: 1 }, reading: [] }
const votes = { entries: [{ book: { id: 42, title: '我的藏书', author: '作者' }, type: 'XIANCAO', createdAt: 1000 }], nextCursor: null }

it('shows personal records and keeps a filter in the URL while applying local reader preferences', async () => {
  vi.stubGlobal('fetch', vi.fn(async (path: string) => response(path === '/api/v1/profile' ? profile : path.includes('type=KUCAO') ? { entries: [], nextCursor: null } : votes)))
  const { wrapper, router } = await render()
  try {
    await vi.waitFor(() => expect(wrapper.text()).toContain('我的藏书'))
    expect(wrapper.find('a[href="/book/42"]').exists()).toBe(true)
    await wrapper.get('select').setValue('KUCAO')
    await vi.waitFor(() => expect(wrapper.text()).toContain('还没有这一档评价'))
    expect(router.currentRoute.value.query.type).toBe('KUCAO')
    expect(wrapper.text()).not.toContain('我的藏书')
    await wrapper.get('input[type="range"]').setValue(24)
    expect(JSON.parse(localStorage.getItem('library.reader.preferences.v1')!).fontSize).toBe(24)
    expect(wrapper.get('a[href="https://auth.test"]').text()).toContain('门户账户')
  } finally { wrapper.unmount() }
})

it('retries failed vote history and clears the whole profile after authentication loss', async () => {
  let calls = 0
  vi.stubGlobal('fetch', vi.fn(async (path: string) => path === '/api/v1/profile' ? response(profile) : ++calls === 1 ? response({ error: { message: '稍后重试' } }, 503) : response(votes)))
  const { wrapper } = await render()
  try {
    await vi.waitFor(() => expect(wrapper.text()).toContain('稍后重试'))
    await wrapper.findAll('button').find(button => button.text() === '重试')!.trigger('click')
    await vi.waitFor(() => expect(wrapper.text()).toContain('我的藏书'))
    window.dispatchEvent(new Event('library:auth-lost'))
    await vi.waitFor(() => expect(wrapper.text()).toContain('重新登录'))
    expect(wrapper.text()).not.toContain('我的昵称')
    expect(wrapper.text()).not.toContain('我的藏书')
  } finally { wrapper.unmount() }
})
