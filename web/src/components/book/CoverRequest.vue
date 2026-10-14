<script setup lang="ts">
import { shallowRef } from 'vue'
import type { Job } from '../../manage'
import { useMutation } from '../../composables/useMutation'
const props = defineProps<{ bookId: number }>()
const { pending, error, run } = useMutation()
const job = shallowRef<Job | null>(null)
async function request() { job.value = await run<Job>(`/api/v1/books/${props.bookId}/cover-jobs`, 'POST', {}) }
</script>
<template><div class="cover-request"><button v-if="!job" class="text-button" :disabled="pending" @click="request">{{ pending ? '正在排队…' : '检查并补全真实封面' }}</button><RouterLink v-else :to="{ path: '/manage', query: { tab: 'jobs', job: job.id } }">封面任务已提交，查看进展 →</RouterLink><p v-if="error" role="alert">{{ error.message }}</p></div></template>
<style scoped>.cover-request { font-size: 13px; margin-bottom: 18px; }.cover-request p { color: var(--ochre); }</style>
