import { describe, expect, it } from 'vitest'
import {
  audioControlFor,
  formatDurationMs,
  leadProvenance,
  provenanceBlock,
  provenanceHeaderLine,
  retentionLabel,
  transcriptStateFor,
  type MemoProvenance,
} from './provenanceBlock'

// CHRN-107 criterion 13: the `1c` provenance block's view model, built from
// `schema.d.ts` alone. No date is computed from `captured_at`, no field is
// fetched from a second place, and the pruned state is rendered from the
// PROVENANCE ENTRY rather than from the audio operation's refusal — which
// carries only a `code` and a `message` and which an `<audio src>` element
// never sees at all.

function entry(overrides: Partial<MemoProvenance> = {}): MemoProvenance {
  return {
    revision_seq: 1,
    revision_id: '11111111-1111-4111-8111-111111111111',
    memo_id: '22222222-2222-4222-8222-222222222222',
    captured_at: '2026-08-21T12:55:00Z',
    duration_ms: 104000,
    duration_source: 'transcript',
    audio_readable: true,
    retention_status: 'scheduled',
    prunes_at: '2026-09-20T12:55:00Z',
    audio_pruned_at: null,
    transcript: { present: true, readable: true, model: 'whisper.cpp/small.en', partial: false },
    ...overrides,
  }
}

describe('formatDurationMs', () => {
  it('renders the board\'s 1:44', () => {
    expect(formatDurationMs(104000)).toBe('1:44')
    expect(formatDurationMs(61000)).toBe('1:01')
    expect(formatDurationMs(9000)).toBe('0:09')
    expect(formatDurationMs(3723000)).toBe('1:02:03')
  })

  it('is null for a memo with neither a header duration nor a transcript', () => {
    // The contract states that as a null rather than a zero, so the segment
    // can be left out instead of rendering 0:00 for a recording nobody has
    // measured yet.
    expect(formatDurationMs(null)).toBeNull()
    expect(formatDurationMs(undefined)).toBeNull()
  })
})

describe('provenanceHeaderLine', () => {
  it('is board 1c\'s FROM MEMO 12:55 · 1:44 · ROUTED BY SCRIBE', () => {
    expect(provenanceHeaderLine(entry(), { verb: 'create' })).toBe('FROM MEMO 12:55 · 1:44 · ROUTED BY SCRIBE')
  })

  it('drops ROUTED BY SCRIBE for text somebody typed', () => {
    // `verb` is non-null exactly when a person confirmed a Scribe proposal.
    expect(provenanceHeaderLine(entry(), null)).toBe('FROM MEMO 12:55 · 1:44')
    expect(provenanceHeaderLine(entry(), {})).toBe('FROM MEMO 12:55 · 1:44')
  })

  it('drops the duration rather than faking one', () => {
    expect(provenanceHeaderLine(entry({ duration_ms: null, duration_source: undefined }), { verb: 'append' }))
      .toBe('FROM MEMO 12:55 · ROUTED BY SCRIBE')
  })
})

describe('retentionLabel', () => {
  it('names the date on the one status that carries one', () => {
    expect(retentionLabel(entry())).toBe('PRUNES 2026-09-20')
  })

  it('NEVER computes a date from captured_at', () => {
    // The whole of CHRN-22 §3's refusal: `awaiting_transcript` prunes WHEN
    // TRANSCRIBED, and `captured_at + 30 days` in its place is a label that
    // passes while nothing happens.
    const awaiting = retentionLabel(entry({ retention_status: 'awaiting_transcript', prunes_at: null }))
    expect(awaiting).toBe('PRUNES WHEN TRANSCRIBED')
    expect(awaiting).not.toMatch(/\d/)

    // And a `scheduled` entry whose date is missing says so rather than
    // deriving one — the guard, not the contract's expectation.
    expect(retentionLabel(entry({ prunes_at: null }))).not.toMatch(/\d{4}-\d{2}-\d{2}/)
  })

  it('says something different for each of the other statuses', () => {
    expect(retentionLabel(entry({ retention_status: 'pinned', prunes_at: null }))).toBe('PINNED — KEPT')
    expect(retentionLabel(entry({ retention_status: 'discard_pending', prunes_at: null })))
      .toBe('PRUNES AT THE NEXT SWEEP')
    expect(retentionLabel(entry({
      retention_status: 'pruned', prunes_at: null, audio_pruned_at: '2026-09-20T03:00:00Z',
    }))).toBe('AUDIO PRUNED 2026-09-20')
  })
})

describe('audioControlFor', () => {
  it('is playable with its duration and its PRUNES label', () => {
    const control = audioControlFor(entry())
    expect(control).toEqual({
      state: 'playable',
      label: '▶ PLAY SOURCE AUDIO',
      href: '/audio/22222222-2222-4222-8222-222222222222',
      duration: '1:44',
      retention: 'PRUNES 2026-09-20',
    })
  })

  it('is ABSENT rather than disabled for a recording this caller may not hear', () => {
    // audio_readable is a permission and nothing else. A visibly disabled
    // control would tell a reader that a recording they may not hear exists,
    // which is a fact about another account's activity.
    expect(audioControlFor(entry({ audio_readable: false, transcript: { present: true, readable: false, partial: false } })))
      .toEqual({ state: 'absent' })
  })

  it('renders *transcript kept, audio pruned <date>* FROM THE ENTRY', () => {
    const control = audioControlFor(entry({
      retention_status: 'pruned',
      prunes_at: null,
      audio_pruned_at: '2026-09-20T03:00:00Z',
      transcript: { present: true, readable: true, model: 'whisper.cpp/small.en', partial: false },
    }))
    expect(control).toEqual({
      state: 'pruned',
      label: 'TRANSCRIPT KEPT · AUDIO PRUNED 2026-09-20',
      transcriptKept: true,
    })
  })

  it('does not claim a transcript a pruned memo does not have', () => {
    // Deletion is gated on a durable transcript, so this should be
    // unreachable — and claiming "transcript kept" about a memo with none
    // would describe the one loss this system calls unrecoverable as if
    // nothing had happened.
    const control = audioControlFor(entry({
      retention_status: 'pruned',
      prunes_at: null,
      audio_pruned_at: '2026-09-20T03:00:00Z',
      transcript: { present: false, readable: false },
    }))
    expect(control).toEqual({ state: 'pruned', label: 'AUDIO PRUNED 2026-09-20', transcriptKept: false })
  })

  it('reports pruned to a caller who may not read the bytes either', () => {
    // The retention dates go to every member who can read the note, on
    // purpose: the pruned state is the more informative answer and it is
    // metadata they already hold.
    const control = audioControlFor(entry({
      audio_readable: false,
      retention_status: 'pruned',
      prunes_at: null,
      audio_pruned_at: '2026-09-20T03:00:00Z',
      transcript: { present: true, readable: false, partial: false },
    }))
    expect(control.state).toBe('pruned')
  })
})

describe('transcriptStateFor', () => {
  it('links the words for the author and the owner', () => {
    expect(transcriptStateFor(entry())).toEqual({
      state: 'readable',
      href: '/transcripts/22222222-2222-4222-8222-222222222222',
      label: 'TRANSCRIPT · whisper.cpp/small.en',
      partial: false,
    })
  })

  it('withholds the words but still says a transcript exists — and that it is incomplete', () => {
    // The only way a non-author learns that a `present` transcript is
    // GetTranscript's incomplete fallback.
    expect(transcriptStateFor(entry({
      transcript: { present: true, readable: false, model: 'whisper.cpp/small.en', partial: true },
    }))).toEqual({
      state: 'withheld',
      label: 'TRANSCRIPT · whisper.cpp/small.en · INCOMPLETE',
      partial: true,
    })
  })

  it('says not transcribed yet rather than nothing', () => {
    expect(transcriptStateFor(entry({ transcript: { present: false, readable: false } })))
      .toEqual({ state: 'none', label: 'NOT TRANSCRIBED YET' })
  })
})

describe('provenanceBlock', () => {
  it('assembles one entry into the block board 1c draws', () => {
    const block = provenanceBlock(entry(), { verb: 'create' })
    expect(block.header).toBe('FROM MEMO 12:55 · 1:44 · ROUTED BY SCRIBE')
    expect(block.audio.state).toBe('playable')
    expect(block.transcript.state).toBe('readable')
    expect(block.memoId).toBe('22222222-2222-4222-8222-222222222222')
    expect(block.revisionSeq).toBe(1)
  })
})

describe('leadProvenance', () => {
  it('leads with the oldest revision, which is what getNoteProvenance answers first', () => {
    const march = entry({ revision_seq: 1 })
    const june = entry({ revision_seq: 3, memo_id: '33333333-3333-4333-8333-333333333333' })
    expect(leadProvenance({ items: [march, june] })?.revision_seq).toBe(1)
  })

  it('is null for a note somebody typed, which is not an error', () => {
    expect(leadProvenance({ items: [] })).toBeNull()
  })
})
