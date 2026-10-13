<script setup lang="ts">
import { computed } from 'vue'
import type { RouteLocationRaw } from 'vue-router'
import type { CatalogPage } from '../../api'
const props = defineProps<{ pagination: CatalogPage['pagination']; pageLink: (page: number) => RouteLocationRaw }>()
const pages = computed(() => {
  const { page, totalPages } = props.pagination
  const selected = [...new Set([1, page - 1, page, page + 1, totalPages].filter(p => p >= 1 && p <= totalPages))].sort((a, b) => a - b)
  return selected.flatMap((p, i) => i > 0 && p - selected[i - 1]! > 1 ? [0, p] : [p])
})
</script>

<template>
  <nav v-if="pagination.totalPages > 1" class="pagination" aria-label="书库翻页">
    <RouterLink v-if="pagination.page > 1" class="button secondary" :to="pageLink(pagination.page - 1)" :aria-current="false">← 上一页</RouterLink>
    <div class="page-numbers"><template v-for="(page, index) in pages" :key="index"><RouterLink v-if="page" :to="pageLink(page)" :aria-label="`第 ${page} 页`" :aria-current="page === pagination.page ? 'page' : false">{{ page }}</RouterLink><span v-else aria-hidden="true">…</span></template></div>
    <RouterLink v-if="pagination.page < pagination.totalPages" class="button secondary" :to="pageLink(pagination.page + 1)" :aria-current="false">下一页 →</RouterLink>
  </nav>
</template>

<style scoped>
.pagination { display: flex; justify-content: center; flex-wrap: wrap; align-items: center; gap: 16px; margin-top: 36px; }.page-numbers { display: flex; gap: 5px; flex-wrap: wrap; align-items: center; justify-content: center; }.page-numbers a { display: inline-flex; align-items: center; justify-content: center; min-width: 40px; min-height: 44px; padding: 8px; text-decoration: none; border-radius: 6px; }.page-numbers a[aria-current="page"] { background: var(--green); color: var(--paper); }.page-numbers span { color: var(--muted); }
@media (max-width: 480px) { .page-numbers { width: 100%; order: 1; }.pagination { justify-content: space-between; gap: 10px; } }
</style>
