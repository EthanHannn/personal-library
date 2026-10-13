<script setup lang="ts">
import CatalogBrowser from '../components/catalog/CatalogBrowser.vue'
import type { CatalogScope } from '../catalog/query'
defineProps<{ kind?: CatalogScope['kind']; name?: string }>()
</script>

<template>
  <CatalogBrowser :kind="kind" :name="name" />
</template>
