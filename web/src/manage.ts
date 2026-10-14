export interface Metadata { title: string; author: string; tag1: string; tag2: string; intro: string }
export interface ManagedBook extends Metadata { id: number; status: 'APPROVED' | 'PENDING'; hasContent: boolean; hasCover: boolean; updatedAt: number }
export interface ManagedPage { books: ManagedBook[]; next: number | null }
import type { SearchCandidate, SearchSourceResult, SourceCheck } from './sources'
export interface JobInput extends Partial<Metadata> { path?: string; encoding?: string; bookId?: number; url?: string; maxChapters?: number; sourceId?: string; sourceVersion?: number; sourceIds?: string[]; query?: string }
export interface Job {
  id: string
  kind: 'import' | 'collect' | 'cover' | 'scan' | 'search' | 'source-check'
  status: 'queued' | 'running' | 'review' | 'completed' | 'failed' | 'cancelled'
  input: JobInput
  result: Partial<Metadata> & {
    file?: { path: string; encoding: string; size: number }
    words?: number; chapterCount?: number; chapters?: { idx: number; title: string }[]
    warnings?: string[]; bookId?: number
    downloaded?: number; planned?: number
    source?: { name: string; label?: string; ruleVersion?: number; workStatus?: string; complete: boolean; total: number; downloaded: number; skippedPaid: number } | string
    searchCandidates?: SearchCandidate[]; searchSources?: SearchSourceResult[]; sourceCheck?: SourceCheck
    checked?: number; missing?: number; changed?: number; candidates?: string[]; issues?: { bookId: number; path: string; message: string }[]; truncated?: boolean
  }
  message: string; attempts: number; nextAt: number; createdAt: number; updatedAt: number
}
export const jobNames = { import: 'TXT 导入', collect: '网络采集', cover: '封面补全', scan: '书库检查', search: '多源搜书', 'source-check': '书源检查' }
export const jobStates = { queued: '等待处理', running: '正在处理', review: '等待确认', completed: '已完成', failed: '未完成', cancelled: '已取消' }
