<script setup lang="ts">
import { computed, ref, shallowRef } from 'vue'
import type { BookSource } from '../../sources'
import type { Job } from '../../manage'
import { useMutation } from '../../composables/useMutation'
import RequestStatus from '../RequestStatus.vue'
const props = defineProps<{ sources: BookSource[] }>()
const emit = defineEmits<{ created: [job: Job] }>()
const available = computed(() => props.sources.filter(s => s.enabled && s.config.search))
const selected = ref(available.value.slice(0, 8).map(s => s.id))
const query = shallowRef(''), author = shallowRef('')
const { pending, error, run } = useMutation()
async function search() {
  const job = await run<Job>('/api/v1/manage/jobs', 'POST', { kind: 'search', input: { query: query.value, author: author.value, sourceIds: selected.value } })
  if (job) emit('created', job)
}
</script>
<template>
  <form v-if="error?.status !== 401" class="manage-form" @submit.prevent="search">
    <h2>多源搜书</h2><p class="muted">搜索已配置的网站，每个来源最多显示 20 个候选。结果按书名和作者分组，选定来源后再采集。</p>
    <div class="form-pair"><label>书名<input v-model="query" required maxlength="100" placeholder="输入想收录的作品"></label><label>作者（可选，用于核对）<input v-model="author" maxlength="100"></label></div>
    <fieldset v-if="available.length"><legend>参与搜索的书源（最多 8 个）</legend><label v-for="source in available" :key="source.id" class="source-option"><input v-model="selected" type="checkbox" :value="source.id">{{ source.config.name }}<span class="muted">{{ source.config.group }}</span></label></fieldset>
    <p v-else class="muted">还没有支持搜索的启用书源。请到“我的书源”添加带搜索规则的书源。</p>
    <button class="button" :disabled="pending || selected.length < 1 || selected.length > 8">{{ pending ? '正在创建…' : '在所选书源中搜索' }}</button>
  </form>
  <RequestStatus :pending="false" :error="error" @retry="search" />
</template>
<style scoped>
fieldset { margin: 20px 0; padding: 16px; border: 1px solid var(--line); border-radius: 8px; }legend { padding: 0 8px; font-size: 14px; }.manage-form .source-option { display: flex; flex-direction: row; align-items: center; flex-wrap: wrap; gap: 10px; margin: 10px 0; }.manage-form .source-option input { width: 18px; height: 18px; flex: 0 0 auto; accent-color: var(--green); }
</style>
