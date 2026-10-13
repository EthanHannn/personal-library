import { flushPromises, mount } from '@vue/test-utils'
import { expect, it, vi } from 'vitest'
import ChapterDrawer from './ChapterDrawer.vue'
import { response } from '../../test/fixtures'

it('renders only the visible window in a long directory and supports numbered navigation', async () => {
  const chapters = Array.from({ length: 100 }, (_, i) => ({ idx: 5001 + i, title: `章节 ${5001 + i}` }))
  const fetch = vi.fn().mockImplementation(async () => response({ chapters, nextAfter: 5100 })); vi.stubGlobal('fetch', fetch)
  const wrapper = mount(ChapterDrawer, { props: { bookId: 42, current: 5050, total: 12000 } })
  await flushPromises()
  expect(String(fetch.mock.calls[0][0])).toContain('after=5000')
  expect(wrapper.findAll('[role=option]').length).toBeLessThanOrEqual(12)
  expect(wrapper.text()).toContain('5050')
  await wrapper.get('#chapter-number').setValue(9999)
  await wrapper.get('.chapter-jump').trigger('submit')
  expect(wrapper.emitted('navigate')?.[0]).toEqual([9999])
  await wrapper.get('#chapter-search').setValue('番外')
  await wrapper.get('.directory-search').trigger('submit')
  await flushPromises()
  expect(String(fetch.mock.calls.at(-1)?.[0])).toContain('q=%E7%95%AA%E5%A4%96')
  wrapper.unmount()
})
