<script setup lang="ts">
import { useShelfAction } from '../../composables/useShelfAction'

const props = defineProps<{ bookId: number; inShelf: boolean }>()
const emit = defineEmits<{ change: [inShelf: boolean] }>()
const { pending, desired, error, set } = useShelfAction(() => props.bookId)
async function change(added: boolean) {
  const value = await set(added)
  if (value !== null) emit('change', value)
}
function login() { window.location.reload() }
</script>

<template>
  <div class="shelf-action">
    <button class="button secondary shelf-button" :disabled="pending" :aria-pressed="inShelf" @click="change(!inShelf)">
      {{ pending ? (desired ? '正在加入…' : '正在移出…') : inShelf ? '移出书架' : '加入书架' }}
    </button>
    <div v-if="error" class="shelf-error" role="alert">
      <p>{{ error.status === 401 || error.status === 403 ? '登录已失效，请重新登录。' : error.message }}</p>
      <button v-if="error.status === 401 || error.status === 403" class="text-button" @click="login">重新登录</button>
      <button v-else class="text-button" @click="desired !== null && change(desired)">重试{{ desired ? '加入' : '移出' }}</button>
    </div>
  </div>
</template>

<style scoped>
.shelf-action { min-width: 0; }.shelf-button { white-space: nowrap; }.shelf-error { color: var(--ochre); max-width: 26em; font-size: 13px; line-height: 1.7; }.shelf-error p { margin-bottom: 0; }
</style>
