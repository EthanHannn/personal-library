<script setup lang="ts">
import { computed, nextTick, onMounted, onBeforeUnmount, shallowRef, useTemplateRef, watch } from 'vue'
import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router'
import { requestJSON } from '../../api'
import type { ContentBlock, Identity, Position } from '../../api'
import { useChapterLoader } from '../../composables/useChapterLoader'
import { useReadingProgress } from '../../composables/useReadingProgress'
import { useReaderPreferences } from '../../composables/useReaderPreferences'
import { useReaderTts } from '../../composables/useReaderTts'
import { paragraphs as parseParagraphs } from '../../reader/text'
import type { Paragraph } from '../../reader/text'
import type { Preferences } from '../../reader/themes'
import ReaderToolbar from './ReaderToolbar.vue'
import ReaderNotice from './ReaderNotice.vue'
import ReaderViewport from './ReaderViewport.vue'
import ReaderDialog from './ReaderDialog.vue'
import ChapterDrawer from './ChapterDrawer.vue'
import ReaderSettings from './ReaderSettings.vue'
import TtsControls from './TtsControls.vue'

const props = defineProps<{ bookId: number }>()
const route = useRoute(); const router = useRouter()
const loader = useChapterLoader(props.bookId)
const progress = useReadingProgress(props.bookId)
const prefs = useReaderPreferences()
const viewport = useTemplateRef('viewport')
const panel = shallowRef<'directory' | 'settings' | 'tts' | null>(null)
const recovery = shallowRef(false)
const checkingSession = shallowRef(false)
const visibleIndex = shallowRef(0)
const paragraphCache = new WeakMap<ContentBlock, Paragraph[]>()
const paragraphs = computed(() => loader.blocks.value.flatMap(block => {
  let value = paragraphCache.get(block)
  if (!value) { value = parseParagraphs(block); paragraphCache.set(block, value) }
  return value
}))
const tts = useReaderTts(paragraphs, prefs.preferences, index => viewport.value?.follow(index), async () => {
  if (blocked.value || document.hidden || !loader.blocks.value.at(-1)?.nextCursor) return null
  const count = paragraphs.value.length
  const previousBlock = loader.blocks.value.at(-1)
  await more(true)
  if (blocked.value || previousBlock === loader.blocks.value.at(-1)) return null
  return paragraphs.value.length > count ? count : 0
})
const blocked = computed(() => progress.expired.value || progress.taken.value || !!progress.conflict.value || checkingSession.value)
const chapter = computed(() => loader.state.value?.content?.chapter.idx ?? (Number(route.query.c) || 1))
const panelTitle = computed(() => ({ directory: '章节目录', settings: '阅读设置', tts: '语音朗读' })[panel.value ?? 'directory'])
let generation = 0
let startup: Promise<Position | null> | null = null
let started = false
let disposed = false

function snapshot() {
  const position = viewport.value?.capture()
  if (position) progress.capture(position)
}

async function restore(position: Position | null) {
  await nextTick()
  const ok = await viewport.value?.restore(position)
  if (ok === false) { recovery.value = true; progress.ready.value = false; return }
  progress.restored(viewport.value?.capture() ?? null)
  if (!blocked.value) loader.prefetch()
}

async function open() {
  const ownGeneration = ++generation
  snapshot(); progress.ready.value = false; tts.stop(); panel.value = null
  void progress.persist(); void progress.flush()
  const raw = route.query.c
  const explicit = raw !== undefined
  const selected = explicit && typeof raw === 'string' && /^[1-9]\d*$/.test(raw) ? Number(raw) : undefined
  if (explicit && (!selected || !Number.isSafeInteger(selected))) { loader.error.value = '章节编号无效，请从目录选择章节'; return }
  const first = !started
  let state = await loader.load(selected, null, first)
  if (!state || ownGeneration !== generation || disposed) return
  checkingSession.value = false
  if (!startup) startup = progress.start(state.progress)
  const saved = await startup
  if (ownGeneration !== generation || disposed) return
  started = true
  recovery.value = state.recoveryRequired
  const target = first && (!explicit || saved?.chapterIdx === selected) ? saved : null
  if (recovery.value && !explicit) return
  if (target && (target.chapterIdx !== state.content?.chapter.idx || (target.anchor && target.anchor.block !== state.content?.blockToken) || (!target.anchor && target.percent > 0 && state.content?.nextCursor))) {
    state = await loader.load(target.chapterIdx, target)
    if (!state || ownGeneration !== generation || disposed) { if (ownGeneration === generation) recovery.value = true; return }
  }
  if (ownGeneration === generation) await restore(target)
}

async function navigate(index: number) {
  if (blocked.value || !Number.isSafeInteger(index) || index < 1) return
  if (recovery.value) {
    const state = await loader.load(index)
    if (!state) return
    await progress.restartConfirmed(); recovery.value = false
  }
  panel.value = null
  if (String(index) === route.query.c) void open()
  else void router.push({ path: `/read/${props.bookId}`, query: { c: String(index) } })
}

async function choose(which: 'local' | 'remote') {
  tts.stop(); progress.ready.value = false
  const position = await progress.choose(which)
  const state = await loader.load(position?.chapterIdx ?? 1, position)
  if (state) { recovery.value = false; await restore(position) }
  else recovery.value = true
}

async function restart() {
  const index = loader.state.value?.content?.chapter.idx ?? 1
  const state = await loader.load(index)
  if (state) { await progress.restartConfirmed(); recovery.value = false; await restore(null) }
}

async function more(fromSpeech = false) {
  snapshot(); await progress.persist()
  const replaced = await loader.append()
  if (replaced) { if (!fromSpeech) tts.stop(); await restore(null) }
}

async function settings(value: Preferences) {
  const position = viewport.value?.capture() ?? null
  prefs.update(value)
  await nextTick()
  if (position) await viewport.value?.restore(position)
}

function keyboard(event: KeyboardEvent) {
  if (panel.value || blocked.value || event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return
  const target = event.target as HTMLElement
  if (target.closest('input, textarea, select, [contenteditable=true]')) return
  const next = event.key === 'ArrowRight' ? loader.state.value?.next : event.key === 'ArrowLeft' ? loader.state.value?.previous : null
  if (next) { event.preventDefault(); navigate(next.idx) }
}

async function visibility() {
  if (document.hidden) {
    snapshot(); loader.pausePrefetch(); await progress.persist(); void progress.flush(true)
  } else if (!progress.expired.value) {
    checkingSession.value = true
    try {
      await requestJSON<Identity>('/api/v1/me', { signal: AbortSignal.timeout(5000) })
      await progress.revalidate(); checkingSession.value = false
      if (!blocked.value && !disposed) loader.prefetch()
    } catch { loader.pausePrefetch(); if (!progress.expired.value) loader.error.value = '无法确认登录状态，请恢复网络后重试' }
  }
}
function leave() { snapshot(); void progress.persist(); void progress.flush(true); tts.stop(); loader.pausePrefetch() }
watch(() => route.query.c, () => { void open() }, { immediate: true })
watch(blocked, value => { if (value) { panel.value = null; tts.stop(); loader.pausePrefetch() } })
onMounted(() => { window.addEventListener('keydown', keyboard); window.addEventListener('pagehide', leave); document.addEventListener('visibilitychange', visibility) })
onBeforeRouteLeave(async () => { snapshot(); await progress.persist(); void progress.flush(true); tts.stop() })
onBeforeUnmount(() => {
  leave(); disposed = true; generation++
  window.removeEventListener('keydown', keyboard); window.removeEventListener('pagehide', leave); document.removeEventListener('visibilitychange', visibility)
})
</script>

<template>
  <section class="reader-session" :style="prefs.style.value">
    <ReaderToolbar :book="loader.state.value?.book ?? null" :book-id="bookId" :chapter="chapter" :previous="loader.state.value?.previous?.idx ?? null" :next="loader.state.value?.next?.idx ?? null" :in-shelf="loader.state.value?.inShelf ?? false" :loading="loader.loading.value" :blocked="blocked" :message="progress.message.value" :speaking="tts.status.value === 'playing'" @navigate="navigate" @panel="panel = $event" @retry="progress.flush()" />
    <ReaderNotice :book-id="bookId" :expired="progress.expired.value" :taken="progress.taken.value" :conflict="progress.conflict.value" :recovery="recovery" :error="loader.error.value" @choose="choose" @restart="restart" @retry="open" />
    <ReaderViewport v-if="loader.blocks.value.length" ref="viewport" :paragraphs="paragraphs" :title="loader.state.value?.content?.chapter.title ?? ''" :font-size="prefs.preferences.value.fontSize" :colorize="prefs.preferences.value.colorize" :highlight="tts.status.value === 'idle' ? -1 : tts.currentIndex.value" :has-more="!!loader.blocks.value.at(-1)?.nextCursor" :loading="loader.loading.value" @position="progress.capture" @visible="visibleIndex = $event" @more="more" />
    <div v-else class="reader-empty" role="status">{{ loader.loading.value ? '正在翻开书页…' : '请选择章节或恢复阅读位置' }}</div>
    <ReaderDialog v-if="panel" :title="panelTitle" @close="panel = null">
      <ChapterDrawer v-if="panel === 'directory'" :book-id="bookId" :current="chapter" :total="loader.state.value?.book.chapterCount ?? 0" @navigate="navigate" />
      <ReaderSettings v-else-if="panel === 'settings'" :preferences="prefs.preferences.value" :warning="prefs.warning.value" @update="settings" />
      <TtsControls v-else :supported="tts.supported.value" :voices="tts.voices.value" :status="tts.status.value" :preferences="prefs.preferences.value" :index="tts.currentIndex.value" :total="paragraphs.length" :error="tts.error.value" @toggle="tts.toggle(visibleIndex)" @stop="tts.stop" @previous="tts.start(tts.currentIndex.value - 1)" @next="tts.start(tts.currentIndex.value + 1)" @update="prefs.update({ ...prefs.preferences.value, ...$event })" />
    </ReaderDialog>
  </section>
</template>

<style>
.reader-session { height: 100dvh; display: flex; flex-direction: column; background: var(--reader-shell); color: var(--reader-text); }
.reader-session a { color: var(--reader-title); }.reader-session :focus-visible { outline-color: var(--reader-accent); }
.reader-button { display: inline-flex; align-items: center; justify-content: center; min-height: 42px; padding: 9px 14px; border: 1px solid var(--reader-border); border-radius: 6px; background: var(--reader-paper); color: var(--reader-text); font: 14px var(--sans); text-decoration: none; }
.reader-button:hover:not(:disabled) { background: var(--reader-soft); }.reader-button.primary { background: var(--reader-soft); border-color: var(--reader-accent); }.reader-button:disabled { opacity: .45; cursor: default; }
.reader-muted { color: var(--reader-muted); }.reader-row { display: flex; align-items: center; gap: 8px; }
.reader-session input:not([type=range]):not([type=checkbox]), .reader-session select { min-height: 42px; border: 1px solid var(--reader-border); border-radius: 5px; background: var(--reader-paper); color: var(--reader-text); padding: 8px; font: inherit; }
.reader-empty { flex: 1; display: grid; place-items: center; padding: 30px; color: var(--reader-muted); }
</style>
