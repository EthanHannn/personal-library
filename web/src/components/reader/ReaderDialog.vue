<script setup lang="ts">
import { onMounted, onBeforeUnmount, useId, useTemplateRef } from 'vue'
defineProps<{ title: string }>()
const emit = defineEmits<{ close: [] }>()
const dialog = useTemplateRef<HTMLDialogElement>('dialog')
const titleId = useId()
onMounted(() => dialog.value?.showModal())
onBeforeUnmount(() => dialog.value?.close())
</script>

<template>
  <dialog ref="dialog" class="reader-dialog" :aria-labelledby="titleId" @close="emit('close')" @cancel.prevent="emit('close')">
    <header class="reader-dialog-heading"><h2 :id="titleId">{{ title }}</h2><button class="reader-button" autofocus @click="emit('close')">关闭</button></header>
    <slot />
  </dialog>
</template>

<style scoped>
.reader-dialog { width: min(520px, calc(100vw - 28px)); max-height: calc(100dvh - 36px); border: 1px solid var(--reader-border); border-radius: 12px; padding: 24px; background: var(--reader-paper); color: var(--reader-text); box-shadow: 0 20px 70px #0004; }
.reader-dialog::backdrop { background: #10182088; }
.reader-dialog-heading { display: flex; justify-content: space-between; align-items: center; gap: 20px; margin-bottom: 22px; }
.reader-dialog-heading h2 { margin: 0; font: 600 24px var(--serif); }
@media (max-width: 400px) { .reader-dialog { padding: 18px; } }
</style>
