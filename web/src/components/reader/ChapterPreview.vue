<script setup lang="ts">
import { computed, shallowRef, watch } from 'vue'
import { useRoute } from 'vue-router'
import type { ContentBlock } from '../../api'
import { useRequest } from '../../composables/useRequest'
import RequestStatus from '../RequestStatus.vue'

const route = useRoute()
const chapter = computed(() => typeof route.query.c === 'string' ? route.query.c : '1')
const key = computed(() => `${route.params.id}:${chapter.value}`)
const cursor = shallowRef<{ key: string; token: string } | null>(null)
watch(key, () => { cursor.value = null }, { flush: 'sync' })
const { data, error, pending, reload } = useRequest<ContentBlock>(() => {
  const token = cursor.value?.key === key.value ? cursor.value.token : ''
  return `/api/v1/books/${route.params.id}/chapters/${encodeURIComponent(chapter.value)}/content?cursor=${encodeURIComponent(token)}`
})

function nextBlock() {
  if (data.value?.nextCursor) {
    cursor.value = { key: key.value, token: data.value.nextCursor }
    window.scrollTo({ top: 0 })
  }
}

function retry() {
  cursor.value = null
  reload()
}
</script>

<template>
  <p class="preview-note">阅读预览 · 暂不保存阅读进度</p>
  <RequestStatus :pending="pending" :error="error" @retry="retry" />
  <article v-if="data" class="chapter">
    <header class="chapter-header">
      <RouterLink :to="`/book/${data.book.id}`">{{ data.book.title }}</RouterLink>
      <h1>{{ data.chapter.title }}</h1>
    </header>
    <div class="chapter-text">{{ data.text }}</div>
    <nav class="chapter-nav" aria-label="阅读翻页">
      <RouterLink v-if="data.chapter.idx > 1" class="button secondary" :to="`/read/${data.book.id}?c=${data.chapter.idx - 1}`">上一章</RouterLink>
      <button v-if="data.nextCursor" class="button" @click="nextBlock">继续本章 →</button>
      <RouterLink v-else-if="data.chapter.idx < data.book.chapterCount" class="button" :to="`/read/${data.book.id}?c=${data.chapter.idx + 1}`">下一章 →</RouterLink>
      <RouterLink v-else class="button" :to="`/book/${data.book.id}`">已读到末章 · 返回书目</RouterLink>
    </nav>
  </article>
</template>

<style scoped>
.preview-note { color: var(--muted); font-size: 13px; text-align: center; padding-bottom: 20px; border-bottom: 1px solid var(--line); }
.chapter { max-width: 740px; margin: 36px auto; }
.chapter-header { margin-bottom: 36px; }
.chapter-text { font-family: var(--serif); font-size: 20px; line-height: 2; white-space: pre-wrap; overflow-wrap: anywhere; }
.chapter-nav { display: flex; justify-content: space-between; flex-wrap: wrap; gap: 16px; margin-top: 48px; }
@media (max-width: 600px) { .chapter-text { font-size: 18px; } }
</style>
