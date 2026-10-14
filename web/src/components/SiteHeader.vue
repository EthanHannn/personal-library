<script setup lang="ts">
import type { Identity } from '../api'
import { useRequest } from '../composables/useRequest'

const { data } = useRequest<Identity>('/api/v1/me')
function logout() { window.dispatchEvent(new Event('library:auth-lost')) }
</script>

<template>
  <header class="site-header">
    <div class="header-inner">
      <RouterLink class="wordmark" to="/" aria-label="知轩书房首页">知轩书房<span>个人书屋</span></RouterLink>
      <nav class="nav" aria-label="主导航">
        <RouterLink to="/library">书库</RouterLink>
        <RouterLink to="/bookshelf">书架</RouterLink>
        <RouterLink to="/profile">我的</RouterLink>
        <a v-if="data" :href="data.logoutUrl" @click="logout">退出</a>
      </nav>
    </div>
  </header>
</template>

<style scoped>
.site-header { border-bottom: 1px solid var(--line); background: var(--paper); }
.header-inner { max-width: 1180px; margin: auto; padding: 22px 28px; display: flex; justify-content: space-between; align-items: center; gap: 20px; }
.wordmark { font-family: var(--serif); font-size: 26px; font-weight: 700; text-decoration: none; white-space: nowrap; }
.wordmark span { margin-left: 14px; font-family: var(--sans); font-size: 12px; font-weight: 400; color: var(--muted); }
.nav { display: flex; align-items: center; gap: 24px; font-size: 14px; }
.nickname { color: var(--muted); max-width: 9em; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
@media (max-width: 600px) {
  .header-inner { padding: 18px; gap: 12px; }
  .wordmark { font-size: 22px; }
  .wordmark span, .nickname { display: none; }
  .nav { gap: 16px; }
}
@media (max-width: 360px) { .wordmark { font-size: 20px; }.nav { gap: 12px; font-size: 13px; }.header-inner { gap: 10px; } }
</style>
