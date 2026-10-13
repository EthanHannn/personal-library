<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import type { Book } from '../../api'
import { useRequest } from '../../composables/useRequest'
import BookList from './BookList.vue'
import RequestStatus from '../RequestStatus.vue'

const route = useRoute()
const after = computed(() => typeof route.query.after === 'string' ? route.query.after : '0')
const { data, pending, error, reload } = useRequest<{ books: Book[]; nextAfter: number | null }>(
  () => `/api/v1/books?limit=24&after=${encodeURIComponent(after.value)}`,
)
</script>

<template>
  <RequestStatus :pending="pending" :error="error" @retry="reload" />
  <template v-if="data">
    <BookList v-if="data.books.length" :books="data.books" />
    <p v-else class="muted">这里还没有书目。可以回到书库首页查看。</p>
    <nav class="pagination" aria-label="书库翻页">
      <RouterLink v-if="after !== '0'" class="button secondary" to="/library">回到首页</RouterLink>
      <RouterLink v-if="data.nextAfter !== null" class="button" :to="{ path: '/library', query: { after: data.nextAfter } }">下一页 →</RouterLink>
    </nav>
  </template>
</template>

<style scoped>
.pagination { display: flex; justify-content: flex-end; flex-wrap: wrap; gap: 12px; margin-top: 32px; }
</style>
