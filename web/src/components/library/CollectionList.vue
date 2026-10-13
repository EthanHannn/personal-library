<script setup lang="ts">
import type { CollectionEntry } from '../../api'
import BookCover from '../book/BookCover.vue'
import ReadingPosition from './ReadingPosition.vue'
import ShelfButton from './ShelfButton.vue'
defineProps<{ entries: CollectionEntry[] }>()
defineEmits<{ change: [bookId: number, inShelf: boolean] }>()
const date = (at: number) => new Date(at).toLocaleDateString('zh-CN')
</script>

<template>
  <ul class="collection-list">
    <li v-for="entry in entries" :key="entry.book.id" class="collection-card">
      <div class="collection-cover"><BookCover :title="entry.book.title" /></div>
      <div class="collection-info">
        <p class="collection-category">{{ entry.book.tag1 }}<template v-if="entry.book.tag2"> · {{ entry.book.tag2 }}</template></p>
        <h2 class="collection-title"><RouterLink :to="`/book/${entry.book.id}`">{{ entry.book.title }}</RouterLink></h2>
        <p class="collection-author">{{ entry.book.author }} · {{ entry.book.wordCount.toLocaleString() }} 字</p>
        <ReadingPosition :progress="entry.progress" :chapter-count="entry.book.chapterCount" />
        <p v-if="entry.progress" class="collection-date">上次阅读：{{ date(entry.progress.updatedAt) }}</p>
        <div class="collection-actions">
          <RouterLink v-if="entry.book.hasContent" class="button" :to="`/read/${entry.book.id}`">{{ entry.progress ? '继续阅读' : '开始阅读' }}</RouterLink>
          <span v-else class="muted no-content">暂未收录正文</span>
          <ShelfButton :book-id="entry.book.id" :in-shelf="entry.inShelf" @change="$emit('change', entry.book.id, $event)" />
        </div>
      </div>
    </li>
  </ul>
</template>

<style scoped>
.collection-list { list-style: none; padding: 0; margin: 0; display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 20px; }
.collection-card { padding: 24px; display: flex; gap: 22px; border: 1px solid var(--line); background: var(--paper); border-radius: 16px; }.collection-cover { width: 90px; flex-shrink: 0; }.collection-info { min-width: 0; flex: 1; }.collection-category { color: var(--muted); font-size: 11px; margin: 0 0 7px; }.collection-title { font: 700 23px/1.5 var(--serif); margin: 0; overflow-wrap: anywhere; }.collection-title a { text-decoration: none; }.collection-author { font-size: 13px; color: var(--muted); margin: 9px 0 0; }.collection-date { font-size: 12px; color: var(--muted); margin: 10px 0 0; }.collection-actions { display: flex; align-items: flex-start; gap: 10px; margin-top: 20px; flex-wrap: wrap; }.collection-actions .button { padding-inline: 15px; white-space: nowrap; }.no-content { font-size: 13px; min-height: 44px; display: flex; align-items: center; }
@media (max-width: 1000px) { .collection-list { grid-template-columns: minmax(0, 1fr); } }
@media (max-width: 420px) { .collection-card { padding: 18px 14px; gap: 14px; }.collection-cover { width: 52px; }.collection-title { font-size: 21px; } }
</style>
