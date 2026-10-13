<script setup lang="ts">
import type { Book } from '../../api'

withDefaults(defineProps<{ books: Book[]; headingTag?: 'h2' | 'h3' }>(), { headingTag: 'h2' })
</script>

<template>
  <ul class="book-list">
    <li v-for="book in books" :key="book.id">
      <RouterLink class="book-card" :to="`/book/${book.id}`">
        <div class="book-spine" aria-hidden="true"><span>{{ book.title.slice(0, 8) }}</span></div>
        <div class="book-info">
          <p class="category">{{ book.tag1 }}<span v-if="book.tag2"> · {{ book.tag2 }}</span></p>
          <component :is="headingTag" class="book-title">{{ book.title }}</component>
          <p class="author">{{ book.author }}</p>
          <p class="book-meta"><span class="score">{{ book.score.toFixed(1) }} 分</span><span>{{ book.hasContent ? `${book.chapterCount.toLocaleString()} 章` : '暂无正文' }}</span></p>
        </div>
      </RouterLink>
    </li>
  </ul>
</template>

<style scoped>
.book-list { list-style: none; margin: 0; padding: 0; display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 20px; }
.book-card { display: flex; height: 100%; gap: 20px; padding: 24px 20px; background: var(--paper); border: 1px solid var(--line); border-radius: 14px; text-decoration: none; transition: border-color .15s; }
.book-card:hover { border-color: var(--green); }
.book-spine { flex: 0 0 66px; min-height: 110px; display: flex; justify-content: center; padding: 14px 8px; border-left: 5px solid #1e332b; background: var(--green); color: var(--paper); border-radius: 2px 5px 5px 2px; font-family: var(--serif); }
.book-spine span { writing-mode: vertical-rl; letter-spacing: .15em; }
.book-info { min-width: 0; }
.category { margin: 0 0 10px; color: var(--muted); font-size: 11px; }
.book-title { margin: 0; font-family: var(--serif); font-size: 21px; line-height: 1.5; overflow-wrap: anywhere; }
.author { color: var(--muted); font-size: 13px; margin: 7px 0 18px; }
.book-meta { display: flex; flex-wrap: wrap; gap: 12px; margin: 0; font-size: 12px; color: var(--muted); }
.score { color: var(--ochre); font-weight: 700; }
@media (max-width: 1050px) { .book-list { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@media (max-width: 660px) { .book-list { grid-template-columns: minmax(0, 1fr); gap: 14px; } }
@media (prefers-reduced-motion: reduce) { .book-card { transition: none; } }
</style>
