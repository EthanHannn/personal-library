<script setup lang="ts">
import type { CollectionEntry } from '../../api'
import BookCover from '../book/BookCover.vue'
defineProps<{ entries: CollectionEntry[] }>()
</script>

<template>
  <section class="continue-section" aria-labelledby="continue-title">
    <div class="section-heading"><h2 id="continue-title">接着上次读</h2><RouterLink :to="{ path: '/bookshelf', query: { view: 'reading' } }">全部阅读记录 →</RouterLink></div>
    <ul v-if="entries.length" class="continue-list">
      <li v-for="entry in entries" :key="entry.book.id">
        <RouterLink class="continue-card" :to="entry.book.hasContent ? `/read/${entry.book.id}` : `/book/${entry.book.id}`">
          <div class="continue-cover"><BookCover :title="entry.book.title" /></div><div class="continue-copy"><h3>{{ entry.book.title }}</h3><p>{{ entry.book.author }}</p><p class="continue-position">第 {{ entry.progress?.chapterIdx.toLocaleString() }} 章 · 章内 {{ Math.round((entry.progress?.percent ?? 0) * 100) }}%</p><span>{{ entry.book.hasContent ? '继续阅读 →' : '正文暂缺 · 查看书目 →' }}</span></div>
        </RouterLink>
      </li>
    </ul>
    <div v-else class="continue-empty"><p>还没有阅读记录。开始阅读后，这里会留下你的位置。</p><RouterLink to="/library">去书库找书 →</RouterLink></div>
  </section>
</template>

<style scoped>
.continue-section { margin-top: 44px; }.continue-list { list-style: none; padding: 0; display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 18px; }.continue-card { display: flex; height: 100%; gap: 18px; padding: 22px; border: 1px solid var(--line); background: var(--paper); border-radius: 14px; text-decoration: none; }.continue-card:hover { border-color: var(--green); }.continue-cover { width: 52px; flex-shrink: 0; }.continue-copy { min-width: 0; }.continue-copy h3 { margin: 0; font: 700 21px/1.5 var(--serif); overflow-wrap: anywhere; }.continue-copy p { margin: 8px 0 0; font-size: 12px; color: var(--muted); line-height: 1.8; }.continue-copy .continue-position { color: var(--ochre); }.continue-copy span { display: block; font-size: 13px; margin-top: 12px; }.continue-empty { border: 1px dashed var(--line); padding: 24px; border-radius: 14px; font-size: 14px; color: var(--muted); line-height: 1.8; }
@media (max-width: 900px) { .continue-list { grid-template-columns: minmax(0, 1fr); }.continue-cover { width: 48px; } }
</style>
