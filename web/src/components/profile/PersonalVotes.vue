<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { PersonalVotes, VoteType } from '../../api'
import { useRequest } from '../../composables/useRequest'
import RequestStatus from '../RequestStatus.vue'

const labels: Record<VoteType, string> = { XIANCAO: '仙草', LIANGCAO: '粮草', GANCAO: '干草', KUCAO: '枯草', DUCAO: '毒草' }
const route = useRoute()
const router = useRouter()
const type = computed(() => typeof route.query.type === 'string' ? route.query.type : '')
const cursor = computed(() => typeof route.query.cursor === 'string' ? route.query.cursor : '')
const { data, pending, error, reload } = useRequest<PersonalVotes>(() => `/api/v1/votes?type=${encodeURIComponent(type.value)}&cursor=${encodeURIComponent(cursor.value)}`)
function filter(event: Event) { void router.push({ path: '/profile', query: { type: (event.target as HTMLSelectElement).value }, hash: '#my-votes' }) }
</script>

<template>
  <section id="my-votes" class="my-votes" aria-labelledby="my-votes-title">
    <div class="section-heading"><h2 id="my-votes-title">我的评价</h2><button class="text-button" :disabled="pending" @click="reload">刷新评价</button></div>
    <div class="vote-filter"><label for="vote-filter">筛选评价</label><select id="vote-filter" :value="type" @change="filter"><option value="">全部</option><option v-for="(label, value) in labels" :key="value" :value="value">{{ label }}</option></select></div>
    <p class="muted">显示当前选择，按首次评价时间从近到远排列；打开作品可修改。</p>
    <RequestStatus :pending="pending" :error="error" @retry="reload" />
    <template v-if="data">
      <ul v-if="data.entries.length" class="vote-history"><li v-for="entry in data.entries" :key="entry.book.id"><div><h3><RouterLink :to="`/book/${entry.book.id}`">{{ entry.book.title }}</RouterLink></h3><p>{{ entry.book.author }} · 首次评价 {{ new Date(entry.createdAt).toLocaleDateString('zh-CN') }}</p></div><span class="choice">{{ labels[entry.type] }}</span></li></ul>
      <p v-else class="empty">{{ cursor ? '这一页没有更多评价了。' : type ? '还没有这一档评价。' : '还没有评价。在作品详情里选一档，留下自己的阅读印象。' }}</p>
      <nav class="pagination" aria-label="评价翻页"><RouterLink v-if="cursor" class="button secondary" :to="{ path: '/profile', query: { type }, hash: '#my-votes' }">返回第一页</RouterLink><RouterLink v-if="data.nextCursor" class="button" :to="{ path: '/profile', query: { type, cursor: data.nextCursor }, hash: '#my-votes' }">下一页 →</RouterLink></nav>
    </template>
    <RouterLink v-if="error?.code === 'invalid_cursor'" to="/profile">返回第一页</RouterLink>
  </section>
</template>

<style scoped>
.my-votes { margin-top: 40px; }.vote-filter { display: flex; gap: 12px; align-items: center; }.vote-filter select { padding: 10px 16px; border: 1px solid var(--line); border-radius: 8px; background: var(--paper); color: var(--ink); }.my-votes > p { font-size: 13px; line-height: 1.8; }.vote-history { padding: 0; list-style: none; }.vote-history li { padding: 20px 0; border-bottom: 1px solid var(--line); display: flex; align-items: center; justify-content: space-between; gap: 18px; }.vote-history li > div { min-width: 0; }.vote-history h3 { margin: 0 0 8px; font: 700 21px/1.5 var(--serif); overflow-wrap: anywhere; }.vote-history a { text-decoration: none; }.vote-history a:hover { text-decoration: underline; }.vote-history p { margin: 0; font-size: 12px; color: var(--muted); overflow-wrap: anywhere; }.choice { flex-shrink: 0; color: var(--green); border: 1px solid var(--line); border-radius: 30px; padding: 8px 14px; background: var(--paper); }.pagination { display: flex; gap: 12px; flex-wrap: wrap; justify-content: flex-end; margin-top: 24px; }.empty { padding: 24px; border: 1px dashed var(--line); border-radius: 12px; }
</style>
