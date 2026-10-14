<script setup lang="ts">
import { ref } from 'vue'
import type { Job, Metadata } from '../../manage'
import { useMutation } from '../../composables/useMutation'
import RequestStatus from '../RequestStatus.vue'

const props = defineProps<{ bookId?: number; initialTitle?: string; initialAuthor?: string; initialPath?: string; metadata?: Metadata }>()
const emit = defineEmits<{ created: [job: Job] }>()
const form = ref({ title: props.initialTitle ?? '', author: props.initialAuthor ?? '', tag1: '', tag2: '', intro: '', ...props.metadata, path: props.initialPath ?? '', encoding: 'utf-8' })
const { pending, error, run } = useMutation()
async function submit() { const j = await run<Job>('/api/v1/manage/jobs', 'POST', { kind: 'import', input: { ...form.value, bookId: props.bookId ?? 0 } }); if (j) emit('created', j) }
</script>

<template>
  <form v-if="error?.status !== 401" class="manage-form" @submit.prevent="submit">
    <h2>{{ bookId ? `补充正文 · ${initialTitle}` : '导入本地 TXT' }}</h2>
    <p class="muted">将 TXT 放在小说目录，填写相对路径。先检查编码、章节和重复项，再确认入库。浏览器采集工具生成的 TXT 也可从这里导入。</p>
    <label>TXT 相对路径<input v-model="form.path" required maxlength="1000" placeholder="例如：新书/作品.txt"></label>
    <label>文件编码<select v-model="form.encoding"><option value="utf-8">UTF-8</option><option value="gb18030">GB18030 / GBK</option></select></label>
    <div class="form-pair"><label>书名<input v-model="form.title" required maxlength="200"></label><label>作者<input v-model="form.author" maxlength="200"></label></div>
    <div class="form-pair"><label>主分类<input v-model="form.tag1" maxlength="100"></label><label>副分类<input v-model="form.tag2" maxlength="100"></label></div>
    <label>简介<textarea v-model="form.intro" rows="4" maxlength="4000" /></label>
    <button class="button" :disabled="pending">{{ pending ? '正在创建…' : '生成导入预览' }}</button>
  </form>
  <RequestStatus :pending="false" :error="error" @retry="submit" />
</template>
