<script setup lang="ts">
import SiteHeader from './components/SiteHeader.vue'
</script>

<template>
  <a class="skip-link" href="#main">跳到正文</a>
  <SiteHeader />
  <main id="main" class="page" tabindex="-1">
    <RouterView />
  </main>
  <footer class="site-footer">知轩书房 · 私人藏书空间</footer>
</template>
