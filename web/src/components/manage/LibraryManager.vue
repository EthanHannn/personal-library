<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import type { Job } from '../../manage'
import { jobNames, jobStates } from '../../manage'
import { useRequest } from '../../composables/useRequest'
import { useMutation } from '../../composables/useMutation'
import RequestStatus from '../RequestStatus.vue'
import ManagedCatalog from './ManagedCatalog.vue'
import CollectForm from './CollectForm.vue'
import ImportForm from './ImportForm.vue'
import JobDetail from './JobDetail.vue'
const route = useRoute(), router = useRouter()
const tab = computed(() => typeof route.query.tab === 'string' ? route.query.tab : 'books')
const job = computed(() => typeof route.query.job === 'string' ? route.query.job : '')
const path = computed(() => typeof route.query.path === 'string' ? route.query.path : '')
const { data, pending, error, reload } = useRequest<{ jobs: Job[]; nextCursor: string | null }>(() => `/api/v1/manage/jobs?cursor=${typeof route.query.cursor === 'string' ? encodeURIComponent(route.query.cursor) : ''}`)
const action = useMutation()
function created(job: Job) { reload(); void router.push({ path: '/manage', query: { tab: 'jobs', job: job.id } }) }
function updated(job: Job) { if (data.value) data.value = { ...data.value, jobs: data.value.jobs.map(item => item.id === job.id ? job : item) } }
async function scan() { const j = await action.run<Job>('/api/v1/manage/jobs', 'POST', { kind: 'scan', input: {} }); if (j) created(j) }
async function covers() { const result = await action.run<{ jobs: Job[] }>('/api/v1/manage/cover-batch', 'POST', {}); if (result?.jobs[0]) created(result.jobs[0]); else if (result) reload() }
</script>

<template>
  <header class="page-heading"><p class="eyebrow">整理我的藏书</p><h1>书库管理</h1><p class="muted">补全书目信息，收录新的作品，查看导入和封面任务。</p></header>
  <RequestStatus :pending="pending" :error="error" @retry="reload" />
  <template v-if="data && action.error.value?.status !== 401">
    <nav class="manage-tabs" aria-label="管理功能"><RouterLink v-for="(label, key) in { books: '书目', import: '导入 TXT', collect: '起点采集', jobs: '任务记录' }" :key="key" :to="{ path: '/manage', query: { tab: key } }" :aria-current="tab === key ? 'page' : undefined">{{ label }}</RouterLink></nav>
    <ManagedCatalog v-if="tab === 'books'" @created="created" />
    <ImportForm v-else-if="tab === 'import'" :key="path" :initial-path="path" @created="created" />
    <CollectForm v-else-if="tab === 'collect'" @created="created" />
    <template v-else>
      <div class="manage-actions"><button class="button secondary" :disabled="action.pending.value" @click="scan">检查正文与发现新 TXT</button><button class="button secondary" :disabled="action.pending.value" @click="covers">批量补全封面（最多 10 本）</button><button class="text-button" @click="reload">刷新任务列表</button></div>
      <p class="muted">任务按顺序处理；关闭页面后继续运行。等待确认的作品不会自动加入书库。</p>
      <JobDetail v-if="job" :key="job" :id="job" @update="updated" />
      <ul class="job-list"><li v-for="item in data.jobs" :key="item.id"><RouterLink :to="{ path: '/manage', query: { tab: 'jobs', job: item.id } }"><strong>{{ jobNames[item.kind] }}{{ item.input.title ? ` · ${item.input.title}` : item.input.bookId ? ` · 作品 ${item.input.bookId}` : '' }}</strong><span>{{ jobStates[item.status] }} · {{ new Date(item.createdAt).toLocaleString('zh-CN') }}</span></RouterLink></li></ul>
      <p v-if="!data.jobs.length" class="muted">还没有任务记录。</p>
      <div class="manage-actions"><RouterLink v-if="route.query.cursor" class="button secondary" to="/manage?tab=jobs">最近任务</RouterLink><RouterLink v-if="data.nextCursor" class="button secondary" :to="{ path: '/manage', query: { tab: 'jobs', cursor: data.nextCursor } }">更早的任务</RouterLink></div>
    </template>
  </template>
  <p v-if="action.error.value" role="alert">{{ action.error.value.message }}</p>
</template>

<style>
.manage-tabs { display: flex; flex-wrap: wrap; gap: 24px; margin-bottom: 28px; border-bottom: 1px solid var(--line); }.manage-tabs a { padding: 14px 0; text-decoration: none; border-bottom: 3px solid transparent; }.manage-tabs a[aria-current=page] { border-color: var(--green); font-weight: 700; }.manage-form { max-width: 780px; margin: 24px 0; }.manage-form h2 { font: 700 26px var(--serif); }.manage-form p { font-size: 14px; line-height: 1.9; }.manage-form label { display: flex; flex-direction: column; gap: 10px; margin-bottom: 18px; min-width: 0; font-size: 14px; }.manage-form input,.manage-form select,.manage-form textarea { width: 100%; min-width: 0; border: 1px solid var(--line); border-radius: 8px; background: var(--paper); color: var(--ink); padding: 12px; font: inherit; }.manage-form textarea { resize: vertical; }.form-pair { display: grid; grid-template-columns: repeat(2,minmax(0,1fr)); gap: 18px; }.manage-actions { display: flex; flex-wrap: wrap; align-items: center; gap: 18px; }.job-list { list-style: none; padding: 0; margin: 28px 0; }.job-list a { display: flex; justify-content: space-between; flex-wrap: wrap; gap: 12px; text-decoration: none; border-bottom: 1px solid var(--line); padding: 20px 0; overflow-wrap: anywhere; }.job-list span { font-size: 12px; color: var(--muted); }@media(max-width:450px) { .form-pair { grid-template-columns: minmax(0,1fr); gap: 0; }.manage-tabs { gap: 16px; font-size: 14px; } }
</style>
