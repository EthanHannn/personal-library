<script setup lang="ts">
import { ref } from 'vue'
import type { ManagedBook } from '../../manage'
import { useMutation } from '../../composables/useMutation'
import RequestStatus from '../RequestStatus.vue'
const props = defineProps<{ book?: ManagedBook }>()
const emit = defineEmits<{ saved: []; close: [] }>()
const form = ref({ title: props.book?.title ?? '', author: props.book?.author ?? '', tag1: props.book?.tag1 ?? '', tag2: props.book?.tag2 ?? '', intro: props.book?.intro ?? '', status: props.book?.status ?? 'PENDING', expectedVersion: props.book?.updatedAt ?? 0 })
const { pending, error, run } = useMutation()
async function submit() { const result = await run(`/api/v1/manage/books${props.book ? `/${props.book.id}` : ''}`, props.book ? 'PUT' : 'POST', form.value); if (result) emit('saved') }
</script>

<template>
  <form v-if="error?.status !== 401" class="manage-form editor" @submit.prevent="submit">
    <h2>{{ book ? '编辑书目' : '手工补充书目' }}</h2>
    <div class="form-pair"><label>书名<input v-model="form.title" required maxlength="200"></label><label>作者<input v-model="form.author" maxlength="200"></label></div>
    <div class="form-pair"><label>主分类<input v-model="form.tag1" maxlength="100"></label><label>副分类<input v-model="form.tag2" maxlength="100"></label></div>
    <label>简介<textarea v-model="form.intro" rows="4" maxlength="4000" /></label>
    <label>书目状态<select v-model="form.status"><option value="PENDING">待审，仅在管理中显示</option><option value="APPROVED">已收录，在书库中显示</option></select></label>
    <div class="manage-actions"><button class="button" :disabled="pending">保存书目</button><button type="button" class="button secondary" :disabled="pending" @click="emit('close')">收起</button></div>
  </form>
  <RequestStatus :pending="false" :error="error" @retry="submit" />
</template>
