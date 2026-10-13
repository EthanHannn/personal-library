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

export interface ContentBlock {
  book: Pick<Book, 'id' | 'title' | 'author' | 'chapterCount'>
  chapter: { idx: number; title: string }
  text: string
  contentVersion: string
  nextCursor: string | null
}

export class APIError extends Error {
  constructor(message: string, readonly status: number) {
    super(message)
  }
}

export async function getJSON<T>(path: string, signal: AbortSignal): Promise<T> {
  const response = await fetch(path, { signal, credentials: 'same-origin', cache: 'no-store', redirect: 'error' })
  if (!response.headers.get('content-type')?.startsWith('application/json')) {
    throw new APIError('未能读取书屋数据，请刷新页面后重试。', response.status)
  }
  const body = await response.json()
  if (!response.ok) {
    throw new APIError(body.error?.message ?? '暂时无法读取，请重试。', response.status)
  }
  return body as T
}
