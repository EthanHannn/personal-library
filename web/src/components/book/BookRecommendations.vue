<script setup lang="ts">
import type { RelatedBooks } from '../../api'
import { useRequest } from '../../composables/useRequest'
import RequestStatus from '../RequestStatus.vue'

const props = defineProps<{ bookId: number }>()
const { data, pending, error, reload } = useRequest<RelatedBooks>(() => `/api/v1/books/${props.bookId}/recommendations`)
</script>

<template>
  <section v-if="pending || error || data?.entries?.length" class="recommendations" aria-labelledby="related-title">
    <h2 id="related-title">书库里，还可以读</h2>
    <p class="muted">按作者、分类与篇幅挑选，均已收录正文。</p>
    <RequestStatus :pending="pending" :error="error" @retry="reload" />
    <ul v-if="data?.entries?.length">
      <li v-for="entry in data.entries" :key="entry.book.id"><RouterLink :to="`/book/${entry.book.id}`"><span class="related-reason">{{ entry.reason }}</span><h3>{{ entry.book.title }}</h3><p>{{ entry.book.author }} · {{ entry.book.score.toFixed(1) }} 分</p></RouterLink></li>
    </ul>
  </section>
</template>

<style scoped>
.recommendations { margin-top: 40px; border-top: 1px solid var(--line); padding-top: 24px; }.recommendations h2 { font: 700 24px var(--serif); }.recommendations > p { font-size: 13px; }.recommendations ul { list-style: none; padding: 0; display: grid; grid-template-columns: repeat(2,minmax(0,1fr)); gap: 12px; }.recommendations a { display: block; height: 100%; padding: 18px; border: 1px solid var(--line); border-radius: 10px; background: var(--paper); text-decoration: none; overflow-wrap: anywhere; }.recommendations a:hover { border-color: var(--green); }.recommendations h3 { font: 700 20px/1.6 var(--serif); margin: 8px 0; }.recommendations li p { color: var(--muted); font-size: 12px; }.related-reason { font-size: 12px; color: var(--ochre); }@media(max-width:450px) { .recommendations ul { grid-template-columns: minmax(0,1fr); } }
</style>
