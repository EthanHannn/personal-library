<script setup lang="ts">
import { watch } from 'vue'
import type { Job } from '../../manage'
import { useJob } from '../../composables/useJob'
import { useMutation } from '../../composables/useMutation'
import { jobNames, jobStates } from '../../manage'
import RequestStatus from '../RequestStatus.vue'
const props = defineProps<{ id: string }>()
const emit = defineEmits<{ change: []; update: [job: Job] }>()
const { data, error, pending, reload } = useJob(() => props.id)
const action = useMutation()
watch(data, value => { if (value) emit('update', value) })
async function act(name: string) { if (await action.run(`/api/v1/manage/jobs/${encodeURIComponent(props.id)}/${name}`, 'POST', {})) { emit('change'); await reload() } }
</script>

<template>
  <RequestStatus :pending="pending" :error="error" @retry="reload" />
  <article v-if="data && action.error.value?.status !== 401" class="job-detail">
    <div class="section-heading"><h2>{{ jobNames[data.kind] }} · {{ jobStates[data.status] }}</h2><button class="text-button" @click="reload">刷新状态</button></div>
    <p v-if="data.message && data.message !== 'confirm'" role="status">{{ data.message }}</p>
    <p v-if="data.nextAt > Date.now()" class="muted">可继续时间：{{ new Date(data.nextAt).toLocaleString('zh-CN') }}</p>
    <p v-if="data.result.planned" class="muted">已采集 {{ data.result.downloaded }} / {{ data.result.planned }} 章。</p>
    <template v-if="data.result.file">
      <h3>{{ data.result.title }} <span class="muted">{{ data.result.author }}</span></h3>
      <p class="muted">{{ data.result.chapterCount }} 章 · 约 {{ data.result.words?.toLocaleString() }} 字 · {{ data.result.file.encoding }} · {{ (data.result.file.size / 1024).toFixed(1) }} KiB</p>
      <ul v-if="data.result.warnings?.length" class="preview-warnings"><li v-for="warning in data.result.warnings" :key="warning">{{ warning }}</li></ul>
      <h4>目录预览（前 12 章）</h4><ol><li v-for="chapter in data.result.chapters" :key="chapter.idx">{{ chapter.title }}</li></ol>
      <p v-if="data.status === 'review'">核对书名、作者与目录后，确认即可在书库中阅读。</p>
      <RouterLink v-if="data.status === 'completed' && data.result.bookId" class="button" :to="`/book/${data.result.bookId}`">查看已收录作品</RouterLink>
    </template>
    <template v-if="data.kind === 'scan' && data.status === 'completed'">
      <p>已检查 {{ data.result.checked }} 条正文记录；缺失或不可读 {{ data.result.missing }} 条，文件有变化 {{ data.result.changed }} 条。</p>
      <ul><li v-for="issue in data.result.issues" :key="issue.bookId"><RouterLink :to="`/book/${issue.bookId}`">作品 {{ issue.bookId }}</RouterLink> · {{ issue.path }} · {{ issue.message }}</li></ul>
      <h3>可导入的 TXT</h3><ul><li v-for="path in data.result.candidates" :key="path"><RouterLink :to="{ path: '/manage', query: { tab: 'import', path } }">{{ path }}</RouterLink></li></ul>
      <p v-if="!data.result.candidates?.length" class="muted">本次没有发现未登记的 TXT。</p><p v-if="data.result.truncated" class="muted">结果较多，本次最多列出 200 个候选；仍可按相对路径导入其他文件。</p>
    </template>
    <RouterLink v-if="data.kind === 'cover' && data.status === 'completed' && data.result.bookId" class="button" :to="`/book/${data.result.bookId}`">查看封面</RouterLink>
    <div class="manage-actions"><button v-if="data.status === 'review'" class="button" :disabled="action.pending.value" @click="act('confirm')">确认入库</button><button v-if="data.status === 'failed'" class="button" :disabled="action.pending.value" @click="act('retry')">重新排队</button><button v-if="['queued', 'running', 'review', 'failed'].includes(data.status)" class="button secondary" :disabled="action.pending.value" @click="act('cancel')">取消任务</button></div>
    <p v-if="action.error.value" role="alert">{{ action.error.value.message }}</p>
  </article>
</template>

<style scoped>
.job-detail { margin: 24px 0; padding: 26px; border: 1px solid var(--line); border-radius: 14px; background: var(--paper); overflow-wrap: anywhere; }.job-detail h2 { font-size: 24px; }.job-detail h3 { font: 700 24px/1.6 var(--serif); }.job-detail h3 span { font: 14px var(--sans); }.job-detail li,.job-detail p { line-height: 1.9; }.job-detail ol,.job-detail ul { padding-left: 24px; }.preview-warnings { color: var(--ochre); }.job-detail .manage-actions { margin-top: 20px; }@media(max-width:450px) { .job-detail { padding: 18px; } }
</style>
