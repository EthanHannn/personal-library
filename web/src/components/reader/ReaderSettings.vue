<script setup lang="ts">
import { themes } from '../../reader/themes'
import type { Preferences } from '../../reader/themes'
const props = defineProps<{ preferences: Preferences; warning: string }>()
const emit = defineEmits<{ update: [value: Preferences] }>()
function update(part: Partial<Preferences>) { emit('update', { ...props.preferences, ...part }) }
</script>

<template>
  <fieldset class="theme-field"><legend>阅读底色</legend><div class="theme-grid"><button v-for="theme in themes" :key="theme.id" class="theme-swatch" :style="{ backgroundColor: theme.paper, color: theme.text, borderColor: theme.accent }" :aria-pressed="preferences.theme === theme.id" @click="update({ theme: theme.id })"><span>{{ theme.name }}</span><span aria-hidden="true">{{ preferences.theme === theme.id ? '✓' : '书' }}</span></button></div></fieldset>
  <label class="setting-label" for="reader-font">字号 <output>{{ preferences.fontSize }}</output></label><input id="reader-font" type="range" min="14" max="30" step="1" :value="preferences.fontSize" @input="update({ fontSize: Number(($event.target as HTMLInputElement).value) })">
  <label class="setting-check"><input type="checkbox" :checked="preferences.colorize" @change="update({ colorize: ($event.target as HTMLInputElement).checked })">彩色正文</label>
  <p class="reader-muted">为对白、注释、数字和英文区分颜色。</p><p v-if="warning" role="status">{{ warning }}</p>
</template>

<style scoped>
.theme-field { border: 0; padding: 0; margin: 0 0 28px; }.theme-field legend { margin-bottom: 12px; }.theme-grid { display: grid; grid-template-columns: repeat(3, 1fr); gap: 10px; }
.theme-swatch { display: flex; justify-content: space-between; min-height: 64px; align-items: center; padding: 12px; border: 1px solid; border-radius: 8px; font-family: var(--serif); }.theme-swatch[aria-pressed=true] { outline: 2px solid var(--reader-accent); outline-offset: 2px; }
.setting-label { display: flex; justify-content: space-between; }.setting-check { display: flex; align-items: center; gap: 12px; margin-top: 24px; }input[type=range] { width: 100%; margin-top: 12px; accent-color: var(--reader-accent); }
</style>
