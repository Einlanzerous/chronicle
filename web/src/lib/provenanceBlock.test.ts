import { describe, expect, it } from 'vitest'
import {
  audioControlFor,
  canPin,
  withRetention,
  formatDurationMs,
  leadProvenance,
  provenanceBlock,
  provenanceHeaderLine,
  recordingInstant,
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
    recorded_at: null,
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

  // CHRN-123: the time is the recording's own where one is believed.
  it('shows the recording time, not the arrival, when the phone sent one', () => {
    // A minute of speech, uploaded straight away: same day, so still a bare clock.
    expect(provenanceHeaderLine(entry({ recorded_at: '2026-08-21T12:53:30Z' }), { verb: 'create' }))
      .toBe('FROM MEMO 12:53 · 1:44 · ROUTED BY SCRIBE')
  })

  it('prints the whole date when the recording is on another day than its arrival', () => {
    // Recorded on the train last night, uploaded this morning. A bare 23:50
    // beside a note dated today would be a time that has not happened yet.
    expect(provenanceHeaderLine(entry({ recorded_at: '2026-08-20T23:50:00Z' }), null))
      .toBe('FROM MEMO 2026-08-20 23:50 · 1:44')
  })

  it('leaves the line exactly as it was for a memo with no recorded_at', () => {
    // Every memo on prod today: the field exists since 1.23 and the phone
    // sender is newer than the installed build.
    expect(provenanceHeaderLine(entry({ recorded_at: null }), { verb: 'create' }))
      .toBe('FROM MEMO 12:55 · 1:44 · ROUTED BY SCRIBE')
  })
})

describe('recordingInstant', () => {
  const captured = '2026-08-21T12:55:00Z'

  it('is recorded_at ?? captured_at', () => {
    expect(recordingInstant({ captured_at: captured, recorded_at: '2026-08-20T23:50:00Z' })).toBe('2026-08-20T23:50:00Z')
    expect(recordingInstant({ captured_at: captured, recorded_at: null })).toBe(captured)
  })

  it('reads a key that is absent as null, for a server that predates the field', () => {
    // The contract says required-but-nullable. A payload without the key is
    // not the contract, and it must not throw.
    const older = { captured_at: captured } as Parameters<typeof recordingInstant>[0]
    expect(recordingInstant(older)).toBe(captured)
  })

  it('accepts what Go emits: fractional seconds, and an offset instead of Z', () => {
    expect(recordingInstant({ captured_at: captured, recorded_at: '2026-08-21T12:40:11.123456Z' }))
      .toBe('2026-08-21T12:40:11.123456Z')
    // 14:00+02:00 is 12:00Z, before the 12:55Z arrival.
    expect(recordingInstant({ captured_at: captured, recorded_at: '2026-08-21T14:00:00+02:00' }))
      .toBe('2026-08-21T14:00:00+02:00')
  })

  it('does not believe a recording made after its own bytes arrived', () => {
    // No real recording postdates its arrival, so this is wrong whatever the
    // cause: a clock running ahead, or a client that lies.
    for (const recorded_at of [
      '2026-08-21T12:55:01Z', // one second after: no skew allowance, the fallback is the arrival
      '2027-01-01T00:00:00Z',
      '9900-12-31T23:59:59Z', // the upload bound's far edge
    ]) {
      expect(recordingInstant({ captured_at: captured, recorded_at }), recorded_at).toBe(captured)
    }
    // The same instant is not later.
    expect(recordingInstant({ captured_at: captured, recorded_at: captured })).toBe(captured)
  })

  it('does not throw on, or believe, anything that is not an RFC 3339 instant', () => {
    for (const recorded_at of ['', 'yesterday', '2026-08-20', '2026-08-20T23:50:00', '12:55', 'NaN', '  ']) {
      expect(recordingInstant({ captured_at: captured, recorded_at }), JSON.stringify(recorded_at)).toBe(captured)
    }
  })

  it('leaves a claim unused when there is no arrival to check it against', () => {
    expect(recordingInstant({ captured_at: 'not a date', recorded_at: '2026-08-20T23:50:00Z' })).toBe('not a date')
  })

  it('does NOT floor a value in the past — it makes it visible instead', () => {
    // Year 100 is the far edge of what the server accepts, and 2019 is a
    // plausible clock reset. Neither can be told from a legitimate old capture
    // (an offline queue, or a future import), so neither is discarded...
    expect(recordingInstant({ captured_at: captured, recorded_at: '0100-01-01T12:00:00Z' })).toBe('0100-01-01T12:00:00Z')
    expect(recordingInstant({ captured_at: captured, recorded_at: '2019-03-01T09:00:00Z' })).toBe('2019-03-01T09:00:00Z')
    // ...and the header prints their dates, so a wrong clock reads as one
    // rather than passing for a bare time of day.
    expect(provenanceHeaderLine(entry({ recorded_at: '0100-01-01T12:00:00Z' }), null))
      .toBe('FROM MEMO 0100-01-01 12:00 · 1:44')
    expect(provenanceHeaderLine(entry({ recorded_at: '2019-03-01T09:00:00Z' }), null))
      .toBe('FROM MEMO 2019-03-01 09:00 · 1:44')
  })

  it('is display only: nothing else the entry says moves with it', () => {
    // captured_at is the prune clock and the contract hands over prunes_at
    // itself. A recording time a year ago moves neither.
    const hostile = entry({ recorded_at: '2020-01-01T00:00:00Z' })
    expect(retentionLabel(hostile)).toBe('PRUNES 2026-09-20')
    expect(audioControlFor(hostile)).toEqual(audioControlFor(entry()))
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
      pinnable: true,
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

// CHRN-128: the pin.
describe('canPin', () => {
  it('offers the pin to a caller who may read the audio, while there is something to raise', () => {
    expect(canPin(entry())).toBe(true)
    expect(canPin(entry({ retention_status: 'awaiting_transcript', prunes_at: null }))).toBe(true)
    expect(canPin(entry({ retention_status: 'discard_pending', prunes_at: null }))).toBe(true)
  })

  it('draws no control for a caller who cannot read the audio', () => {
    expect(canPin(entry({ audio_readable: false }))).toBe(false)
    // ...and the block carries no control of any kind for them.
    expect(audioControlFor(entry({ audio_readable: false }))).toEqual({ state: 'absent' })
  })

  it('offers nothing once pinned, and nothing over audio that is gone', () => {
    expect(canPin(entry({ retention_status: 'pinned', prunes_at: null }))).toBe(false)
    expect(canPin(entry({ retention_status: 'pruned', prunes_at: null, audio_pruned_at: '2026-09-20T03:00:00Z' }))).toBe(false)
  })

  it('rides on the playable control', () => {
    const control = audioControlFor(entry())
    expect(control.state === 'playable' && control.pinnable).toBe(true)
    const pinned = audioControlFor(entry({ retention_status: 'pinned', prunes_at: null }))
    expect(pinned.state === 'playable' && pinned.pinnable).toBe(false)
  })
})

describe('withRetention', () => {
  const answer = {
    memo_id: '22222222-2222-4222-8222-222222222222',
    retention: 'forever' as const,
    retention_status: 'pinned',
    prunes_at: null,
  }

  it('moves PRUNES <date> to PINNED — KEPT from the server\'s answer', () => {
    const before = entry()
    expect(audioControlFor(before)).toMatchObject({ retention: 'PRUNES 2026-09-20', pinnable: true })
    expect(audioControlFor(withRetention(before, answer))).toMatchObject({ retention: 'PINNED — KEPT', pinnable: false })
  })

  it('leaves an entry about a different memo alone', () => {
    const other = entry({ memo_id: '33333333-3333-4333-8333-333333333333' })
    expect(withRetention(other, answer)).toBe(other)
  })

  it('does not invent a label for a status it does not know', () => {
    const before = entry()
    expect(withRetention(before, { ...answer, retention_status: 'archived' })).toBe(before)
  })
})
