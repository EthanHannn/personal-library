<script setup lang="ts">
import { computed } from 'vue'
import type { CatalogScope } from '../../catalog/query'
import { useCatalog } from '../../composables/useCatalog'
import BookList from './BookList.vue'
import CatalogFilters from './CatalogFilters.vue'
import CatalogPagination from './CatalogPagination.vue'
import CategoryDirectory from './CategoryDirectory.vue'
import RequestStatus from '../RequestStatus.vue'

const props = withDefaults(defineProps<{ kind?: CatalogScope['kind']; name?: string }>(), { kind: 'library', name: '' })
const scope = computed(() => ({ kind: props.kind, name: props.name }))
const { data, pending, error, reload, categories, filters, apply, reset, showAll, pageLink } = useCatalog(scope)
const { data: categoryData, error: categoryError, reload: reloadCategories } = categories
const title = computed(() => props.kind === 'library' ? '我的书库' : props.name)
const description = computed(() => props.kind === 'author' ? '这位作者在书屋中的作品。可切换到全部馆藏，查看未评分或尚无正文的书目。'
  : props.kind === 'category' ? '收录于这一分类的作品。按评分、篇幅或入库时间，找下一本想读的书。'
    : '从书名、作者或分类出发，找下一本想读的书。默认选出 7.5 分以上且有正文的作品。')
</script>

<template>
  <header class="page-heading">
    <RouterLink v-if="kind !== 'library'" class="back-link" to="/library">← 返回书库</RouterLink>
    <p class="eyebrow">{{ kind === 'author' ? '作者作品' : kind === 'category' ? '分类书目' : '藏书目录' }}</p>
    <h1>{{ title }}</h1><p class="muted">{{ description }}</p>
  </header>
  <CatalogFilters :filters="filters" :categories="categoryData?.categories ?? []" @apply="apply" @reset="reset" />
  <CategoryDirectory v-if="categoryData" :categories="categoryData.categories" />
  <RequestStatus v-if="categoryError && !error" :pending="false" :error="categoryError" @retry="reloadCategories" />
  <p v-if="filters.author || filters.tag" class="muted">限定范围：{{ filters.author }} {{ filters.tag }} · <button class="text-button" @click="reset">清除限定</button></p>
  <section class="catalog-results" aria-label="检索结果" :aria-busy="pending">
    <RequestStatus :pending="pending" :error="error" @retry="reload" />
    <template v-if="data">
      <div class="result-summary" role="status"><p>找到 <strong>{{ data.pagination.total.toLocaleString() }}</strong> 部作品<span> · {{ data.summary.authors.toLocaleString() }} 位作者 · {{ data.summary.readable.toLocaleString() }} 部有正文</span></p><p v-if="data.pagination.totalPages">第 {{ data.pagination.page }} / {{ data.pagination.totalPages }} 页</p></div>
      <BookList v-if="data.books.length" :books="data.books" :sort-by="filters.sortBy" />
      <div v-else class="empty-state"><h2>没有符合条件的作品</h2><p class="muted">试试更短的书名或作者名，或放宽评分、分类和正文条件。</p><button class="button secondary" @click="showAll">{{ kind === 'library' ? '查看全部馆藏' : '查看本页全部作品' }}</button></div>
      <CatalogPagination :pagination="data.pagination" :page-link="pageLink" />
    </template>
  </section>
</template>

<style scoped>
.back-link { display: inline-flex; align-items: center; min-height: 44px; font-size: 14px; }.catalog-results { margin-top: 28px; }.result-summary { display: flex; flex-wrap: wrap; justify-content: space-between; align-items: baseline; gap: 6px 16px; margin-bottom: 22px; font-size: 14px; color: var(--muted); }.result-summary p { margin: 0; line-height: 1.8; }.result-summary strong { color: var(--green); font-size: 22px; font-weight: 600; }.empty-state { padding: 40px 24px; text-align: center; border: 1px dashed var(--line); border-radius: 12px; }.empty-state h2 { font: 600 24px/1.5 var(--serif); color: var(--green); }
@media (max-width: 480px) { .result-summary span { display: block; } }
</style>
