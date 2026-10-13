<script setup lang="ts">
import type { Preferences } from '../../reader/themes'
defineProps<{ supported: boolean; status: string; voices: SpeechSynthesisVoice[]; preferences: Preferences; index: number; total: number; error: string }>()
const emit = defineEmits<{ toggle: []; stop: []; previous: []; next: []; update: [value: Partial<Preferences>] }>()
</script>

<template>
  <p v-if="!supported" role="status">此浏览器不支持语音朗读，可以继续阅读正文。</p>
  <template v-else>
    <p v-if="voices.length === 0" role="status">此设备尚未提供可用音色，请检查系统语音设置。</p>
    <label class="tts-label" for="tts-voice">音色<select id="tts-voice" :value="preferences.voice" @change="emit('update', { voice: ($event.target as HTMLSelectElement).value })"><option value="">自动选择中文音色</option><option v-for="voice in voices" :key="voice.voiceURI" :value="voice.voiceURI">{{ voice.name }} · {{ voice.lang }}</option></select></label>
    <label class="tts-label" for="tts-rate">语速 · {{ preferences.rate.toFixed(1) }}×<input id="tts-rate" type="range" min="0.6" max="2" step="0.1" :value="preferences.rate" @input="emit('update', { rate: Number(($event.target as HTMLInputElement).value) })"></label>
    <div class="reader-row tts-actions"><button class="reader-button" :disabled="index <= 0 || !voices.length" @click="emit('previous')">上一段</button><button class="reader-button primary" :disabled="!total || !voices.length" @click="emit('toggle')">{{ status === 'playing' ? '暂停' : status === 'paused' ? '继续朗读' : '开始朗读' }}</button><button class="reader-button" :disabled="index >= total - 1 || !voices.length" @click="emit('next')">下一段</button><button class="reader-button" :disabled="status === 'idle'" @click="emit('stop')">停止</button></div>
    <p class="reader-muted">第 {{ Math.min(index + 1, total) }} / {{ total }} 段 · 隐藏页面时暂停朗读。</p>
    <p v-if="error" role="alert">{{ error }}</p>
  </template>
</template>

<style scoped>
.tts-label { display: grid; gap: 10px; margin-bottom: 24px; }.tts-label select { width: 100%; min-width: 0; }.tts-actions { flex-wrap: wrap; }.tts-label input { accent-color: var(--reader-accent); }
</style>
