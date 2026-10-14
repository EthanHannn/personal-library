<script setup lang="ts">
import { shallowRef } from 'vue'
import type { BookSource } from '../../sources'
import type { Job } from '../../manage'
import { useMutation } from '../../composables/useMutation'
import RequestStatus from '../RequestStatus.vue'
const props = defineProps<{ source: BookSource }>()
const emit = defineEmits<{ created: [job: Job] }>()
const url = shallowRef(''), query = shallowRef('')
const { pending, error, run } = useMutation()
async function check() {
  const job = await run<Job>('/api/v1/manage/jobs', 'POST', { kind: 'source-check', input: { sourceId: props.source.id, sourceVersion: props.source.version, url: url.value, query: query.value } })
  if (job) emit('created', job)
}
</script>
<template>
  <form v-if="error?.status !== 401" class="manage-form" @submit.prevent="check">
    <h3>检查书源 · {{ source.config.name }}</h3><p class="muted">读取样本作品的详情、目录与首章，结果保存在任务记录中。</p>
    <label>样本作品链接<input v-model="url" type="url" required maxlength="2000" :placeholder="source.config.baseUrl"></label>
    <label v-if="source.config.search">同时检查搜索（可选）<input v-model="query" maxlength="100" placeholder="输入样本书名"></label>
    <button class="button secondary" :disabled="pending">{{ pending ? '正在创建…' : '检查样本' }}</button>
  </form>
  <RequestStatus :pending="false" :error="error" @retry="check" />
</template>
