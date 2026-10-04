// Pure provenance-block logic (CHRN-107), board `1c`: the `FROM MEMO 12:55 ·
// 1:44 · ROUTED BY SCRIBE` line on a note's header and the `▶ PLAY SOURCE
// AUDIO · 1:44 · PRUNES 2026-09-20` control under its body — as a view model,
// kept out of any component so it is unit testable without mounting anything.
// notePane.ts and noteRevisions.ts's precedent.
//
// ============================================================================
// EVERY STATE COMES OFF THE PROVENANCE ENTRY. NOTHING IS COMPUTED, AND NOTHING
// IS READ OFF A FAILED REQUEST.
// ============================================================================
//
// Three rules, and they are the whole reason this file exists rather than the
// logic living inline in a `<template>`:
//
//  0. `recorded_at` IS A CLAIM, NOT A FACT (CHRN-123). A client's clock asserted
//     it and nothing upstream filters it, so it is used only where it can be
//     checked against `captured_at`, and shown with its date wherever that
//     date is not the arrival's own. See `recordingInstant`.
//  1. NO DATE IS DERIVED FROM `captured_at`. `prunes_at` is null on four of the
//     five retention statuses, and `awaiting_transcript` means "prunes WHEN
//     TRANSCRIBED" — there is no date, and rendering `captured_at + 30 days`
//     in its place produces exactly the label CHRN-22 §3 forbids: one that
//     passes while nothing happens. So a date is shown when the contract hands
//     one over, and never otherwise.
//  2. THE PRUNED STATE IS RENDERED FROM `retention_status`, `audio_pruned_at`
//     and `transcript.present` — never from `getMemoAudio`'s 410. A media
//     element exposes neither the status nor the body of a failed load, only a
//     `MediaError`, so the refusal's `code` is unreadable by the one thing
//     that would meet it.
//
// The render itself is CHRN-109. This is the obligation CHRN-107 owes it: a
// contract it can draw from with no computed date and no second request.
import type { components } from '@/api/schema.d.ts'
import { formatClock, formatDate, formatTimestamp } from './format'

export type MemoProvenance = components['schemas']['MemoProvenance']
export type RevisionMeta = components['schemas']['RevisionMeta']
export type ProvenanceList = components['schemas']['ProvenanceList']

/**
 * `1:44`, and `null` for a memo with neither a header duration nor a
 * transcript — the pre-transcription window, which the contract states as a
 * null rather than a zero so that a UI can leave the segment out instead of
 * rendering `0:00`.
 */
export function formatDurationMs(ms: number | null | undefined): string | null {
  if (ms == null || ms < 0) return null
  const total = Math.round(ms / 1000)
  const seconds = total % 60
  const minutes = Math.floor(total / 60) % 60
  const hours = Math.floor(total / 3600)
  const mm = hours > 0 ? String(minutes).padStart(2, '0') : String(minutes)
  return `${hours > 0 ? `${hours}:` : ''}${mm}:${String(seconds).padStart(2, '0')}`
}

// What the Go server emits for a `date-time`: RFC 3339, with an offset. A value
// that is not this shape is not something the contract can have sent.
const RFC3339 = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/

/**
 * The instant the header names: `recorded_at ?? captured_at`, except that
 * `recorded_at` is only believed where it can be checked.
 *
 * `recorded_at` is whatever a client's clock said. The upload path refuses a
 * UTC year outside 100–9900 because the response encoder needs it to, and
 * nothing else (CHRN-118 ruling 2), so year 100, year 9900 and a plausible-but
 * -wrong 2019 all arrive here as stored. Two things are checkable from the
 * payload alone, and either failing sends the read back to `captured_at`:
 *
 *  - it is a well-formed RFC 3339 instant, and
 *  - it does not postdate `captured_at`. A recording cannot be made after its
 *    own bytes arrived, so that value is wrong whatever the reason. There is
 *    deliberately no tolerance for clock skew: the fallback IS the arrival, so
 *    a phone running a little fast costs the reader a few seconds and nothing
 *    else, and a threshold would be one more number nobody has evidence for.
 *
 * What is NOT checkable is a value in the past, and it gets no floor. An
 * offline capture can wait on a phone for as long as it likes, and an import
 * path would make an old date legitimate, so any floor discards a true value
 * to catch a false one. Those are made visible instead — see
 * `provenanceHeaderLine`, which prints the date whenever it is not the
 * arrival's own.
 *
 * Nothing here is written back or feeds retention: `captured_at` stays the
 * prune clock, and this is the display's choice of which instant to show.
 */
export function recordingInstant(entry: Pick<MemoProvenance, 'captured_at' | 'recorded_at'>): string {
  const claimed = entry.recorded_at
  // `== null`: the contract says null, but a key that is absent (a server that
  // predates CHRN-123) reads the same and must not throw.
  if (claimed == null || !RFC3339.test(claimed)) return entry.captured_at
  const recorded = Date.parse(claimed)
  const arrived = Date.parse(entry.captured_at)
  // Unverifiable is not the same as trusted: with no arrival to check against,
  // the claim is left unused.
  if (!Number.isFinite(recorded) || !Number.isFinite(arrived) || recorded > arrived) return entry.captured_at
  return claimed
}

/**
 * Board 1c's header line: `FROM MEMO 12:55 · 1:44 · ROUTED BY SCRIBE`.
 *
 * The time is the recording's own where `recordingInstant` believes one and the
 * arrival's otherwise. It is a bare clock — the date is "already on the note"
 * — for as long as that holds, which is when the recording and its arrival fall
 * on the same day, and it is always true of `captured_at`. It stops being true
 * for exactly the memos `recorded_at` exists for: recorded on a train last
 * night and uploaded this morning would read `FROM MEMO 23:50` beside a note
 * dated today, a time that has not happened yet. So a different day prints the
 * whole `2026-08-20 23:50`. That is also what keeps a wrong clock visible: a
 * bare `12:55` off a phone reset to 1970 would pass for a real one.
 *
 * `ROUTED BY SCRIBE` is the revision's `verb`, which is non-null exactly when
 * a person confirmed a Scribe proposal — so it is read off `RevisionMeta` and
 * is absent for text somebody typed. The duration segment is dropped rather
 * than faked when there is no duration.
 */
export function provenanceHeaderLine(
  entry: Pick<MemoProvenance, 'captured_at' | 'recorded_at' | 'duration_ms'>,
  revision?: Pick<RevisionMeta, 'verb'> | null,
): string {
  const at = recordingInstant(entry)
  // Both are rendered in the server's one zone, so their date parts compare.
  const sameDay = formatDate(at) === formatDate(entry.captured_at)
  const parts = [`FROM MEMO ${sameDay ? formatClock(at) : formatTimestamp(at)}`]
  const duration = formatDurationMs(entry.duration_ms)
  if (duration) parts.push(duration)
  if (revision?.verb) parts.push('ROUTED BY SCRIBE')
  return parts.join(' · ')
}

/**
 * The retention label beside the play control, and the reason this is a
 * function rather than a template expression: FOUR OF THE FIVE STATUSES HAVE
 * NO DATE, and each says something different about why.
 */
export function retentionLabel(
  entry: Pick<MemoProvenance, 'retention_status' | 'prunes_at' | 'audio_pruned_at'>,
): string {
  switch (entry.retention_status) {
    case 'scheduled':
      // The only status the contract puts a date on. Still guarded: a null
      // here renders the reason, never an arithmetic guess.
      return entry.prunes_at ? `PRUNES ${formatDate(entry.prunes_at)}` : 'PRUNES ON THE USUAL SCHEDULE'
    case 'awaiting_transcript':
      return 'PRUNES WHEN TRANSCRIBED'
    case 'discard_pending':
      return 'PRUNES AT THE NEXT SWEEP'
    case 'pinned':
      return 'PINNED — KEPT'
    case 'pruned':
      return entry.audio_pruned_at ? `AUDIO PRUNED ${formatDate(entry.audio_pruned_at)}` : 'AUDIO PRUNED'
  }
}

export type AudioControl =
  /** `▶ PLAY SOURCE AUDIO · 1:44 · PRUNES 2026-09-20`. */
  | { state: 'playable'; label: string; href: string; duration: string | null; retention: string; pinnable: boolean }
  /** The bytes went by policy; the transcript is what remains. */
  | { state: 'pruned'; label: string; transcriptKept: boolean }
  /** ABSENT, not disabled: this caller may not have somebody else's recording. */
  | { state: 'absent' }

/**
 * The play control's three states.
 *
 * **`pruned` is checked FIRST, before `audio_readable`.** The flag is a
 * permission to fetch bytes, and once they are gone there are none to permit;
 * the retention state is metadata every member who can read the note already
 * has, deliberately, and it is the more informative answer for all of them.
 *
 * **`absent` rather than disabled.** A control that is visibly disabled tells
 * a reader that a recording they may not hear exists, which is a fact about
 * another account's activity; a control that is not drawn tells them nothing.
 */
export function audioControlFor(entry: MemoProvenance): AudioControl {
  if (entry.retention_status === 'pruned') {
    const kept = entry.transcript.present
    const pruned = retentionLabel(entry)
    return {
      state: 'pruned',
      // The transcript half is CLAIMED ONLY WHEN IT IS THERE. Deletion is
      // gated on a durable transcript, so a pruned memo without one should be
      // impossible — and saying "transcript kept" about a memo that has none
      // would be this system describing the one loss it calls unrecoverable as
      // if nothing had happened.
      label: kept ? `TRANSCRIPT KEPT · ${pruned}` : pruned,
      transcriptKept: kept,
    }
  }
  if (!entry.audio_readable) return { state: 'absent' }
  return {
    state: 'playable',
    label: '▶ PLAY SOURCE AUDIO',
    // The same-origin `chronicle_session` cookie is what makes a bare
    // `<audio src>` work at all (web/src/api/client.ts).
    href: `/audio/${entry.memo_id}`,
    duration: formatDurationMs(entry.duration_ms),
    retention: retentionLabel(entry),
    pinnable: canPin(entry),
  }
}

/**
 * Whether to draw KEEP FOREVER (CHRN-128). The same two facts the play
 * control reads, for the same reasons: `audio_readable` is the permission --
 * `raiseMemoRetention` is the author's and the owner's, exactly as the bytes
 * are -- and `retention_status` says whether there is anything left to do. A
 * pinned memo is already kept; a pruned one has nothing to keep, and the
 * server refuses it `410`. So a caller who cannot read the audio is offered
 * no control, rather than one that fails.
 */
export function canPin(entry: Pick<MemoProvenance, 'audio_readable' | 'retention_status'>): boolean {
  return entry.audio_readable && entry.retention_status !== 'pinned' && entry.retention_status !== 'pruned'
}

export type RetentionState = components['schemas']['RetentionState']

/**
 * Folds `raiseMemoRetention`'s answer into the entry it is about, so the block
 * redraws `PRUNES <date>` as `PINNED — KEPT` from what the server said rather
 * than from an assumption about what a 200 means. The status is the server's
 * string, narrowed only if it is one this client knows how to label.
 */
export function withRetention(entry: MemoProvenance, state: RetentionState): MemoProvenance {
  const known: readonly MemoProvenance['retention_status'][] = [
    'pruned',
    'pinned',
    'awaiting_transcript',
    'discard_pending',
    'scheduled',
  ]
  const status = known.find((k) => k === state.retention_status)
  if (!status || state.memo_id !== entry.memo_id) return entry
  return { ...entry, retention_status: status, prunes_at: state.prunes_at }
}

export type TranscriptState =
  | { state: 'readable'; href: string; label: string; partial: boolean }
  | { state: 'withheld'; label: string; partial: boolean }
  | { state: 'none'; label: string }

/**
 * Whether there is a transcript, and whether this caller may read it — two
 * facts, which is why the contract carries them as two fields.
 *
 * `partial` is carried into both present states because a non-author holds
 * `readable: false` and has no other way to learn that a `present` transcript
 * is `GetTranscript`'s incomplete fallback.
 */
export function transcriptStateFor(entry: MemoProvenance): TranscriptState {
  const { present, readable, partial, model } = entry.transcript
  if (!present) return { state: 'none', label: 'NOT TRANSCRIBED YET' }
  const suffix = partial ? ' · INCOMPLETE' : ''
  const label = `TRANSCRIPT${model ? ` · ${model}` : ''}${suffix}`
  if (!readable) return { state: 'withheld', label, partial: partial === true }
  return { state: 'readable', href: `/transcripts/${entry.memo_id}`, label, partial: partial === true }
}

export interface ProvenanceBlock {
  memoId: string
  revisionSeq: number
  header: string
  audio: AudioControl
  transcript: TranscriptState
}

/** One entry's whole block, as board 1c draws it. */
export function provenanceBlock(
  entry: MemoProvenance,
  revision?: Pick<RevisionMeta, 'verb'> | null,
): ProvenanceBlock {
  return {
    memoId: entry.memo_id,
    revisionSeq: entry.revision_seq,
    header: provenanceHeaderLine(entry, revision),
    audio: audioControlFor(entry),
    transcript: transcriptStateFor(entry),
  }
}

/**
 * The note header renders `items[0]`: `getNoteProvenance` answers oldest
 * revision first, and a note fed by several memos leads with the one that
 * started it. An empty list is a note somebody typed — no block at all, which
 * is not an error.
 */
export function leadProvenance(list: ProvenanceList): MemoProvenance | null {
  return list.items.length > 0 ? list.items[0] : null
}
