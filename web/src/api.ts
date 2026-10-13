export interface Book {
  id: number
  title: string
  author: string
  tag1: string
  tag2: string
  wordCount: number
  chapterCount: number
  hasContent: boolean
  score: number
  intro?: string
}

export interface Identity {
  user: { id: string; nickname: string }
  logoutUrl: string
}

export interface CatalogBook extends Book { popularity: number; xiancaoCount: number }
export interface CatalogPage {
  books: CatalogBook[]
  pagination: { page: number; limit: number; total: number; totalPages: number }
  summary: { authors: number; readable: number }
}
export interface Category { name: string; count: number; subcategories: { name: string; count: number }[] }

export interface ProgressSummary { chapterIdx: number; percent: number; updatedAt: number }
export interface CollectionEntry {
  book: Book
  inShelf: boolean
  addedAt: number | null
  progress: ProgressSummary | null
}
export interface CollectionPage {
  entries: CollectionEntry[]
  nextCursor: string | null
  counts: { shelf: number; reading: number }
}
export interface BookState { book: Book; inShelf: boolean; progress: ProgressSummary | null }
export interface FeaturedBook extends Book { xiancaoCount: number }
export interface HomeData {
  stats: { books: number; authors: number; categories: number; readable: number }
  featured: FeaturedBook[]
  highRated: Book[]
  latest: Book[]
  reading: CollectionEntry[]
}

export interface ContentBlock {
  book: Pick<Book, 'id' | 'title' | 'author' | 'chapterCount'>
  chapter: { idx: number; title: string }
  text: string
  contentVersion: string
  nextCursor: string | null
  blockToken: string
  blockStart: number
  blockEnd: number
  chapterStart: number
  chapterEnd: number
}

export interface Position {
  chapterIdx: number
  percent: number
  contentVersion?: string
  anchor?: { block: string; paragraph: number; character: number }
}
export interface Progress extends Position { revision: number; updatedAt: number }
export interface Mutation extends Position {
  baseRevision: number
  deviceId: string
  clientSeq: number
  mutationId: string
}
export interface ReadingState {
  book: ContentBlock['book']
  content: ContentBlock | null
  progress: Progress | null
  inShelf: boolean
  previous: ContentBlock['chapter'] | null
  next: ContentBlock['chapter'] | null
  recoveryRequired: boolean
}

export class APIError extends Error {
  constructor(message: string, readonly status: number, readonly code = '', readonly progress: Progress | null = null) {
    super(message)
  }
}

export async function getJSON<T>(path: string, signal: AbortSignal): Promise<T> {
  return requestJSON<T>(path, { signal })
}

export async function requestJSON<T>(path: string, options: RequestInit = {}): Promise<T> {
  const response = await fetch(path, { ...options, credentials: 'same-origin', cache: 'no-store', redirect: 'error' })
  if (response.status === 401 || response.status === 403) window.dispatchEvent(new Event('library:auth-lost'))
  if (!response.headers.get('content-type')?.startsWith('application/json')) {
    throw new APIError('未能读取书屋数据，请刷新页面后重试。', response.status)
  }
  const body = await response.json()
  if (!response.ok) {
    throw new APIError(body.error?.message ?? '暂时无法读取，请重试。', response.status, body.error?.code, body.progress ?? null)
  }
  return body as T
}
