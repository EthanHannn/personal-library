<script setup lang="ts">
import { ref, shallowRef } from 'vue'
import type { Job, ManagedBook, ManagedPage } from '../../manage'
import { useRequest } from '../../composables/useRequest'
import { useMutation } from '../../composables/useMutation'
import RequestStatus from '../RequestStatus.vue'
import BookEditor from './BookEditor.vue'
import ImportForm from './ImportForm.vue'
const emit = defineEmits<{ created: [job: Job] }>()
const search = ref(''), query = ref(''), filter = ref('all'), after = ref(0)
const selected = shallowRef<ManagedBook | null>(null)
const editing = ref(false), importing = ref(false)
const { data, pending, error, reload } = useRequest<ManagedPage>(() => `/api/v1/manage/books?search=${encodeURIComponent(query.value)}&filter=${filter.value}&after=${after.value}`)
const action = useMutation()
function apply() { query.value = search.value; after.value = 0; selected.value = null; editing.value = false; importing.value = false; reload() }
function edit(book?: ManagedBook) { selected.value = book ?? null; editing.value = true; importing.value = false }
function saved() { editing.value = false; selected.value = null; reload() }
function attach(book: ManagedBook) { selected.value = book; importing.value = true; editing.value = false }
async function cover(book: ManagedBook) { const job = await action.run<Job>(`/api/v1/books/${book.id}/cover-jobs`, 'POST', {}); if (job) emit('created', job) }
</script>

<template>
  <RequestStatus :pending="pending" :error="error" @retry="reload" />
  <template v-if="data && action.error.value?.status !== 401">
    <form class="catalog-search manage-form" @submit.prevent="apply"><label>查找书名或作者<input v-model="search" maxlength="200" type="search"></label><label>书目筛选<select v-model="filter" @change="after = 0"><option value="all">全部书目</option><option value="pending">待审书目</option><option value="no-text">缺少正文</option><option value="no-cover">未登记真实封面</option></select></label><div class="manage-actions"><button class="button">查询</button><button type="button" class="button secondary" @click="edit()">手工补充书目</button></div></form>
    <BookEditor v-if="editing" :key="`${selected?.id ?? 0}:${selected?.updatedAt ?? 0}`" :book="selected ?? undefined" @saved="saved" @close="editing = false" />
    <ImportForm v-if="importing && selected" :key="selected.id" :book-id="selected.id" :initial-title="selected.title" :initial-author="selected.author" :metadata="selected" @created="emit('created', $event)" />
    <ul class="managed-books"><li v-for="book in data.books" :key="book.id"><h3><RouterLink v-if="book.status === 'APPROVED'" :to="`/book/${book.id}`">{{ book.title }}</RouterLink><span v-else>{{ book.title }}</span></h3><p class="muted">{{ book.author || '作者未详' }} · {{ book.status === 'APPROVED' ? '已收录' : '待审' }} · {{ book.hasContent ? '有正文' : '缺少正文' }}</p><div class="manage-actions"><button class="text-button" @click="edit(book)">编辑书目</button><button v-if="!book.hasContent" class="text-button" @click="attach(book)">补充 TXT</button><button class="text-button" :disabled="action.pending.value" @click="cover(book)">{{ book.hasCover ? '检查封面' : '补全封面' }}</button></div></li></ul>
    <p v-if="!data.books.length" class="muted">没有符合条件的书目。</p>
    <nav class="manage-actions" aria-label="管理书目翻页"><button v-if="after" class="button secondary" @click="after = 0">第一页</button><button v-if="data.next" class="button" @click="after = data.next">下一页 →</button></nav>
  </template>
  <p v-if="action.error.value" role="alert">{{ action.error.value.message }}</p>
</template>

<style scoped>
.managed-books { list-style: none; padding: 0; display: grid; grid-template-columns: repeat(2,minmax(0,1fr)); gap: 16px; margin: 28px 0; }.managed-books li { border: 1px solid var(--line); background: var(--paper); border-radius: 12px; padding: 20px; }.managed-books h3 { margin: 0; font: 700 22px/1.6 var(--serif); overflow-wrap: anywhere; }.managed-books p { font-size: 13px; overflow-wrap: anywhere; }.managed-books a { text-decoration: none; }@media(max-width:640px) { .managed-books { grid-template-columns: minmax(0,1fr); } }
</style>
