import { expect, it } from 'vitest'
import { content } from '../test/fixtures'
import { paragraphs, tokenize } from './text'
import { speechChunks } from '../composables/useReaderTts'
import { loadPreferences } from '../composables/useReaderPreferences'

it('keeps Unicode scalar anchors stable across CRLF, blank lines and very long lines', () => {
  const p = paragraphs(content(1, '甲😀\r\n\n' + '汉'.repeat(1001) + '\n末行'))
  expect(p.map(v => v.start)).toEqual([0, 5, 1005, 1007])
  expect(p.map(v => v.text)).toEqual(['甲😀\r', '汉'.repeat(1000), '汉', '末行'])
})
it('never introduces HTML and preserves exact text during coloring', () => {
  const text = '<img src=x onerror=alert(1)>“对白” （注） 2026 abc 😀'
  expect(tokenize(text).map(v => v.text).join('')).toBe(text)
  expect(tokenize(text).some(v => v.kind === 'quote')).toBe(true)
})
it('splits speech without breaking supplementary characters', () => {
  const text = '𠀀😀汉'.repeat(300)
  const chunks = speechChunks(text)
  expect(chunks.join('')).toBe(text)
  expect(chunks.every(c => Array.from(c).length <= 180)).toBe(true)
})
it('migrates and validates preferences once while retaining old keys', () => {
  localStorage.setItem('zx_reader_theme', 'ocean'); localStorage.setItem('zx_reader_fontsize', '26')
  localStorage.setItem('zx_reader_tts_rate', '1.4'); localStorage.setItem('zx_reader_colorize', 'false')
  const prefs = loadPreferences()
  expect(prefs).toMatchObject({ theme: 'ocean', fontSize: 26, rate: 1.4, colorize: false })
  localStorage.setItem('zx_reader_theme', 'dark')
  expect(loadPreferences()).toEqual(prefs)
  expect(localStorage.getItem('zx_reader_fontsize')).toBe('26')
})
