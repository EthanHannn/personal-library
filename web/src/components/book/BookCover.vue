<script setup lang="ts">
defineProps<{ title: string }>()
</script>

<template>
  <div class="book-cover" aria-hidden="true"><span>{{ title.slice(0, 12) }}</span></div>
</template>

<style scoped>
.book-cover { position: relative; width: 100%; aspect-ratio: 5 / 7; border-left: 6px solid #1e332b; border-radius: 2px 6px 6px 2px; background: var(--green); color: var(--paper); font-family: var(--serif); overflow: hidden; }
.book-cover span { position: absolute; top: 12px; bottom: 12px; left: 50%; transform: translateX(-50%); width: 1.6em; writing-mode: vertical-rl; letter-spacing: .18em; font-size: 18px; line-height: 1.6; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; text-align: center; }
</style>
