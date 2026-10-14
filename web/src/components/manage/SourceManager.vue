<script setup lang="ts">
import { shallowRef } from 'vue'
import type { Job } from '../../manage'
import type { BookSource, SourcesPage } from '../../sources'
import { sourceHealth } from '../../sources'
import { useRequest } from '../../composables/useRequest'
import { useMutation } from '../../composables/useMutation'
import RequestStatus from '../RequestStatus.vue'
import SourceEditor from './SourceEditor.vue'
import SourceCheckForm from './SourceCheckForm.vue'

const emit = defineEmits<{ created: [job: Job] }>()
const { data, pending, error, reload } = useRequest<SourcesPage>('/api/v1/manage/sources')
const action = useMutation()
const editing = shallowRef<BookSource | 'new' | null>(null)
const checking = shallowRef('')
function saved() { editing.value = null; reload() }
async function toggle(source: BookSource) {
  const result = await action.run(`/api/v1/manage/sources/${encodeURIComponent(source.id)}`, 'PUT', { config: source.config, enabled: !source.enabled, expectedVersion: source.version })
  if (result) reload()
}
</script>
<template>
  <RequestStatus :pending="pending" :error="error" @retry="reload" />
  <section v-if="data && action.error.value?.status !== 401" class="source-manager">
    <div class="section-heading"><h2>我的书源</h2><button class="button" @click="editing = 'new'">添加书源</button></div>
    <p class="muted">为不同网站保存解析规则。启用的书源可用于网络采集；有搜索规则的书源也可参与多源搜书。</p>
    <SourceEditor v-if="editing" :key="editing === 'new' ? 'new' : editing.id" :source="editing === 'new' ? undefined : editing" :templates="data.templates" @saved="saved" @cancel="editing = null" />
    <ul class="sources-list">
      <li v-for="source in data.sources" :key="source.id">
        <h3>{{ source.config.name }} <span class="source-state">{{ source.enabled ? '已启用' : '已停用' }}</span></h3>
        <p class="muted">{{ source.config.group || '未分组' }} · {{ source.config.search ? '支持搜书和链接采集' : '作品链接采集' }} · 规则 v{{ source.version }}</p>
        <p class="muted">{{ source.config.baseUrl }}</p>
        <p role="status">{{ sourceHealth(source) }}<span v-if="source.checkedAt"> · {{ new Date(source.checkedAt).toLocaleString('zh-CN') }}</span></p>
        <div class="manage-actions"><button v-if="source.config.kind !== 'qidian'" class="text-button" @click="editing = source">编辑规则</button><button class="text-button" :disabled="action.pending.value" @click="toggle(source)">{{ source.enabled ? '停用' : '启用' }}</button><button v-if="source.config.kind !== 'qidian'" class="text-button" @click="checking = checking === source.id ? '' : source.id">{{ checking === source.id ? '收起检查' : '检查书源' }}</button></div>
        <p v-if="source.config.kind === 'qidian'" class="muted">内置规则；可用少量章节的采集任务检查公开章节访问情况。</p>
        <SourceCheckForm v-if="checking === source.id" :source="source" @created="emit('created', $event)" />
      </li>
    </ul>
  </section>
  <p v-if="action.error.value" role="alert">{{ action.error.value.message }} <button class="text-button" @click="reload">刷新书源</button></p>
</template>
<style scoped>
.source-manager { max-width: 900px; }.source-manager h2 { font: 700 26px var(--serif); }.source-manager p { font-size: 14px; line-height: 1.8; overflow-wrap: anywhere; }.sources-list { list-style: none; padding: 0; }.sources-list > li { padding: 24px 0; border-bottom: 1px solid var(--line); }.sources-list h3 { font-size: 18px; }.source-state { margin-left: 12px; font: 12px var(--sans); color: var(--muted); }
</style>
