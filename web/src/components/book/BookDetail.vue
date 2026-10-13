<script setup lang="ts">
import { useRoute } from 'vue-router'
import type { Book } from '../../api'
import { useRequest } from '../../composables/useRequest'
import RequestStatus from '../RequestStatus.vue'

const route = useRoute()
const { data, pending, error, reload } = useRequest<{ book: Book }>(() => `/api/v1/books/${route.params.id}`)
</script>

<template>
  <RequestStatus :pending="pending" :error="error" @retry="reload" />
  <article v-if="data" class="book-detail">
    <p class="eyebrow">{{ data.book.tag1 }} · {{ data.book.tag2 }}</p>
    <h1>{{ data.book.title }}</h1>
    <p class="muted">{{ data.book.author }} · {{ data.book.wordCount.toLocaleString() }} 字 · {{ data.book.score.toFixed(1) }} 分</p>
    <p class="intro">{{ data.book.intro || '这本书还没有简介。' }}</p>
    <RouterLink v-if="data.book.hasContent" class="button" :to="`/read/${data.book.id}?c=1`">阅读预览</RouterLink>
    <p v-else class="muted">暂未收录正文。</p>
  </article>
</template>

<style scoped>
.book-detail { max-width: 760px; margin: 44px auto; }
.intro { margin: 32px 0; line-height: 2; white-space: pre-wrap; overflow-wrap: anywhere; }
</style>
