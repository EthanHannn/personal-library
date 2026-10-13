<script setup lang="ts">
import { computed, shallowRef, watch } from 'vue'
import type { FeaturedBook, HomeData } from '../../api'
import BookCover from '../book/BookCover.vue'

const props = defineProps<{ books: FeaturedBook[]; stats: HomeData['stats'] }>()
const selected = shallowRef(0)
const book = computed(() => props.books[selected.value])
watch(() => props.books, () => { selected.value = 0 })
function step(delta: number) { selected.value = (selected.value + delta + props.books.length) % props.books.length }
</script>

<template>
  <section class="featured" aria-label="书房精选" aria-roledescription="轮播">
    <div class="featured-main">
      <div class="featured-copy" aria-live="polite" aria-atomic="true">
        <p class="featured-kicker">书房精选</p>
        <template v-if="book"><h1 class="featured-title">{{ book.title }}</h1><p class="featured-author">{{ book.author }} · {{ book.tag1 }}<template v-if="book.tag2"> / {{ book.tag2 }}</template></p><p class="featured-intro">{{ book.intro || '打开书页，开始一段新的阅读。' }}</p><p class="featured-rating"><strong>{{ book.score.toFixed(1) }}</strong> 分<span>{{ book.xiancaoCount.toLocaleString() }} 张仙草票</span></p><div class="featured-actions"><RouterLink class="button" :to="`/read/${book.id}`">打开阅读</RouterLink><RouterLink class="featured-detail" :to="`/book/${book.id}`">查看书目 →</RouterLink></div></template>
        <template v-else><h1 class="featured-title">知轩书房</h1><p class="featured-intro">从馆藏中挑一本想读的书，或回到自己的书架。</p><RouterLink class="button" to="/library">浏览书库</RouterLink></template>
      </div>
      <div v-if="book" class="featured-cover"><BookCover :title="book.title" /></div>
    </div>
    <div class="featured-footer">
      <dl class="featured-stats"><div><dd>{{ stats.books.toLocaleString() }}</dd><dt>部馆藏</dt></div><div><dd>{{ stats.readable.toLocaleString() }}</dd><dt>部可阅读</dt></div><div><dd>{{ stats.authors.toLocaleString() }}</dd><dt>位作者</dt></div></dl>
      <div v-if="books.length > 1" class="featured-controls"><button aria-label="上一本精选" @click="step(-1)">←</button><span>{{ selected + 1 }} / {{ books.length }}</span><button aria-label="下一本精选" @click="step(1)">→</button></div>
    </div>
  </section>
</template>

<style scoped>
.featured { background: #294e42; color: #e6eee8; border-radius: 24px; overflow: hidden; }.featured-main { display: flex; align-items: center; gap: 50px; padding: 40px 48px; min-height: 360px; }.featured-copy { flex: 1; min-width: 0; }.featured-kicker { color: #efb69d; font-size: 13px; font-weight: 600; letter-spacing: .16em; }.featured-title { color: var(--paper); font-size: clamp(30px, 4vw, 44px); margin-top: 18px; }.featured-author { font-size: 14px; line-height: 1.7; }.featured-intro { max-width: 42em; font-size: 14px; line-height: 1.9; display: -webkit-box; -webkit-box-orient: vertical; -webkit-line-clamp: 3; overflow: hidden; overflow-wrap: anywhere; min-height: 5.7em; }.featured-cover { width: 160px; flex-shrink: 0; padding: 12px; background: #fffdf8; border-radius: 8px; transform: rotate(4deg); }.featured-cover :deep(.book-cover) { min-height: 185px; }.featured-rating { display: flex; align-items: baseline; gap: 6px; color: #efb69d; font-size: 13px; }.featured-rating strong { font-family: var(--serif); font-size: 30px; }.featured-rating span { margin-left: 18px; color: #e6eee8; }.featured-actions { display: flex; align-items: center; flex-wrap: wrap; gap: 20px; margin-top: 22px; }.featured .button { background: #fffdf8; color: #263e35; border-color: #fffdf8; font-weight: 600; }.featured-detail { color: #fffdf8; min-height: 44px; display: flex; align-items: center; font-size: 14px; }.featured-footer { display: flex; align-items: center; justify-content: space-between; gap: 20px; padding: 22px 48px; border-top: 1px solid #547266; }.featured-stats { display: flex; gap: 34px; margin: 0; flex-wrap: wrap; }.featured-stats div { display: flex; align-items: baseline; gap: 7px; }.featured-stats dd { font: 23px var(--serif); margin: 0; color: #fffdf8; }.featured-stats dt { font-size: 12px; }.featured-controls { display: flex; align-items: center; gap: 16px; font-size: 13px; white-space: nowrap; }.featured-controls button { color: #fffdf8; background: transparent; border: 1px solid #8da698; border-radius: 50%; height: 44px; width: 44px; }
@media (max-width: 700px) { .featured-main { gap: 22px; padding: 26px; }.featured-cover { width: 105px; padding: 8px; }.featured-cover :deep(.book-cover) { min-height: 130px; }.featured-footer { padding: 20px 26px; flex-wrap: wrap; }.featured-stats { gap: 20px; }.featured-stats div { display: grid; gap: 5px; }.featured-title { font-size: 30px; }.featured-intro { -webkit-line-clamp: 4; min-height: auto; } }
@media (max-width: 440px) { .featured { border-radius: 16px; }.featured-main { padding: 22px; min-height: 0; }.featured-cover { display: none; }.featured-stats { width: 100%; justify-content: space-between; gap: 10px; }.featured-controls { margin-left: auto; }.featured-footer { padding: 20px 22px; }.featured-rating span { margin-left: 10px; } }
</style>
