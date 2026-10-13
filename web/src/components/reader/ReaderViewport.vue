<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, shallowRef, useTemplateRef, watch } from 'vue'
import type { Position } from '../../api'
import { positionAt } from '../../reader/text'
import type { Paragraph } from '../../reader/text'

const props = defineProps<{ paragraphs: Paragraph[]; title: string; fontSize: number; colorize: boolean; highlight: number; hasMore: boolean; loading: boolean }>()
const emit = defineEmits<{ position: [position: Position]; more: []; visible: [index: number] }>()
const scroller = useTemplateRef<HTMLDivElement>('scroller')
const restoring = shallowRef(false)
const headingParagraph = computed(() => {
  const first = props.paragraphs[0]
  return first && first.start === 0 && first.block.blockStart === first.block.chapterStart && first.text.replace(/\r$/, '') === props.title ? first : null
})
const bodyParagraphs = computed(() => props.paragraphs.map((paragraph, index) => ({ paragraph, index })).filter(({ index }) => index !== 0 || !headingParagraph.value))
let timer = 0
let nodes: HTMLElement[] = []
watch(() => props.paragraphs, async () => {
  await nextTick()
  nodes = Array.from(scroller.value?.querySelectorAll<HTMLElement>('[data-paragraph]') ?? [])
}, { flush: 'post' })

function visibleIndex() {
  const top = (scroller.value?.getBoundingClientRect().top ?? 0) + 24
  let low = 0; let high = nodes.length - 1
  while (low < high) { const middle = Math.floor((low + high) / 2); if (nodes[middle].getBoundingClientRect().bottom <= top) low = middle + 1; else high = middle }
  return low
}

function capture(): Position | null {
  const index = visibleIndex()
  const node = nodes[index]; const p = props.paragraphs[index]
  if (!node || !p || !scroller.value) return null
  const top = scroller.value.getBoundingClientRect().top + 24
  let character = 0
  if (node.getBoundingClientRect().top < top) {
    // Geometry also works underneath an open settings dialog, unlike hit
    // testing, which would see the dialog instead of the text beneath it.
    let low = 0; let high = Array.from(p.text).length
    while (low < high) {
      const middle = Math.floor((low + high) / 2)
      const rect = point(node, middle)?.getBoundingClientRect()
      if (rect && rect.bottom < top) low = middle + 1
      else high = middle
    }
    character = low
  }
  emit('visible', index)
  return positionAt(p, character)
}

function report() {
  clearTimeout(timer)
  timer = window.setTimeout(() => {
    if (restoring.value) return
    const position = capture()
    if (position) emit('position', position)
  }, 120)
}

function point(node: HTMLElement, character: number): Range | null {
  const walker = document.createTreeWalker(node, NodeFilter.SHOW_TEXT)
  let text: Node | null
  let remaining = character
  while ((text = walker.nextNode())) {
    const chars = Array.from(text.textContent ?? '')
    if (remaining <= chars.length) {
      const offset = chars.slice(0, remaining).join('').length
      const range = document.createRange()
      range.setStart(text, offset)
      range.setEnd(text, Math.min((text.textContent ?? '').length, offset + (chars[remaining]?.length ?? 0)))
      return range
    }
    remaining -= chars.length
  }
  return null
}

async function restore(position: Position | null): Promise<boolean> {
  restoring.value = true; clearTimeout(timer)
  await nextTick()
  nodes = Array.from(scroller.value?.querySelectorAll<HTMLElement>('[data-paragraph]') ?? [])
  let index = 0; let character = 0
  if (position?.anchor) {
    index = props.paragraphs.findIndex(p => p.block.blockToken === position.anchor!.block && p.start === position.anchor!.paragraph)
    character = position.anchor.character
    if (index < 0) { restoring.value = false; return false }
  } else if (position && props.paragraphs.length) {
    const first = props.paragraphs[0]
    const b = first.block
    const target = Math.max(0, (position.percent * (b.chapterEnd - b.chapterStart) + b.chapterStart - b.blockStart) / (b.blockEnd - b.blockStart) * first.blockLength)
    for (let i = 0; i < props.paragraphs.length; i++) { if (props.paragraphs[i].start <= target) index = i }
    character = Math.min(Array.from(props.paragraphs[index].text).length, Math.max(0, Math.floor(target) - props.paragraphs[index].start))
  }
  const container = scroller.value
  if (container && nodes[index]) {
    if (!position) container.scrollTop = 0
    else {
      const rect = point(nodes[index], character)?.getBoundingClientRect() ?? nodes[index].getBoundingClientRect()
      container.scrollTop += rect.top - container.getBoundingClientRect().top - 24
    }
  }
  await new Promise<void>(resolve => requestAnimationFrame(() => resolve()))
  restoring.value = false
  return true
}

function follow(index: number) {
  const node = nodes[index]
  if (!node || !scroller.value) return
  scroller.value.scrollTop += node.getBoundingClientRect().top - scroller.value.getBoundingClientRect().top - 24
  report()
}

defineExpose({ capture, restore, follow })
onBeforeUnmount(() => clearTimeout(timer))
</script>

<template>
  <div ref="scroller" class="reader-scroll" role="region" tabindex="0" aria-label="小说正文" @scroll.passive="report">
    <article class="reader-paper" :class="{ colorized: colorize }" :style="{ fontSize: `${fontSize}px` }">
      <header class="reader-chapter-heading"><p class="chapter-kicker">知轩书房</p><h1 class="reader-chapter-title" :data-paragraph="headingParagraph?.id" :class="{ speaking: !!headingParagraph && highlight === 0 }">{{ title }}</h1></header>
      <p v-for="{ paragraph, index } in bodyParagraphs" :key="paragraph.id" :data-paragraph="paragraph.id" class="reader-paragraph" :class="{ speaking: highlight === index }">
        <span v-for="(token, tokenIndex) in paragraph.tokens" :key="tokenIndex" :class="`token-${token.kind}`">{{ token.text }}</span>
      </p>
      <div class="reader-block-end">
        <button v-if="hasMore" class="reader-button" :disabled="loading" @click="emit('more')">{{ loading ? '正在加载…' : '继续本章' }}</button>
        <p v-else class="reader-muted">本章结束</p>
      </div>
    </article>
  </div>
</template>

<style scoped>
.reader-scroll { flex: 1; min-height: 0; overflow: auto; overscroll-behavior: contain; scrollbar-color: var(--reader-border) var(--reader-shell); }
.reader-paper { max-width: 820px; min-height: 100%; margin: auto; padding: 48px 54px 80px; background: var(--reader-paper); font-family: var(--serif); line-height: 2; }
.reader-chapter-heading { border-bottom: 1px solid var(--reader-border); padding-bottom: 20px; margin-bottom: 32px; }
.chapter-kicker { color: var(--reader-muted); font: 12px var(--sans); letter-spacing: .2em; }
.reader-chapter-title { color: var(--reader-title); font-size: 1.5em; }
.reader-paragraph { margin: 0 0 1em; white-space: pre-wrap; overflow-wrap: anywhere; scroll-margin-top: 24px; }
.reader-paragraph.speaking, .reader-chapter-title.speaking { background: var(--reader-soft); outline: 2px solid var(--reader-border); border-radius: 3px; }
.reader-paper ::selection { color: var(--reader-paper); background: var(--reader-accent); }
.reader-block-end { text-align: center; margin-top: 40px; font: 14px var(--sans); }
.colorized .token-quote { color: var(--reader-quote); }.colorized .token-bracket { color: var(--reader-bracket); }.colorized .token-punctuation { color: var(--reader-punctuation); }.colorized .token-marker { color: var(--reader-marker); }.colorized .token-number { color: var(--reader-number); }.colorized .token-english { color: var(--reader-english); }
@media (max-width: 600px) { .reader-paper { padding: 30px 22px 60px; } }
</style>
