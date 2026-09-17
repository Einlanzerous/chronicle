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
// Two rules, and they are the whole reason this file exists rather than the
// logic living inline in a `<template>`:
//
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
import { formatClock, formatDate } from './format'

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

/**
 * Board 1c's header line: `FROM MEMO 12:55 · 1:44 · ROUTED BY SCRIBE`.
 *
 * `ROUTED BY SCRIBE` is the revision's `verb`, which is non-null exactly when
 * a person confirmed a Scribe proposal — so it is read off `RevisionMeta` and
 * is absent for text somebody typed. The duration segment is dropped rather
 * than faked when there is no duration.
 */
export function provenanceHeaderLine(
  entry: Pick<MemoProvenance, 'captured_at' | 'duration_ms'>,
  revision?: Pick<RevisionMeta, 'verb'> | null,
): string {
  const parts = [`FROM MEMO ${formatClock(entry.captured_at)}`]
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
  | { state: 'playable'; label: string; href: string; duration: string | null; retention: string }
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
  }
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
