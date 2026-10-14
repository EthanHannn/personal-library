<script setup lang="ts">
import type { ProfileData } from '../../api'
import { useRequest } from '../../composables/useRequest'
import { useReaderPreferences } from '../../composables/useReaderPreferences'
import RequestStatus from '../RequestStatus.vue'
import ContinueReading from '../home/ContinueReading.vue'
import ReaderSettings from '../reader/ReaderSettings.vue'
import PersonalVotes from './PersonalVotes.vue'

const { data, pending, error, reload } = useRequest<ProfileData>('/api/v1/profile')
const { preferences, warning, update, style } = useReaderPreferences()
</script>

<template>
  <header class="profile-heading"><p class="eyebrow">我的阅读空间</p><h1>个人中心</h1><p v-if="data">{{ data.nickname }}，从这里接着读，也回看自己的选择。</p></header>
  <RequestStatus :pending="pending" :error="error" @retry="reload" />
  <template v-if="data">
    <nav class="profile-stats" aria-label="我的阅读数据"><RouterLink to="/bookshelf"><strong>{{ data.counts.shelf.toLocaleString() }}</strong> 本收藏</RouterLink><RouterLink to="/bookshelf?view=reading"><strong>{{ data.counts.reading.toLocaleString() }}</strong> 本读过</RouterLink><a href="#my-votes"><strong>{{ data.counts.votes.toLocaleString() }}</strong> 本已评价</a></nav>
    <ContinueReading :entries="data.reading" />
    <PersonalVotes />
    <section class="profile-preferences" aria-labelledby="preferences-title"><h2 id="preferences-title">阅读设置</h2><p class="muted">保存在当前浏览器，下次打开阅读器时生效。</p><div :style="style"><ReaderSettings :preferences="preferences" :warning="warning" @update="update" /></div></section>
    <p class="account-settings"><RouterLink to="/manage">书库管理 →</RouterLink><a :href="data.accountUrl">门户账户设置 →</a><span class="muted">登录与安全设置由门户管理。</span></p>
  </template>
</template>

<style scoped>
.profile-heading { padding: 28px 0 16px; }.profile-heading p { line-height: 1.8; overflow-wrap: anywhere; }.profile-stats { display: flex; flex-wrap: wrap; gap: 16px; }.profile-stats a { flex: 1; min-width: 130px; padding: 24px; background: var(--paper); border: 1px solid var(--line); border-radius: 12px; text-decoration: none; font-size: 13px; }.profile-stats strong { font: 700 32px var(--serif); display: block; margin-bottom: 10px; }.profile-preferences { margin-top: 44px; padding: 26px; border: 1px solid var(--line); background: var(--paper); border-radius: 16px; }.profile-preferences h2 { font: 700 25px var(--serif); }.profile-preferences > p { font-size: 13px; margin-bottom: 28px; }.profile-preferences > div { max-width: 640px; }.account-settings { display: flex; flex-wrap: wrap; gap: 16px; margin-top: 28px; font-size: 14px; line-height: 1.8; }@media(max-width:450px) { .profile-stats { gap: 10px; }.profile-stats a { min-width: 0; padding: 16px 10px; }.profile-preferences { padding: 18px; } }
</style>
