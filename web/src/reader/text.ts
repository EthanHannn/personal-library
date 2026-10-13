import type { ContentBlock, Position } from '../api'

export interface Token { text: string; kind: string }
export interface Paragraph {
  id: string
  text: string
  start: number
  block: ContentBlock
  tokens: Token[]
  blockLength: number
}

// In agreement with internal/reader/paragraph.go: offsets count Unicode scalar
// values, including original CR characters and line separators.
export function paragraphs(block: ContentBlock): Paragraph[] {
  const result: Paragraph[] = []
  const blockLength = Array.from(block.text).length
  let start = 0
  for (const line of block.text.split('\n')) {
    const chars = Array.from(line)
    for (let i = 0; i < chars.length; i += 1000) {
      const text = chars.slice(i, i + 1000).join('')
      if (text.trim()) result.push({ id: `${block.contentVersion}:${block.blockStart}:${start + i}`, text, start: start + i, block, tokens: tokenize(text), blockLength })
    }
    start += chars.length + 1
  }
  return result
}

export function tokenize(text: string): Token[] {
  const parts = text.split(/([“”‘’"]|[（(【\[〔《〈]|[）)】\]〕》〉]|\d+(?:[.,]\d+)*|[A-Za-z]+(?:['’-][A-Za-z]+)*|[※★☆◆◇●○◎＊*#＃~～]+|[，。！？；：、,.!?;:…—·]+)/g).filter(Boolean)
  let quote = 0
  let bracket = 0
  return parts.map(text => {
    let kind = 'text'
    if ('“‘'.includes(text)) { quote++; kind = 'punctuation' }
    else if ('”’'.includes(text)) { quote = Math.max(0, quote - 1); kind = 'punctuation' }
    else if (text === '"') { quote = 1 - Math.min(1, quote); kind = 'punctuation' }
    else if ('（(【[〔《〈'.includes(text)) { bracket++; kind = 'punctuation' }
    else if ('）)】]〕》〉'.includes(text)) { bracket = Math.max(0, bracket - 1); kind = 'punctuation' }
    else if (quote) kind = 'quote'
    else if (bracket) kind = 'bracket'
    else if (/^\d/.test(text)) kind = 'number'
    else if (/^[A-Za-z]/.test(text)) kind = 'english'
    else if (/^[※★☆◆◇●○◎＊*#＃~～]+$/.test(text)) kind = 'marker'
    else if (/^[，。！？；：、,.!?;:…—·]+$/.test(text)) kind = 'punctuation'
    return { text, kind }
  })
}

export function positionAt(p: Paragraph, character: number): Position {
  const b = p.block
  const fraction = (p.start + character) / Math.max(1, p.blockLength)
  return {
    chapterIdx: b.chapter.idx,
    percent: Math.min(1, Math.max(0, (b.blockStart - b.chapterStart + fraction * (b.blockEnd - b.blockStart)) / (b.chapterEnd - b.chapterStart))),
    contentVersion: b.contentVersion,
    anchor: { block: b.blockToken, paragraph: p.start, character },
  }
}

export function samePosition(a: Position | null, b: Position | null): boolean {
  return a?.chapterIdx === b?.chapterIdx && a?.percent === b?.percent && a?.contentVersion === b?.contentVersion && a?.anchor?.block === b?.anchor?.block && a?.anchor?.paragraph === b?.anchor?.paragraph && a?.anchor?.character === b?.anchor?.character
}

export function cleanPosition(p: Position): Position {
  return { chapterIdx: p.chapterIdx, percent: p.percent, ...(p.contentVersion ? { contentVersion: p.contentVersion } : {}), ...(p.anchor ? { anchor: { ...p.anchor } } : {}) }
}
