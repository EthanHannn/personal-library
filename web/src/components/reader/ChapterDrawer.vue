<script setup lang="ts">
import { computed, nextTick, shallowRef, useTemplateRef, watch } from 'vue'
import type { ContentBlock } from '../../api'
import { useRequest } from '../../composables/useRequest'

const props = defineProps<{ bookId: number; current: number; total: number }>()
const emit = defineEmits<{ navigate: [chapter: number] }>()
const after = shallowRef(Math.floor((props.current - 1) / 100) * 100)
const query = shallowRef('')
const input = shallowRef('')
const jump = shallowRef(props.current)
const scrollTop = shallowRef(0)
const active = shallowRef(0)
const list = useTemplateRef<HTMLDivElement>('list')
const history = shallowRef<number[]>([])
const { data, error, pending, reload } = useRequest<{ chapters: ContentBlock['chapter'][]; nextAfter: number | null }>(() => `/api/v1/books/${props.bookId}/directory?after=${after.value}&q=${encodeURIComponent(query.value)}`)
const start = computed(() => Math.max(0, Math.floor(scrollTop.value / 52) - 2))
const visible = computed(() => data.value?.chapters.slice(start.value, start.value + 12) ?? [])
watch(data, async value => {
  active.value = Math.max(0, value?.chapters.findIndex(c => c.idx === props.current) ?? 0)
  await nextTick()
  if (list.value) { list.value.scrollTop = active.value * 52; scrollTop.value = list.value.scrollTop }
})
function search() { after.value = 0; history.value = []; query.value = input.value.trim(); reload() }
function next() { if (data.value?.nextAfter !== null && data.value?.nextAfter !== undefined) { history.value = [...history.value, after.value]; after.value = data.value.nextAfter } }
function previous() { after.value = history.value.at(-1) ?? Math.max(0, after.value - 100); history.value = history.value.slice(0, -1) }
function locate() { query.value = ''; input.value = ''; history.value = []; after.value = Math.floor((props.current - 1) / 100) * 100; reload() }
function scroll(event: Event) {
  scrollTop.value = (event.target as HTMLElement).scrollTop
  if (active.value < start.value || active.value >= start.value + 12) active.value = Math.floor(scrollTop.value / 52)
}
function keyboard(event: KeyboardEvent) {
  const items = data.value?.chapters ?? []
  if (event.key === 'Enter') { if (items[active.value]) emit('navigate', items[active.value].idx); event.preventDefault(); return }
  const delta = event.key === 'ArrowDown' ? 1 : event.key === 'ArrowUp' ? -1 : 0
  if (!delta) return
  event.preventDefault(); active.value = Math.max(0, Math.min(items.length - 1, active.value + delta))
  if (list.value) { list.value.scrollTop = Math.max(0, active.value * 52 - 104); scrollTop.value = list.value.scrollTop }
}
</script>

<template>
  <form class="directory-search" @submit.prevent="search"><label for="chapter-search">查找本书章节</label><div class="reader-row"><input id="chapter-search" v-model="input" type="search" maxlength="80" placeholder="输入章节标题"><button class="reader-button" type="submit">查找</button></div></form>
  <form class="reader-row chapter-jump" @submit.prevent="emit('navigate', Number(jump))"><label for="chapter-number">跳到第</label><input id="chapter-number" v-model="jump" type="number" min="1" step="1" required><span>章</span><button class="reader-button" type="submit">前往</button><button class="reader-button" type="button" @click="locate">当前章</button></form>
  <p class="reader-muted">共 {{ total.toLocaleString() }} 章 · 当前第 {{ current }} 章</p>
  <p v-if="error" role="alert">{{ error.message }} <button class="reader-button" @click="reload">重试</button></p>
  <p v-if="pending" role="status">正在加载目录…</p>
  <div v-else ref="list" class="directory-list" role="listbox" tabindex="0" aria-label="章节目录" :aria-activedescendant="data?.chapters[active] ? `chapter-option-${data.chapters[active].idx}` : undefined" @scroll.passive="scroll" @keydown="keyboard">
    <div class="directory-space" :style="{ height: `${(data?.chapters.length ?? 0) * 52}px` }">
      <div v-for="(chapter, index) in visible" :id="`chapter-option-${chapter.idx}`" :key="chapter.idx" role="option" :aria-selected="chapter.idx === current" :aria-posinset="start + index + 1" :aria-setsize="data?.chapters.length" class="directory-entry" :class="{ current: chapter.idx === current, active: start + index === active }" :style="{ transform: `translateY(${(start + index) * 52}px)` }" :title="chapter.title" @click="emit('navigate', chapter.idx)">{{ chapter.idx }} · {{ chapter.title }}</div>
    </div>
    <p v-if="data?.chapters.length === 0" class="reader-muted">未找到章节</p>
  </div>
  <div class="reader-row directory-pages"><button class="reader-button" :disabled="after === 0 || pending" @click="previous">前 100 章</button><button class="reader-button" :disabled="!data?.nextAfter || pending" @click="next">后 100 章</button></div>
</template>

<style scoped>
.directory-search { display: grid; gap: 8px; }.directory-search input { flex: 1; min-width: 0; }
.chapter-jump { margin: 16px 0; flex-wrap: wrap; }.chapter-jump input { width: 70px; }
.directory-list { height: min(360px, 42dvh); overflow: auto; border-block: 1px solid var(--reader-border); }
.directory-space { position: relative; }.directory-entry { position: absolute; inset: 0 0 auto; height: 52px; padding: 14px 10px; cursor: pointer; white-space: nowrap; text-overflow: ellipsis; overflow: hidden; border-bottom: 1px solid var(--reader-border); }
.directory-entry.current { color: var(--reader-title); font-weight: 700; }.directory-entry:hover, .directory-entry.active { background: var(--reader-soft); }
.directory-pages { justify-content: space-between; margin-top: 16px; }
</style>
