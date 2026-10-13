<script setup lang="ts">
import type { ContentBlock } from '../../api'
defineProps<{ book: ContentBlock['book'] | null; bookId: number; chapter: number; previous: number | null; next: number | null; loading: boolean; blocked: boolean; inShelf: boolean; message: string; speaking: boolean }>()
const emit = defineEmits<{ navigate: [chapter: number]; panel: [name: 'directory' | 'settings' | 'tts']; retry: [] }>()
</script>

<template>
  <header class="reader-toolbar">
    <div class="reader-title-row"><RouterLink class="reader-back" :to="`/book/${bookId}`" aria-label="返回书目详情">← 书目</RouterLink><div class="reader-book"><strong>{{ book?.title ?? '正在打开书页…' }}</strong><span class="reader-muted">{{ book?.author }}<template v-if="inShelf"> · 已在书架</template></span></div><span class="reader-count">{{ chapter }} / {{ book?.chapterCount ?? '—' }}</span></div>
    <div class="reader-sync" role="status">{{ loading ? '正在打开章节…' : message }}<button v-if="message.includes('重试')" class="reader-retry" @click="emit('retry')">重试同步</button></div>
  </header>
  <nav class="reader-navigation" aria-label="阅读操作"><button class="reader-button" :disabled="previous === null || blocked" @click="previous !== null && emit('navigate', previous)">← 上一章</button><button class="reader-button" :disabled="blocked" @click="emit('panel', 'directory')">目录</button><button class="reader-button" @click="emit('panel', 'settings')">设置</button><button class="reader-button" :disabled="blocked" @click="emit('panel', 'tts')">{{ speaking ? '朗读中' : '朗读' }}</button><button class="reader-button" :disabled="next === null || blocked" @click="next !== null && emit('navigate', next)">下一章 →</button></nav>
</template>

<style scoped>
.reader-toolbar { padding: 14px max(18px, calc((100vw - 1040px) / 2)); background: var(--reader-paper); border-bottom: 1px solid var(--reader-border); flex-shrink: 0; }
.reader-title-row { display: flex; align-items: center; gap: 22px; }.reader-back { white-space: nowrap; text-decoration: none; font-size: 14px; }.reader-book { display: grid; gap: 5px; min-width: 0; flex: 1; }.reader-book strong { font-family: var(--serif); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }.reader-book span { font-size: 12px; }.reader-count { white-space: nowrap; font-size: 12px; color: var(--reader-muted); }
.reader-sync { color: var(--reader-muted); font-size: 12px; margin-top: 9px; }.reader-retry { margin-left: 10px; border: 0; background: none; color: var(--reader-accent); text-decoration: underline; }
.reader-navigation { order: 10; display: flex; justify-content: center; gap: 10px; padding: 10px 12px max(10px, env(safe-area-inset-bottom)); border-top: 1px solid var(--reader-border); background: var(--reader-paper); flex-shrink: 0; }
@media (max-width: 420px) { .reader-title-row { gap: 12px; }.reader-navigation { gap: 4px; padding-inline: 8px; }.reader-navigation .reader-button { padding-inline: 8px; font-size: 12px; min-height: 44px; white-space: nowrap; flex-shrink: 0; } }
</style>
