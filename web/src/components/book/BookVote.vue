<script setup lang="ts">
import { computed, shallowRef } from 'vue'
import type { VoteSummary, VoteType } from '../../api'
import { useVoteAction } from '../../composables/useVoteAction'

const props = defineProps<{ summary: VoteSummary }>()
const emit = defineEmits<{ change: [summary: VoteSummary] }>()
const choices: { type: VoteType; label: string; points: number }[] = [
  { type: 'XIANCAO', label: '仙草', points: 10 }, { type: 'LIANGCAO', label: '粮草', points: 7 },
  { type: 'GANCAO', label: '干草', points: 5 }, { type: 'KUCAO', label: '枯草', points: 3 },
  { type: 'DUCAO', label: '毒草', points: 1 },
]
const { pending, desired, error, set } = useVoteAction(() => props.summary.bookId)
const notice = shallowRef('')
const selectedLabel = computed(() => choices.find(choice => choice.type === props.summary.myVote)?.label)
const retryLabel = computed(() => choices.find(choice => choice.type === desired.value)?.label)
const distribution = computed(() => choices.map(choice => ({
  ...choice, count: props.summary.current.counts[choice.type],
  percent: props.summary.current.popularity ? props.summary.current.counts[choice.type] / props.summary.current.popularity * 100 : 0,
})))

async function choose(type: VoteType) {
  notice.value = ''
  const result = await set(type)
  if (result) {
    emit('change', result)
    notice.value = `已保存为${choices.find(choice => choice.type === result.myVote)?.label}。`
  }
}
function login() { window.location.reload() }
</script>

<template>
  <section class="book-vote" aria-labelledby="my-vote-title">
    <div class="vote-heading"><h2 id="my-vote-title">我的评价</h2><span class="vote-selection">{{ selectedLabel ? `已选${selectedLabel}` : '尚未评价' }}</span></div>
    <p class="vote-hint muted">给这本书留一个评价，以后也可以修改。</p>
    <div class="vote-choices" role="group" aria-label="五档评价" :aria-busy="pending">
      <button v-for="choice in choices" :key="choice.type" type="button" class="vote-choice" :aria-pressed="summary.myVote === choice.type" :disabled="pending" @click="choose(choice.type)">
        <strong>{{ choice.label }}</strong><span>{{ choice.points }} 分</span>
      </button>
    </div>
    <p class="vote-notice muted" role="status">{{ pending ? '正在保存评价…' : notice }}</p>
    <div v-if="error" class="vote-error" role="alert">
      <p>{{ error.message }}</p>
      <button v-if="error.status === 401 || error.status === 403" class="text-button" @click="login">重新登录</button>
      <button v-else class="text-button" :disabled="pending" @click="desired && choose(desired)">重试保存{{ retryLabel }}</button>
    </div>
    <details class="vote-distribution">
      <summary>票数分布 · {{ summary.current.popularity.toLocaleString() }} 票</summary>
      <p class="vote-hint muted">包含历史票数及你的投票调整。来源评分保持独立。</p>
      <dl class="vote-counts">
        <div v-for="item in distribution" :key="item.type" class="vote-count-row">
          <dt>{{ item.label }}</dt><dd><span class="vote-track" aria-hidden="true"><span :style="{ width: `${item.percent}%` }" /></span><span>{{ item.count.toLocaleString() }} 票</span></dd>
        </div>
      </dl>
    </details>
  </section>
</template>

<style scoped>
.book-vote { margin-top: 28px; padding: 26px; border: 1px solid var(--line); border-radius: 14px; background: var(--paper); }
.vote-heading { display: flex; align-items: baseline; justify-content: space-between; flex-wrap: wrap; gap: 12px; }.vote-heading h2 { margin: 0; color: var(--green); font: 700 24px/1.4 var(--serif); }.vote-selection { color: var(--green); font-size: 14px; }
.vote-hint { margin: 12px 0 20px; font-size: 13px; }.vote-choices { display: grid; grid-template-columns: repeat(5, minmax(0, 1fr)); gap: 10px; }
.vote-choice { display: flex; flex-direction: column; gap: 7px; align-items: center; justify-content: center; min-height: 76px; border: 1px solid var(--line); border-radius: 8px; color: var(--green); background: transparent; }.vote-choice strong { font-size: 16px; }.vote-choice span { font-size: 12px; }.vote-choice[aria-pressed="true"] { color: var(--paper); background: var(--green); border-color: var(--green); }.vote-choice:hover:not(:disabled) { border-color: var(--green); }.vote-choice:disabled { opacity: .6; cursor: wait; }
.vote-notice { min-height: 24px; margin: 10px 0; font-size: 13px; }.vote-error { color: var(--ochre); font-size: 14px; }.vote-error p { margin-bottom: 0; }
.vote-distribution { border-top: 1px solid var(--line); padding-top: 10px; }.vote-distribution summary { min-height: 44px; padding-block: 12px; cursor: pointer; font-size: 14px; color: var(--green); }.vote-counts { margin: 0; }.vote-count-row { display: flex; align-items: center; gap: 16px; margin: 14px 0; font-size: 13px; }.vote-count-row dt { flex: 0 0 2em; }.vote-count-row dd { flex: 1; min-width: 0; display: flex; align-items: center; gap: 12px; margin: 0; }.vote-count-row dd > span:last-child { min-width: 5em; text-align: right; font-variant-numeric: tabular-nums; overflow-wrap: anywhere; }.vote-track { height: 5px; flex: 1; background: var(--line); border-radius: 3px; overflow: hidden; }.vote-track > span { display: block; height: 100%; background: var(--green); }
@media (max-width: 480px) { .book-vote { padding: 18px 16px; }.vote-choices { grid-template-columns: repeat(3, minmax(0, 1fr)); }.vote-count-row { gap: 10px; } }
</style>
