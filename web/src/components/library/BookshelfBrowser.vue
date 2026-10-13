<script setup lang="ts">
import { computed, nextTick, shallowRef, useTemplateRef, watch } from 'vue'
import { useRoute } from 'vue-router'
import type { CollectionPage } from '../../api'
import { useRequest } from '../../composables/useRequest'
import RequestStatus from '../RequestStatus.vue'
import CollectionList from './CollectionList.vue'

const route = useRoute()
const view = computed(() => route.query.view === 'reading' ? 'reading' : 'shelf')
const cursor = computed(() => typeof route.query.cursor === 'string' ? route.query.cursor : '')
const { data, pending, error, reload } = useRequest<CollectionPage>(() => `/api/v1/${view.value}?limit=24&cursor=${encodeURIComponent(cursor.value)}`)
const notice = shallowRef('')
const heading = useTemplateRef<HTMLHeadingElement>('heading')
watch([view, cursor], () => { notice.value = '' })

async function changed(bookId: number, inShelf: boolean) {
  const page = data.value
  const old = page?.entries.find(entry => entry.book.id === bookId)
  if (!page || !old) return
  const delta = Number(inShelf) - Number(old.inShelf)
  data.value = { ...page, counts: { ...page.counts, shelf: page.counts.shelf + delta }, entries: view.value === 'shelf' && !inShelf
    ? page.entries.filter(entry => entry.book.id !== bookId)
    : page.entries.map(entry => entry.book.id === bookId ? { ...entry, inShelf } : entry) }
  notice.value = inShelf ? `已将《${old.book.title}》加入书架。` : `已将《${old.book.title}》移出书架，阅读进度已保留。`
  if (view.value === 'shelf' && !inShelf) { await nextTick(); heading.value?.focus() }
}
</script>

<template>
  <header class="shelf-heading">
    <div><p class="eyebrow">我的阅读空间</p><h1 ref="heading" tabindex="-1">我的书架</h1><p>收藏想读的作品，从上次停下的位置继续。</p></div>
    <dl v-if="data" class="shelf-counts"><div><dt>本收藏</dt><dd>{{ data.counts.shelf.toLocaleString() }}</dd></div><div><dt>本读过</dt><dd>{{ data.counts.reading.toLocaleString() }}</dd></div></dl>
  </header>
  <div class="shelf-controls">
    <nav class="shelf-tabs" aria-label="书架内容"><RouterLink v-slot="{ href, navigate }" custom :to="{ path: '/bookshelf' }"><a :href="href" :aria-current="view === 'shelf' ? 'page' : undefined" @click="navigate">已收藏</a></RouterLink><RouterLink v-slot="{ href, navigate }" custom :to="{ path: '/bookshelf', query: { view: 'reading' } }"><a :href="href" :aria-current="view === 'reading' ? 'page' : undefined" @click="navigate">最近阅读</a></RouterLink></nav>
    <button class="text-button" :disabled="pending" @click="reload">刷新</button>
  </div>
  <p class="muted collection-order">{{ view === 'shelf' ? '按收藏时间从近到远；移出书架会保留阅读记录。' : '按最近阅读时间排序；未加入书架的作品也会出现在这里。' }}</p>
  <p class="collection-notice" role="status">{{ notice }}</p>
  <RequestStatus :pending="pending" :error="error" @retry="reload" />
  <template v-if="data">
    <CollectionList v-if="data.entries.length" :entries="data.entries" @change="changed" />
    <div v-else class="collection-empty"><h2>{{ cursor ? '这一页已经没有作品了' : view === 'shelf' ? '书架还是空的' : '还没有阅读记录' }}</h2><p class="muted">{{ view === 'shelf' ? '在书目详情中加入想读的作品，随时从这里打开。' : '打开任意有正文的作品，阅读后会自动留下位置。' }}</p><RouterLink class="button" to="/library">去书库找书</RouterLink></div>
    <nav class="collection-pagination" aria-label="书架翻页"><RouterLink v-if="cursor" class="button secondary" :to="{ path: '/bookshelf', query: { view } }">返回第一页</RouterLink><RouterLink v-if="data.nextCursor" class="button" :to="{ path: '/bookshelf', query: { view, cursor: data.nextCursor } }">下一页 →</RouterLink></nav>
  </template>
  <RouterLink v-if="error?.code === 'invalid_cursor'" class="button secondary" :to="{ path: '/bookshelf', query: { view } }">返回第一页</RouterLink>
</template>

<style scoped>
.shelf-heading { display: flex; align-items: flex-end; justify-content: space-between; flex-wrap: wrap; gap: 22px; padding: 32px 36px; background: #294e42; border-radius: 22px; color: #e6eee8; }.shelf-heading .eyebrow { color: #efb69d; }.shelf-heading h1 { color: var(--paper); }.shelf-heading p { line-height: 1.8; }.shelf-counts { display: flex; gap: 32px; margin: 0 0 8px; }.shelf-counts div { display: flex; align-items: baseline; gap: 8px; }.shelf-counts dd { order: -1; margin: 0; font: 32px var(--serif); color: white; }.shelf-counts dt { font-size: 13px; }
.shelf-controls { display: flex; justify-content: space-between; align-items: center; border-bottom: 1px solid var(--line); margin-top: 32px; gap: 16px; }.shelf-tabs { display: flex; gap: 24px; }.shelf-tabs a { display: block; padding: 16px 0; font-weight: 600; text-decoration: none; border-bottom: 3px solid transparent; }.shelf-tabs a[aria-current=page] { border-color: var(--green); }.collection-order { font-size: 13px; }.collection-notice { color: var(--green); font-size: 14px; line-height: 1.8; }.collection-notice:empty { margin: 0; }.collection-empty { padding: 40px 24px; text-align: center; border: 1px dashed var(--line); border-radius: 16px; background: var(--paper); }.collection-empty h2 { font: 700 25px var(--serif); color: var(--green); }.collection-pagination { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 12px; margin-top: 28px; }
@media (max-width: 600px) { .shelf-heading { padding: 22px; border-radius: 16px; }.shelf-counts { gap: 24px; }.shelf-heading h1 { font-size: 32px; } }
</style>
