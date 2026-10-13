<script setup lang="ts">
import SiteHeader from './components/SiteHeader.vue'
import { computed } from 'vue'
import { useRoute } from 'vue-router'
const route = useRoute()
const reading = computed(() => route.path.startsWith('/read/'))
</script>

<template>
  <a class="skip-link" href="#main">跳到正文</a>
  <SiteHeader v-if="!reading" />
  <main id="main" :class="{ page: !reading }" tabindex="-1">
    <RouterView />
  </main>
  <footer v-if="!reading" class="site-footer">知轩书房 · 私人藏书空间</footer>
</template>
