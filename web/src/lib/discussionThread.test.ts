import { describe, expect, it } from 'vitest'
import {
  activeParticipantCount,
  avatarTreatmentFor,
  canReply,
  firstUnreadSeq,
  hasReadMarker,
  initialsFor,
  resolvedFooterLabel,
  threadCountsLabel,
  type Participant,
  type Turn,
} from './discussionThread'

function turn(overrides: Partial<Turn> = {}): Turn {
  return {
    id: '11111111-1111-4111-8111-111111111111',
    seq: 1,
    author_id: '22222222-2222-4222-8222-222222222222',
    author_kind: 'person',
    body: 'hello',
    html: '<p>hello</p>',
    references: [],
    created_at: '2026-09-16T19:07:00Z',
    ...overrides,
  }
}

function participant(overrides: Partial<Participant> = {}): Participant {
  return {
    user_id: '33333333-3333-4333-8333-333333333333',
    kind: 'person',
    display_name: 'You',
    added_at: '2026-09-16T19:07:00Z',
    added_by: '33333333-3333-4333-8333-333333333333',
    ...overrides,
  }
}

describe('avatarTreatmentFor', () => {
  // CH092 freezes author_kind on the turn; the ticket forbids steel
  // (--ch-generated) here specifically because that colour means
  // regenerated tier-1 content and a turn is authored tier 2 — so the
  // distinction has to survive on SHAPE and the AGENT tag alone, not colour.
  it('gives a person turn a circle with no agent tag', () => {
    expect(avatarTreatmentFor('person')).toEqual({ shape: 'circle', agentTag: false })
  })

  it('gives an agent turn a square with the agent tag', () => {
    expect(avatarTreatmentFor('agent')).toEqual({ shape: 'square', agentTag: true })
  })
})

describe('initialsFor', () => {
  it('takes the first two letters of a single-word name, matching the board\'s SC for Scribe', () => {
    expect(initialsFor('Scribe')).toBe('SC')
  })

  it('takes one letter each from the first two words of a multi-word name', () => {
    expect(initialsFor('Jane Doe')).toBe('JD')
  })

  it('uppercases regardless of input case', () => {
    expect(initialsFor('alice')).toBe('AL')
  })

  it('falls back to a placeholder for an empty name rather than throwing', () => {
    expect(initialsFor('   ')).toBe('?')
  })
})

describe('threadCountsLabel / activeParticipantCount', () => {
  it('matches the board exactly: 3 turns (opener included) and 2 current participants reads "3 REPLIES · 2 PARTICIPANTS"', () => {
    const turns = [turn({ seq: 1 }), turn({ seq: 2 }), turn({ seq: 3 })]
    const participants = [participant(), participant({ user_id: 'a', display_name: 'Scribe', kind: 'agent' })]
    expect(threadCountsLabel(turns, participants)).toBe('3 REPLIES · 2 PARTICIPANTS')
  })

  it('does not count a removed participant towards the current total', () => {
    const participants = [
      participant(),
      participant({ user_id: 'removed-1', removed_at: '2026-09-16T20:00:00Z', removed_by: 'x' }),
    ]
    expect(activeParticipantCount(participants)).toBe(1)
  })
})

describe('resolvedFooterLabel', () => {
  it('names the note when the thread resolved into one', () => {
    expect(
      resolvedFooterLabel({ resolved: { at: '2026-09-16T21:00:00Z', by: 'p', note: 'CHR-0311' } }),
    ).toBe('RESOLVED → CHR-0311')
  })

  it('reads plainly "RESOLVED" when it concluded with no note (CHRN-43 ruling 6: a deliberate, completable-later choice)', () => {
    expect(resolvedFooterLabel({ resolved: { at: '2026-09-16T21:00:00Z', by: 'p' } })).toBe('RESOLVED')
  })

  it('is null for a thread that has not resolved', () => {
    expect(resolvedFooterLabel({})).toBeNull()
  })
})

describe('canReply', () => {
  it('CH093: a resolved thread takes no more turns', () => {
    expect(canReply({ resolved: { at: '2026-09-16T21:00:00Z', by: 'p' } })).toBe(false)
    expect(canReply({})).toBe(true)
  })
})

describe('firstUnreadSeq', () => {
  it('inverts the store\'s max(seq) - last_read_seq to find where the NEW rule goes', () => {
    const turns = [1, 2, 3, 4, 5].map((seq) => turn({ seq }))
    expect(firstUnreadSeq(turns, 2)).toBe(4)
  })

  it('is null when nothing is unread', () => {
    const turns = [turn({ seq: 1 }), turn({ seq: 2 })]
    expect(firstUnreadSeq(turns, 0)).toBeNull()
  })

  it('is null when the caller has no marker at all (not a participant, or an agent)', () => {
    const turns = [turn({ seq: 1 })]
    expect(firstUnreadSeq(turns, undefined)).toBeNull()
  })

  it('is null for an empty thread', () => {
    expect(firstUnreadSeq([], 3)).toBeNull()
  })

  it('clamps defensively rather than pointing at a turn that does not exist', () => {
    const turns = [turn({ seq: 4 }), turn({ seq: 5 })]
    expect(firstUnreadSeq(turns, 99)).toBe(4)
  })
})

describe('hasReadMarker', () => {
  it('is true exactly when the server sent an unread count', () => {
    expect(hasReadMarker({ unread: 0 })).toBe(true)
    expect(hasReadMarker({ unread: 3 })).toBe(true)
  })

  it('is false when unread is absent (not a participant, or an agent — both refuse markRead)', () => {
    expect(hasReadMarker({})).toBe(false)
  })
})
