<script setup lang="ts">
import { ref } from 'vue'
import type { Job } from '../../manage'
import { useMutation } from '../../composables/useMutation'
import RequestStatus from '../RequestStatus.vue'
const emit = defineEmits<{ created: [job: Job] }>()
const url = ref('')
const limit = ref(200)
const { pending, error, run } = useMutation()
async function submit() { const j = await run<Job>('/api/v1/manage/jobs', 'POST', { kind: 'collect', input: { url: url.value, maxChapters: limit.value } }); if (j) emit('created', j) }
</script>

<template>
  <form v-if="error?.status !== 401" class="manage-form" @submit.prevent="submit">
    <h2>从起点收录</h2><p class="muted">填写作品链接，采集书目信息与公开免费章节。任务按章节保存进度；完成后可检查目录和收录范围，再确认入库。</p>
    <label>起点作品链接<input v-model="url" type="url" required maxlength="1000" placeholder="https://www.qidian.com/book/作品编号/"></label>
    <label>本次最多采集章数<input v-model.number="limit" type="number" required min="1" max="2000"></label>
    <p class="muted">付费章节不采集。遇到访问限制或特殊字体会暂停，并保留已取得的章节。其他站点或已订阅内容，可用浏览器工具取得 TXT 后导入。</p>
    <button class="button" :disabled="pending">{{ pending ? '正在创建…' : '开始采集并预览' }}</button>
  </form>
  <RequestStatus :pending="false" :error="error" @retry="submit" />
</template>
