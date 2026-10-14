<script setup lang="ts">
import { computed, shallowRef } from 'vue'
import type { Job } from '../../manage'
import type { SourcesPage } from '../../sources'
import { useMutation } from '../../composables/useMutation'
import { useRequest } from '../../composables/useRequest'
import RequestStatus from '../RequestStatus.vue'
import SourceSearchForm from './SourceSearchForm.vue'
const emit = defineEmits<{ created: [job: Job] }>()
const { data, pending: loading, error: loadError, reload } = useRequest<SourcesPage>('/api/v1/manage/sources')
const mode = shallowRef<'url' | 'search'>('url')
const sourceId = shallowRef('qidian'), url = shallowRef('')
const limit = shallowRef(200)
const enabled = computed(() => data.value?.sources.filter(s => s.enabled) ?? [])
const selected = computed(() => enabled.value.find(s => s.id === sourceId.value))
const { pending, error, run } = useMutation()
async function submit() {
  if (!selected.value) return
  const job = await run<Job>('/api/v1/manage/jobs', 'POST', { kind: 'collect', input: { url: url.value, maxChapters: limit.value, sourceId: sourceId.value, sourceVersion: selected.value.version } })
  if (job) emit('created', job)
}
</script>

<template>
  <RequestStatus :pending="loading" :error="loadError" @retry="reload" />
  <section v-if="data && error?.status !== 401">
    <div class="manage-actions"><button class="button secondary" :aria-pressed="mode === 'url'" @click="mode = 'url'">作品链接</button><button class="button secondary" :aria-pressed="mode === 'search'" @click="mode = 'search'">多源搜书</button></div>
    <SourceSearchForm v-if="mode === 'search'" :sources="data.sources" @created="emit('created', $event)" />
    <form v-else class="manage-form" @submit.prevent="submit">
      <h2>网络采集</h2><p class="muted">选择书源并填写作品链接，采集书目、目录和正文。完成后检查预览，再确认加入本地书库。</p>
      <label>正文书源<select v-model="sourceId" required><option value="" disabled>请选择书源</option><option v-for="source in enabled" :key="source.id" :value="source.id">{{ source.config.name }} · {{ source.config.group }}</option></select></label>
      <p v-if="!enabled.length" class="muted">请先在“我的书源”添加或启用书源。</p>
      <label>作品链接<input v-model="url" type="url" required maxlength="2000" :placeholder="selected?.config.baseUrl || 'https://…'"></label>
      <label>本次最多采集章数<input v-model.number="limit" type="number" required min="1" max="2000"></label>
      <p v-if="sourceId === 'qidian'" class="muted">起点适配器只读取公开免费章节，付费章节不采集。其他网站请添加对应书源后选择。</p>
      <p class="muted">任务逐章保存进度。部分正文入库后，暂不支持自动追更或替换已有正文。</p>
      <button class="button" :disabled="pending || !selected">{{ pending ? '正在创建…' : '开始采集并预览' }}</button>
    </form>
  </section>
  <RequestStatus :pending="false" :error="error" @retry="submit" />
</template>
