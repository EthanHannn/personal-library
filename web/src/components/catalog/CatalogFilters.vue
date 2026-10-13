<script setup lang="ts">
import { computed, reactive, watch } from 'vue'
import type { Category } from '../../api'
import type { CatalogFilters } from '../../catalog/query'
import { defaults, sorts } from '../../catalog/query'

const props = defineProps<{ filters: CatalogFilters; categories: Category[] }>()
const emit = defineEmits<{ apply: [filters: CatalogFilters]; reset: [] }>()
const draft = reactive({ ...props.filters })
watch(() => props.filters, filters => Object.assign(draft, filters))
const subcategories = computed(() => [...new Set(props.categories
  .filter(category => !draft.tag1 || category.name === draft.tag1)
  .flatMap(category => category.subcategories.map(sub => sub.name)))])
const words = [
  { value: `0,${defaults.maxWords}`, label: '不限字数' },
  { value: '1,500000', label: '50 万字及以下' },
  { value: '500001,1000000', label: '50–100 万字' },
  { value: '1000001,2000000', label: '100–200 万字' },
  { value: `2000001,${defaults.maxWords}`, label: '200 万字以上' },
]
const wordRange = computed({
  get: () => `${draft.minWords},${draft.maxWords}`,
  set: (value: string) => { const [min, max] = value.split(','); draft.minWords = min!; draft.maxWords = max! },
})
const customWords = computed(() => !words.some(item => item.value === wordRange.value))
const featured = computed(() => props.filters.minScore === '7.5' && props.filters.maxScore === '10' && props.filters.onlyContent === '1')
const all = computed(() => props.filters.minScore === '0' && props.filters.maxScore === '10' && props.filters.onlyContent === '0')
function preset(high: boolean) {
  emit('apply', { ...draft, minScore: high ? '7.5' : '0', maxScore: '10', onlyContent: high ? '1' : '0' })
}
function submit() { emit('apply', { ...draft, search: draft.search.trim() }) }
function reset() { Object.assign(draft, defaults); emit('reset') }
</script>

<template>
  <section class="catalog-filters" aria-label="书库筛选">
    <div class="presets" role="group" aria-label="馆藏预设">
      <button type="button" :aria-pressed="featured" @click="preset(true)">高分有正文 <span>7.5+</span></button>
      <button type="button" :aria-pressed="all" @click="preset(false)">全部馆藏</button>
    </div>
    <form role="search" aria-label="搜索与筛选作品" @submit.prevent="submit">
      <label class="search-field" for="catalog-search">书名 / 作者<input id="catalog-search" v-model="draft.search" type="search" maxlength="100" placeholder="输入书名或作者" /></label>
      <div class="filter-grid">
        <label class="field" for="catalog-tag1">主分类<select id="catalog-tag1" v-model="draft.tag1" @change="draft.tag2 = ''"><option value="">全部分类</option><option v-if="draft.tag1 && !categories.some(c => c.name === draft.tag1)" :value="draft.tag1">{{ draft.tag1 }}</option><option v-for="category in categories" :key="category.name" :value="category.name">{{ category.name }}</option></select></label>
        <label class="field" for="catalog-tag2">副分类<select id="catalog-tag2" v-model="draft.tag2"><option value="">全部副分类</option><option v-if="draft.tag2 && !subcategories.includes(draft.tag2)" :value="draft.tag2">{{ draft.tag2 }}</option><option v-for="name in subcategories" :key="name" :value="name">{{ name }}</option></select></label>
        <fieldset class="score-range"><legend>评分范围</legend><div><label class="field" for="catalog-min-score"><span class="sr-only">最低评分</span><input id="catalog-min-score" v-model="draft.minScore" type="number" min="0" :max="draft.maxScore || '10'" step="0.1" required /></label><span aria-hidden="true">–</span><label class="field" for="catalog-max-score"><span class="sr-only">最高评分</span><input id="catalog-max-score" v-model="draft.maxScore" type="number" :min="draft.minScore || '0'" max="10" step="0.1" required /></label></div></fieldset>
        <label class="field" for="catalog-words">作品字数<select id="catalog-words" v-model="wordRange"><option v-if="customWords" :value="wordRange">自定字数范围</option><option v-for="range in words" :key="range.value" :value="range.value">{{ range.label }}</option></select></label>
        <label class="field" for="catalog-sort">排序<select id="catalog-sort" v-model="draft.sortBy"><option v-for="sort in sorts" :key="sort.value" :value="sort.value">{{ sort.label }}</option></select></label>
      </div>
      <div class="filter-actions">
        <label class="content-check"><input v-model="draft.onlyContent" type="checkbox" true-value="1" false-value="0" />只看有正文</label>
        <div class="submit-actions"><button class="text-button" type="button" @click="reset">重置筛选</button><button class="button" type="submit">查找作品</button></div>
      </div>
    </form>
  </section>
</template>

<style scoped>
.catalog-filters { padding: 26px; background: var(--paper); border: 1px solid var(--line); border-top: 3px solid var(--green); border-radius: 4px 4px 12px 12px; }
.presets { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 24px; }.presets button { min-height: 44px; border: 1px solid var(--line); background: transparent; color: var(--green); padding: 10px 16px; border-radius: 6px; font-size: 14px; }.presets button[aria-pressed="true"] { background: var(--green); border-color: var(--green); color: var(--paper); }.presets span { margin-left: 6px; }
.search-field, .field { display: grid; gap: 9px; min-width: 0; font-size: 13px; color: var(--muted); }.search-field { margin-bottom: 20px; }
input[type="search"], input[type="number"], select { width: 100%; min-width: 0; height: 44px; padding: 8px 10px; border: 1px solid var(--line); background: var(--paper); color: var(--ink); border-radius: 5px; font: 15px var(--sans); }
.filter-grid { display: grid; grid-template-columns: 1fr 1fr 1.25fr 1fr 1fr; gap: 16px; }.score-range { min-width: 0; margin: 0; padding: 0; border: 0; }.score-range legend { padding: 0; font-size: 13px; color: var(--muted); margin-bottom: 9px; }.score-range > div { display: flex; align-items: center; gap: 6px; }.score-range .field { flex: 1; }
.filter-actions { display: flex; align-items: center; justify-content: space-between; gap: 12px; flex-wrap: wrap; margin-top: 20px; }.content-check { display: flex; align-items: center; gap: 8px; min-height: 44px; font-size: 14px; }.content-check input { width: 18px; height: 18px; accent-color: var(--green); }.submit-actions { display: flex; gap: 22px; align-items: center; }
.sr-only { position: absolute; width: 1px; height: 1px; padding: 0; overflow: hidden; clip-path: inset(50%); white-space: nowrap; }
@media (max-width: 900px) { .filter-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
@media (max-width: 480px) { .catalog-filters { padding: 18px 14px; }.filter-grid { gap: 16px 10px; }.score-range { grid-column: span 2; }.presets button { padding-inline: 10px; }.filter-actions { align-items: stretch; }.submit-actions { width: 100%; justify-content: space-between; } }
</style>
