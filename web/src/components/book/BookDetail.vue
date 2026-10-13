<script setup lang="ts">
import { useRoute } from 'vue-router'
import { shallowRef, watch } from 'vue'
import type { BookState } from '../../api'
import { useRequest } from '../../composables/useRequest'
import RequestStatus from '../RequestStatus.vue'
import ShelfButton from '../library/ShelfButton.vue'
import ReadingPosition from '../library/ReadingPosition.vue'

const route = useRoute()
const { data, pending, error, reload } = useRequest<BookState>(() => `/api/v1/books/${route.params.id}`)
const notice = shallowRef('')
watch(() => route.params.id, () => { notice.value = '' })
function changed(inShelf: boolean) {
  if (!data.value) return
  data.value = { ...data.value, inShelf }
  notice.value = inShelf ? '已加入书架。' : '已移出书架，阅读进度已保留。'
}
</script>

<template>
  <RequestStatus :pending="pending" :error="error" @retry="reload" />
  <article v-if="data" class="book-detail">
    <p class="eyebrow">{{ data.book.tag1 }} · {{ data.book.tag2 }}</p>
    <h1>{{ data.book.title }}</h1>
    <p class="muted">{{ data.book.author }} · {{ data.book.wordCount.toLocaleString() }} 字 · {{ data.book.score.toFixed(1) }} 分</p>
    <p class="intro">{{ data.book.intro || '这本书还没有简介。' }}</p>
    <ReadingPosition v-if="data.progress" :progress="data.progress" :chapter-count="data.book.chapterCount" />
    <div class="book-actions"><RouterLink v-if="data.book.hasContent" class="button" :to="`/read/${data.book.id}`">{{ data.progress ? '继续阅读' : '开始阅读' }}</RouterLink><p v-else class="muted">暂未收录正文。</p><ShelfButton :key="data.book.id" :book-id="data.book.id" :in-shelf="data.inShelf" @change="changed" /><RouterLink class="shelf-link" to="/bookshelf">我的书架 →</RouterLink></div>
    <p role="status" class="muted">{{ notice }}</p>
  </article>
</template>

<style scoped>
.book-detail { max-width: 760px; margin: 44px auto; }
.intro { margin: 32px 0; line-height: 2; white-space: pre-wrap; overflow-wrap: anywhere; }
.book-actions { display: flex; flex-wrap: wrap; align-items: flex-start; gap: 12px; margin-top: 26px; }.shelf-link { display: flex; align-items: center; min-height: 44px; margin-inline: 8px; font-size: 14px; }
</style>
