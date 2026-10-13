<script setup lang="ts">
import type { Category } from '../../api'
defineProps<{ categories: Category[] }>()
</script>

<template>
  <details v-if="categories.length" class="category-directory">
    <summary>按分类浏览 <span class="muted">全部馆藏</span></summary>
    <nav class="category-links" aria-label="作品分类"><RouterLink v-for="category in categories" :key="category.name" :to="`/category/${encodeURIComponent(category.name)}`">{{ category.name }}<span>{{ category.count.toLocaleString() }} 部</span></RouterLink></nav>
  </details>
</template>

<style scoped>
.category-directory { margin: 20px 0 28px; padding-block: 16px; border-bottom: 1px solid var(--line); }.category-directory summary { min-height: 28px; cursor: pointer; color: var(--green); font-size: 14px; }.category-directory summary span { margin-left: 12px; font-size: 12px; }.category-links { display: flex; flex-wrap: wrap; gap: 10px; margin-top: 18px; }.category-links a { min-height: 44px; display: inline-flex; flex-wrap: wrap; align-items: center; gap: 12px; padding: 10px 14px; border: 1px solid var(--line); border-radius: 6px; text-decoration: none; overflow-wrap: anywhere; }.category-links a:hover { background: var(--paper); }.category-links span { font-size: 12px; color: var(--muted); }
</style>
