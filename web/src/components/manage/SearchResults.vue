<script setup lang="ts">
import { computed, nextTick, shallowRef, useTemplateRef } from 'vue'
import type { SearchCandidate, SearchSourceResult } from '../../sources'
import type { Job } from '../../manage'
import { useMutation } from '../../composables/useMutation'
const props = defineProps<{ candidates: SearchCandidate[]; sources: SearchSourceResult[] }>()
const emit = defineEmits<{ created: [job: Job] }>()
const selected = shallowRef<SearchCandidate | null>(null)
const limit = shallowRef(200)
const selection = useTemplateRef<HTMLFormElement>('selection')
async function choose(candidate: SearchCandidate) {
  selected.value = candidate
  await nextTick()
  selection.value?.scrollIntoView?.({ block: 'nearest', behavior: 'smooth' })
}
const { pending, error, run } = useMutation()
const groups = computed(() => {
  const out = new Map<string, { title: string; author: string; candidates: SearchCandidate[] }>()
  for (const candidate of props.candidates) {
    const normalize = (value: string) => value.replace(/\s+/gu, '').toLocaleLowerCase()
    const key = candidate.author ? JSON.stringify([normalize(candidate.title), normalize(candidate.author)]) : JSON.stringify([candidate.sourceId, candidate.url])
    if (!out.has(key)) out.set(key, { title: candidate.title, author: candidate.author, candidates: [] })
    out.get(key)!.candidates.push(candidate)
  }
  return [...out.entries()].map(([key, group]) => ({ key, ...group }))
})
async function collect() {
  if (!selected.value) return
  const c = selected.value
  const job = await run<Job>('/api/v1/manage/jobs', 'POST', { kind: 'collect', input: { sourceId: c.sourceId, sourceVersion: c.sourceVersion, url: c.url, title: c.title, author: c.author, maxChapters: limit.value } })
  if (job) emit('created', job)
}
</script>
<template>
  <section v-if="error?.status !== 401" class="search-results">
    <h3>搜索结果</h3>
    <ul class="search-report"><li v-for="source in sources" :key="source.id">{{ source.name }} · {{ source.error || `${source.count} 个候选` }}</li></ul>
    <p v-if="!candidates.length" class="muted">暂未找到候选。可核对书名、检查书源，或直接使用作品链接采集。</p>
    <form v-if="selected" ref="selection" class="manage-form selection" @submit.prevent="collect">
      <h4>已选：{{ selected.title }} · {{ selected.sourceName }}</h4>
      <p>{{ selected.author || '来源未提供作者，请核对' }} · {{ selected.url }}</p>
      <label>本次最多采集章数<input v-model.number="limit" type="number" min="1" max="2000" required></label>
      <p class="muted">完成后先检查预览，再确认入库。部分正文入库后，暂不支持自动追加或替换。</p>
      <div class="manage-actions"><button class="button" :disabled="pending">{{ pending ? '正在创建…' : '采集所选来源并预览' }}</button><button type="button" class="text-button" :disabled="pending" @click="selected = null">取消选择</button></div>
      <p v-if="error" role="alert">{{ error.message }}</p>
    </form>
    <article v-for="group in groups" :key="group.key" class="candidate-group">
      <h4>{{ group.title }} <span class="muted">{{ group.author || '作者待核对' }}</span></h4>
      <ul><li v-for="candidate in group.candidates" :key="`${candidate.sourceId}:${candidate.url}`">
        <div><strong>{{ candidate.sourceName }}</strong><p class="muted">{{ candidate.tag1 || '未标注分类' }}<span v-if="candidate.latest"> · {{ candidate.latest }}</span></p><p v-if="candidate.needsReview" class="muted">作者缺失或与检索作者不符，请核对作品。</p><p class="muted candidate-url">{{ candidate.url }}</p></div>
        <button class="button secondary" :disabled="pending" :aria-pressed="selected === candidate" @click="choose(candidate)">{{ selected === candidate ? '已选择' : '选择此来源' }}</button>
      </li></ul>
    </article>
  </section>
</template>
<style scoped>
.search-report { color: var(--muted); font-size: 14px; }.candidate-group { border-top: 1px solid var(--line); padding: 18px 0; }.candidate-group h4 { margin: 0 0 14px; font: 700 20px/1.6 var(--serif); }.candidate-group h4 span { font: 14px var(--sans); }.candidate-group ul { list-style: none; padding: 0; }.candidate-group li { display: flex; flex-wrap: wrap; align-items: center; justify-content: space-between; gap: 18px; margin-bottom: 20px; }.candidate-group li > div { flex: 1 1 220px; min-width: 0; }.candidate-group p { margin: 6px 0; font-size: 13px; }.candidate-url,.selection { overflow-wrap: anywhere; }.selection { padding: 18px; border: 1px solid var(--green); border-radius: 10px; }
</style>
