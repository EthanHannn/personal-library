import type { ContentBlock, Position, ReadingState, VoteSummary, VoteType } from '../api'

export function voting(myVote: VoteType | null = null, bookId = 42): VoteSummary {
  const source = { score: 8.5, popularity: 2, totalScore: 17, counts: { XIANCAO: 1, LIANGCAO: 1, GANCAO: 0, KUCAO: 0, DUCAO: 0 } }
  const counts = { ...source.counts }
  if (myVote) counts[myVote]++
  return { bookId, myVote, source, current: { ...source, counts, popularity: myVote ? 3 : 2 } }
}

export function content(chapter = 1, text = '第一章\n这是正文😀。'): ContentBlock {
  return { book: { id: 42, title: '测试小说', author: '测试作者', chapterCount: 3 }, chapter: { idx: chapter, title: `第 ${chapter} 章` }, text, contentVersion: 'v'.repeat(64), blockToken: `block-${chapter}`, nextCursor: null, blockStart: 0, blockEnd: 40, chapterStart: 0, chapterEnd: 40 }
}
export function reading(chapter = 1): ReadingState {
  return { book: content().book, content: content(chapter), progress: null, inShelf: false, previous: chapter > 1 ? { idx: chapter - 1, title: '前章' } : null, next: chapter < 3 ? { idx: chapter + 1, title: '后章' } : null, recoveryRequired: false }
}
export function position(chapter = 1, percent = 0.5): Position {
  return { chapterIdx: chapter, percent, contentVersion: 'v'.repeat(64), anchor: { block: `block-${chapter}`, paragraph: 4, character: 2 } }
}
export function response(value: unknown, status = 200) { return new Response(JSON.stringify(value), { status, headers: { 'Content-Type': 'application/json' } }) }
export function deferred<T>() {
  let resolve!: (value: T) => void
  let reject!: (reason: unknown) => void
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
