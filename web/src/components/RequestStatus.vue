<script setup lang="ts">
import type { APIError } from '../api'

defineProps<{ pending: boolean; error: APIError | null }>()
defineEmits<{ retry: [] }>()
const refresh = () => window.location.reload()
</script>

<template>
  <p v-if="pending" class="request-status muted" role="status">正在翻找书架…</p>
  <div v-else-if="error" class="request-status" role="alert">
    <p>{{ error.status === 401 ? '登录已过期，请重新登录后继续。' : error.message }}</p>
    <button v-if="error.status === 401" class="button" @click="refresh">重新登录</button>
    <button v-else class="button" @click="$emit('retry')">重试</button>
  </div>
</template>

<style scoped>
.request-status { padding: 40px 0; }
</style>
