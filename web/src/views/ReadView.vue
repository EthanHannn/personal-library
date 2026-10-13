<script setup lang="ts">
import { useRoute } from 'vue-router'
import ReaderSession from '../components/reader/ReaderSession.vue'
const route = useRoute()
</script>

<template>
  <ReaderSession :key="String(route.params.id)" :book-id="Number(route.params.id)" />
</template>
