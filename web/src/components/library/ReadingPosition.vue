<script setup lang="ts">
import { computed } from 'vue'
import type { ProgressSummary } from '../../api'
const props = defineProps<{ progress: ProgressSummary | null; chapterCount: number }>()
const completion = computed(() => props.progress && props.chapterCount > 0 ? Math.min(100, Math.max(0, (props.progress.chapterIdx - 1 + props.progress.percent) / props.chapterCount * 100)) : 0)
const percent = computed(() => Math.round(completion.value))
</script>

<template>
  <div class="reading-position">
    <div class="position-label"><span>{{ progress ? `第 ${progress.chapterIdx.toLocaleString()} 章 · 章内 ${Math.round(progress.percent * 100)}%` : '尚未开始阅读' }}</span><span v-if="progress && chapterCount > 0">全书约 {{ percent }}%</span></div>
    <div v-if="progress && chapterCount > 0" class="progress-track" role="progressbar" aria-label="按章节估算的全书进度" :aria-valuenow="percent" :aria-valuemin="0" :aria-valuemax="100"><span :style="{ width: `${completion}%` }" /></div>
  </div>
</template>

<style scoped>
.reading-position { margin-top: 18px; }.position-label { display: flex; justify-content: space-between; flex-wrap: wrap; gap: 6px 14px; font-size: 12px; color: var(--muted); line-height: 1.7; }.progress-track { height: 5px; border-radius: 3px; background: var(--line); margin-top: 9px; overflow: hidden; }.progress-track span { display: block; height: 100%; background: var(--ochre); border-radius: inherit; }
</style>
