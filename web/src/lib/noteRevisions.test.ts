import { describe, expect, it } from 'vitest'
import { restorePayloadFor, revisionLabel, revisionsCountLabel, sortRevisionsNewestFirst, type Revision } from './noteRevisions'

function revision(overrides: Partial<Revision> = {}): Revision {
  return {
    id: '11111111-1111-4111-8111-111111111111',
    seq: 1,
    created_at: '2026-09-16T19:07:00Z',
    author_id: '22222222-2222-4222-8222-222222222222',
    title: 'Never copy Amber or Switchyard into tier 2',
    body: 'Amber already holds the durable archive...',
    ...overrides,
  }
}

describe('sortRevisionsNewestFirst', () => {
  it('reverses listNoteRevisions\'s oldest-first order for the HISTORY panel', () => {
    const revs = [revision({ seq: 1 }), revision({ seq: 2 }), revision({ seq: 3 })]
    expect(sortRevisionsNewestFirst(revs).map((r) => r.seq)).toEqual([3, 2, 1])
  })

  it('does not mutate the input array', () => {
    const revs = [revision({ seq: 1 }), revision({ seq: 2 })]
    sortRevisionsNewestFirst(revs)
    expect(revs.map((r) => r.seq)).toEqual([1, 2])
  })
})

describe('revisionLabel', () => {
  it('renders the mono REVISION n · <when> banner', () => {
    expect(revisionLabel(revision({ seq: 2, created_at: '2026-09-16T21:44:00Z' }))).toBe('REVISION 2 · 2026-09-16 21:44')
  })
})

describe('restorePayloadFor', () => {
  it('reproduces the old title and body -- an append, never a rewrite', () => {
    const rev = revision({ seq: 1, title: 'Old title', body: 'Old body' })
    expect(restorePayloadFor(rev)).toEqual({ title: 'Old title', body: 'Old body' })
  })
})

describe('revisionsCountLabel', () => {
  it('pluralises for anything but exactly one', () => {
    expect(revisionsCountLabel(1)).toBe('1 REVISION')
    expect(revisionsCountLabel(3)).toBe('3 REVISIONS')
    expect(revisionsCountLabel(0)).toBe('0 REVISIONS')
  })
})
