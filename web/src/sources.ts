import type { Metadata } from './manage'

export interface FieldRule { selector: string; attr?: string; trimPrefix?: string }
export interface SourceConfig {
  name: string; group: string; kind: 'html' | 'json' | 'qidian'; baseUrl: string
  allowedHosts: string[]; charset: string; intervalMs: number
  search?: { url: string; form?: Record<string, string>; items: string; title: FieldRule; author: FieldRule; link: FieldRule; category?: FieldRule; latest?: FieldRule }
  book: { title: FieldRule; author: FieldRule; intro?: FieldRule; category?: FieldRule; toc?: FieldRule; status?: FieldRule }
  toc: { items: string; title: FieldRule; link: FieldRule; next?: FieldRule; reverse?: boolean }
  chapter: { title: FieldRule; content: FieldRule; next?: FieldRule; remove?: string[]; dropLines?: string[] }
}
export interface SourceCheck {
  sourceId: string; version: number; passed: boolean; stage: string; error?: string
  title: string; author: string; chapters: number; sampleTitle: string; sampleWords: number; searchCount?: number; checkedAt: number
}
export interface BookSource {
  id: string; version: number; enabled: boolean; config: SourceConfig
  checkedAt: number; checkVersion: number; check: Partial<SourceCheck>; cooldownUntil: number
}
export interface SourceTemplate { id: string; note: string; config: SourceConfig }
export interface SourcesPage { sources: BookSource[]; templates: SourceTemplate[] }
export interface SearchCandidate extends Metadata { sourceId: string; sourceName: string; sourceVersion: number; url: string; latest: string; needsReview: boolean }
export interface SearchSourceResult { id: string; name: string; count: number; error?: string }
export function sourceHealth(source: BookSource) {
  if (source.cooldownUntil > Date.now()) return `冷却至 ${new Date(source.cooldownUntil).toLocaleString('zh-CN')}`
  if (!source.checkedAt) return '尚未检查样本'
  if (source.checkVersion !== source.version) return '规则已更新，需重新检查'
  return source.check.passed ? '最近样本检查通过' : `检查未通过 · ${source.check.stage ?? ''} ${source.check.error ?? ''}`
}
