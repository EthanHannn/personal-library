<script setup lang="ts">
import type { HomeData } from '../../api'
import { useRequest } from '../../composables/useRequest'
import RequestStatus from '../RequestStatus.vue'
import BookList from '../catalog/BookList.vue'
import FeaturedCarousel from './FeaturedCarousel.vue'
import ContinueReading from './ContinueReading.vue'
const { data, pending, error, reload } = useRequest<HomeData>('/api/v1/home')
</script>

<template>
  <RequestStatus :pending="pending" :error="error" @retry="reload" />
  <template v-if="data">
    <FeaturedCarousel :books="data.featured" :stats="data.stats" />
    <ContinueReading :entries="data.reading" />
    <section class="home-section" aria-labelledby="high-rated-title"><div class="section-heading"><div><h2 id="high-rated-title">高分精选</h2><p class="muted">已收录正文的高分作品。</p></div><RouterLink to="/library?sortBy=score">浏览高分作品 →</RouterLink></div><BookList v-if="data.highRated.length" :books="data.highRated" heading-tag="h3" /><p v-else class="home-empty muted">还没有高分且有正文的作品，可到书库浏览<RouterLink to="/library?minScore=0&onlyContent=0">全部馆藏</RouterLink>。</p></section>
    <section class="home-section" aria-labelledby="latest-title"><div class="section-heading"><h2 id="latest-title">最近入库</h2><RouterLink to="/library?sortBy=latest">浏览最近入库 →</RouterLink></div><BookList v-if="data.latest.length" :books="data.latest" heading-tag="h3" /><p v-else class="home-empty muted">新收录的高分正文会出现在这里。</p></section>
  </template>
</template>

<style scoped>
.home-section { margin-top: 52px; }.home-empty { border: 1px dashed var(--line); border-radius: 12px; padding: 24px; }
</style>
