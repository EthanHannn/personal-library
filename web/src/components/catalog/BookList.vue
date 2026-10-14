<script setup lang="ts">
import BookCover from '../book/BookCover.vue'
import type { Book, CatalogBook } from '../../api'

withDefaults(defineProps<{ books: (Book & Partial<Pick<CatalogBook, 'popularity' | 'xiancaoCount'>>)[]; headingTag?: 'h2' | 'h3'; sortBy?: string }>(), { headingTag: 'h2', sortBy: 'score' })
</script>

<template>
  <ul class="book-list">
    <li v-for="book in books" :key="book.id">
      <article class="book-card">
        <RouterLink class="book-spine" :to="`/book/${book.id}`" tabindex="-1" aria-hidden="true"><BookCover :title="book.title" :src="book.coverUrl" /></RouterLink>
        <div class="book-info">
          <p class="category"><RouterLink v-if="book.tag1" :to="`/category/${encodeURIComponent(book.tag1)}`">{{ book.tag1 }}</RouterLink><span v-if="book.tag2"> · <RouterLink :to="`/category/${encodeURIComponent(book.tag2)}`">{{ book.tag2 }}</RouterLink></span></p>
          <component :is="headingTag" class="book-title"><RouterLink :to="`/book/${book.id}`">{{ book.title }}</RouterLink></component>
          <p class="author"><RouterLink v-if="book.author" :to="`/author/${encodeURIComponent(book.author)}`">{{ book.author }}</RouterLink><span v-else>作者未详</span></p>
          <p class="book-meta"><span class="score">{{ book.score.toFixed(1) }} 分</span><span>{{ book.hasContent ? `${book.chapterCount.toLocaleString()} 章` : '暂无正文' }}</span></p>
          <p class="book-length">{{ book.wordCount ? `${(book.wordCount / 10000).toLocaleString('zh-CN', { maximumFractionDigits: 1 })} 万字` : '字数未详' }}<span v-if="sortBy === 'xiancao' && book.xiancaoCount !== undefined"> · 仙草 {{ book.xiancaoCount.toLocaleString() }} 票</span><span v-if="sortBy === 'popularity' && book.popularity !== undefined"> · {{ book.popularity.toLocaleString() }} 人评价</span></p>
        </div>
      </article>
    </li>
  </ul>
</template>

<style scoped>
.book-list { list-style: none; margin: 0; padding: 0; display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 20px; }
.book-card { display: flex; height: 100%; gap: 20px; padding: 24px 20px; background: var(--paper); border: 1px solid var(--line); border-radius: 14px; text-decoration: none; transition: border-color .15s; }
.book-card:hover { border-color: var(--green); }
.book-spine { flex: 0 0 66px; align-self: flex-start; display: block; text-decoration: none; }
.book-info { min-width: 0; }
.category { margin: 0 0 10px; color: var(--muted); font-size: 11px; overflow-wrap: anywhere; }.category a, .author a { color: inherit; text-decoration: none; }.category a:hover, .author a:hover, .book-title a:hover { text-decoration: underline; }
.book-title a { text-decoration: none; }.book-length { margin: 10px 0 0; font-size: 12px; line-height: 1.7; color: var(--muted); }
.book-title { margin: 0; font-family: var(--serif); font-size: 21px; line-height: 1.5; overflow-wrap: anywhere; }
.author { color: var(--muted); font-size: 13px; margin: 7px 0 18px; overflow-wrap: anywhere; }
.book-meta { display: flex; flex-wrap: wrap; gap: 12px; margin: 0; font-size: 12px; color: var(--muted); }
.score { color: var(--ochre); font-weight: 700; }
@media (max-width: 1050px) { .book-list { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@media (max-width: 660px) { .book-list { grid-template-columns: minmax(0, 1fr); gap: 14px; } }
@media (prefers-reduced-motion: reduce) { .book-card { transition: none; } }
</style>
