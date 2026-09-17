import { describe, expect, it } from 'vitest'
import { describeSearchHit, type SearchHit } from './searchResult'

function noteHit(overrides: Partial<SearchHit> = {}): SearchHit {
  return {
    kind: 'note',
    ref: 'CHR-0311',
    title: 'Never copy Amber or Switchyard into tier 2',
    snippet: 'Amber already holds the <b>durable</b> archive',
    rank: 0.9,
    created_at: '2026-09-15T12:55:00Z',
    ...overrides,
  }
}

function transcriptHit(overrides: Partial<SearchHit> = {}): SearchHit {
  return {
    kind: 'transcript',
    memo_id: '11111111-1111-4111-8111-111111111111',
    model: 'whisper.cpp/small.en',
    snippet: 'the queue drains faster than I can talk into it',
    rank: 0.7,
    created_at: '2026-09-15T18:22:00Z',
    ...overrides,
  }
}

describe('describeSearchHit', () => {
  it('labels a note hit with its ref and links to the note', () => {
    const source = describeSearchHit(noteHit())
    expect(source.kind).toBe('note')
    expect(source.label).toBe('NOTE · CHR-0311')
    expect(source.to).toBe('/notes/CHR-0311')
    expect(source.title).toBeUndefined()
  })

  it('labels a transcript hit with its transcribed-at time and does not link', () => {
    const source = describeSearchHit(transcriptHit())
    expect(source.kind).toBe('transcript')
    expect(source.label).toBe('TRANSCRIPT · 2026-09-15 18:22')
    expect(source.to).toBeNull()
    expect(source.title).toMatch(/CHRN-107/)
  })

  it('never mistakes one kind for the other, even with matching refs/ids absent', () => {
    const note = describeSearchHit(noteHit({ ref: 'CHR-0007' }))
    const transcript = describeSearchHit(transcriptHit())
    expect(note.kind).not.toBe(transcript.kind)
    expect(note.to).not.toBeNull()
    expect(transcript.to).toBeNull()
  })
})
