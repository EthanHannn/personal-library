import { describe, expect, it } from 'vitest'
import { acknowledge, claim, OwnershipError, prepareMutation, saveDraft } from './localProgress'
import { position } from '../test/fixtures'

describe('durable progress queue', () => {
  it('retains a newer position when an older request is acknowledged, then advances the sequence', async () => {
    const book = 101
    await claim(book, 'tab-a'); await saveDraft(book, 'tab-a', position())
    const first = (await prepareMutation(book, 'tab-a')).pending!
    await saveDraft(book, 'tab-a', position(2))
    expect((await prepareMutation(book, 'tab-a')).pending).toEqual(first)
    const result = await acknowledge(book, first, { ...first, revision: 1, updatedAt: 123 })
    expect(result.draft).toEqual(position(2))
    const second = (await prepareMutation(book, 'tab-a')).pending!
    expect(second.clientSeq).toBe(first.clientSeq + 1)
    expect(second.baseRevision).toBe(1)
    expect(second.mutationId).not.toBe(first.mutationId)
    await acknowledge(book, first, { ...first, revision: 1, updatedAt: 123 })
    expect((await prepareMutation(book, 'tab-a')).pending).toEqual(second)
  })

  it('recovers the exact mutation after reload and rejects writes from a superseded tab', async () => {
    const book = 102
    await claim(book, 'old-tab'); await saveDraft(book, 'old-tab', position(3))
    const pending = (await prepareMutation(book, 'old-tab')).pending!
    const resumed = await claim(book, 'new-tab')
    expect(resumed.pending).toEqual(pending)
    await expect(saveDraft(book, 'old-tab', position(1))).rejects.toBeInstanceOf(OwnershipError)
    expect((await prepareMutation(book, 'new-tab')).pending).toEqual(pending)
  })

  it('copies legacy pending progress once and keeps the legacy key for rollback', async () => {
    localStorage.setItem('zx_reading_103', JSON.stringify({ chapterIdx: 7, percent: 0.8, pending: true }))
    const first = await claim(103, 'tab')
    expect(first.legacy).toEqual({ chapterIdx: 7, percent: 0.8 })
    expect(first.legacyPending).toBe(true)
    localStorage.setItem('zx_reading_103', JSON.stringify({ chapterIdx: 1, percent: 0 }))
    expect((await claim(103, 'tab')).legacy).toEqual(first.legacy)
    expect(localStorage.getItem('zx_reading_103')).not.toBeNull()
  })
})
