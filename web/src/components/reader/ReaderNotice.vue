<script setup lang="ts">
import type { Position, Progress } from '../../api'
defineProps<{ bookId: number; expired: boolean; taken: boolean; conflict: { local: Position; remote: Progress | null } | null; recovery: boolean; error: string }>()
const emit = defineEmits<{ choose: [which: 'local' | 'remote']; restart: []; retry: [] }>()
function label(position: Position | null) { return position ? `第 ${position.chapterIdx} 章 · ${Math.round(position.percent * 100)}%` : '尚无阅读记录' }
</script>

<template>
  <aside v-if="expired || taken || conflict || recovery || error" class="reader-notice" role="alert">
    <template v-if="expired"><p>登录已失效，阅读位置已留在本机。</p><a class="reader-button" :href="`/read/${bookId}`">重新登录并恢复</a></template>
    <template v-else-if="taken"><p>另一标签页正在阅读此书，本页已暂停保存。</p><a class="reader-button" :href="`/read/${bookId}`">在本页继续</a></template>
    <template v-else-if="conflict"><p>发现另一处阅读位置，请选择从哪里继续。</p><div class="reader-row conflict-actions"><button class="reader-button" @click="emit('choose', 'local')">本机：{{ label(conflict.local) }}</button><button class="reader-button" @click="emit('choose', 'remote')">服务端：{{ label(conflict.remote) }}</button></div></template>
    <template v-else-if="recovery"><p>正文或章节已变化，原位置仍保留。可打开目录选择章节，或从本章开头重新阅读。</p><button class="reader-button" @click="emit('restart')">从本章开头阅读</button></template>
    <template v-else><p>{{ error }}</p><button class="reader-button" @click="emit('retry')">重新加载</button></template>
  </aside>
</template>

<style scoped>
.reader-notice { padding: 12px 20px; background: var(--reader-soft); border-bottom: 1px solid var(--reader-border); font-size: 14px; max-height: 40dvh; overflow: auto; }.reader-notice p { margin: 0 0 12px; line-height: 1.7; }.conflict-actions { flex-wrap: wrap; }
</style>
