<script setup lang="ts">
import { ref, shallowRef, toRaw } from 'vue'
import type { BookSource, SourceConfig, SourceTemplate } from '../../sources'
import { useMutation } from '../../composables/useMutation'
import RequestStatus from '../RequestStatus.vue'

const props = defineProps<{ source?: BookSource; templates: SourceTemplate[] }>()
const emit = defineEmits<{ saved: [source: BookSource]; cancel: [] }>()
const templateId = shallowRef(props.templates[0]?.id ?? '')
const config = ref<SourceConfig>(structuredClone(toRaw(props.source?.config ?? props.templates[0]!.config)))
const enabled = shallowRef(props.source?.enabled ?? true)
const hosts = shallowRef(config.value.allowedHosts.join(', '))
const rules = shallowRef('')
const pasted = shallowRef('')
const localError = shallowRef('')
const { pending, error, run } = useMutation()
function setConfig(value: SourceConfig) {
  config.value = structuredClone(toRaw(value))
  hosts.value = value.allowedHosts.join(', ')
  rules.value = JSON.stringify({ search: value.search, book: value.book, toc: value.toc, chapter: value.chapter }, null, 2)
  localError.value = ''
}
setConfig(config.value)
function applyTemplate() { const value = props.templates.find(t => t.id === templateId.value); if (value) setConfig(value.config) }
function importConfig() {
  try {
    const value = JSON.parse(pasted.value)
    if (!value || !['html', 'json'].includes(value.kind) || !Array.isArray(value.allowedHosts) || !value.book || !value.toc || !value.chapter) throw new Error('请粘贴书屋格式的完整书源 JSON；阅读/Legado 规则不能直接导入。')
    setConfig(value)
  } catch (cause) { localError.value = cause instanceof Error ? cause.message : 'JSON 格式无效' }
}
async function save() {
  localError.value = ''
  let parsed: Pick<SourceConfig, 'search' | 'book' | 'toc' | 'chapter'>
  try {
    parsed = JSON.parse(rules.value)
    if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed) || !parsed.book || !parsed.toc || !parsed.chapter || Object.keys(parsed).some(k => !['search', 'book', 'toc', 'chapter'].includes(k))) throw new Error('规则须包含 book、toc、chapter，可选 search；其他字段不受支持。')
  } catch (cause) { localError.value = cause instanceof Error ? cause.message : 'JSON 格式无效'; return }
  const value = { ...config.value, search: undefined, ...parsed, allowedHosts: hosts.value.split(/[,，\s]+/).filter(Boolean) }
  const saved = await run<BookSource>(props.source ? `/api/v1/manage/sources/${encodeURIComponent(props.source.id)}` : '/api/v1/manage/sources', props.source ? 'PUT' : 'POST', { config: value, enabled: enabled.value, expectedVersion: props.source?.version ?? 0 })
  if (saved) emit('saved', saved)
}
</script>

<template>
  <form v-if="error?.status !== 401" class="manage-form source-editor" @submit.prevent="save">
    <h2>{{ source ? `编辑书源 · ${source.config.name}` : '添加书源' }}</h2>
    <div v-if="!source" class="template-picker">
      <label>从模板开始<select v-model="templateId"><option v-for="item in templates" :key="item.id" :value="item.id">{{ item.config.name }}</option></select></label>
      <button type="button" class="button secondary" @click="applyTemplate">填入模板</button>
      <p class="muted">{{ templates.find(t => t.id === templateId)?.note }}</p>
    </div>
    <div class="form-pair"><label>书源名称<input v-model="config.name" required maxlength="100"></label><label>分组<input v-model="config.group" maxlength="100" placeholder="例如：男频、玄幻、都市"></label></div>
    <label>网站地址<input v-model="config.baseUrl" type="url" required maxlength="2000"></label>
    <label>允许访问的域名<input v-model="hosts" required placeholder="www.example.com, m.example.com"><span class="muted">多个域名用逗号分隔。详情、目录、正文及跳转使用的域名都需列出。</span></label>
    <div class="form-pair"><label>页面格式<select v-model="config.kind"><option value="html">网页 · CSS 选择器</option><option value="json">接口 · JSON Pointer</option></select></label><label>编码<select v-model="config.charset"><option value="utf-8">UTF-8</option><option value="gb18030">GB18030 / GBK</option><option value="auto">自动识别</option></select></label></div>
    <div class="form-pair"><label>请求间隔（毫秒）<input v-model.number="config.intervalMs" type="number" min="1000" max="60000" step="1000" required></label><label>状态<select v-model="enabled"><option :value="true">启用</option><option :value="false">停用</option></select></label></div>
    <label>解析规则 JSON<textarea v-model="rules" rows="18" spellcheck="false" required class="rule-input" /></label>
    <details class="rule-help"><summary>规则填写说明与完整配置导入</summary>
      <p>search 搜书，book 详情，toc 目录，chapter 正文。不支持站内搜索时删除 search。搜索地址或表单用 <code>{query}</code> 代替关键词，每个书源返回前 20 个候选。</p>
      <p>HTML 字段用 <code>selector</code> 指定 CSS 选择器，链接加 <code>attr: "href"</code>，meta 数据加 <code>attr: "content"</code>；当前目录项用 <code>selector: "."</code>。可用 trimPrefix 去掉“作者：”等前缀。</p>
      <p>JSON 字段使用 <code>/data/title</code> 形式的指针，items 指向数组；正文应为纯文本。目录和正文的 next 提取下一页链接，toc.reverse 可倒转目录顺序。chapter.remove 移除明确的 CSS 区域，dropLines 仅删除整行相同的文本。</p>
      <p>当前格式不执行 JS、XPath、浏览器渲染或完整 Legado 规则。保存后用作品链接检查详情、目录和首章，再开始采集。</p>
      <label>完整书源 JSON<textarea v-model="pasted" rows="5" spellcheck="false" placeholder="粘贴书屋 SourceConfig 格式的完整 JSON" /></label>
      <button type="button" class="button secondary" @click="importConfig">读取配置到表单</button>
    </details>
    <p v-if="localError" role="alert">{{ localError }}</p>
    <div class="manage-actions"><button class="button" :disabled="pending">{{ pending ? '正在保存…' : '保存书源' }}</button><button type="button" class="text-button" :disabled="pending" @click="emit('cancel')">取消编辑</button></div>
    <p v-if="source" class="muted">旧任务继续使用创建时的规则版本。修改后请重新检查，并新建采集任务使用新规则。</p>
  </form>
  <RequestStatus :pending="false" :error="error" @retry="save" />
</template>

<style scoped>
.rule-input { font-family: ui-monospace, monospace; font-size: 13px; line-height: 1.6; tab-size: 2; }.rule-help { margin: 20px 0; overflow-wrap: anywhere; }.rule-help summary { cursor: pointer; padding: 10px 0; }.rule-help code { font-size: 12px; }.template-picker { padding: 18px; margin: 20px 0; border: 1px solid var(--line); border-radius: 10px; }
</style>
