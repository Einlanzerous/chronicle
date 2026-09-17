// Pure discussion-thread logic, kept out of the components so it is unit
// testable without mounting anything (CHRN-57, following CHRN-58's
// pageTree.ts/searchResult.ts precedent). Three things live here because
// CHRN-43's rulings made them properties of the DATA rather than of any one
// screen: where the unread rule sits, what "N REPLIES · M PARTICIPANTS"
// counts, and how an author's frozen `author_kind` becomes a visual
// treatment.
import type { components } from '@/api/schema.d.ts'

export type Discussion = components['schemas']['Discussion']
export type Thread = components['schemas']['Thread']
export type Turn = components['schemas']['Turn']
export type Participant = components['schemas']['Participant']

// ── Avatars and the agent tag ───────────────────────────────────────────
//
// CHRN-43 ruling 5: `author_kind` is stamped on the TURN by a trigger (CH092)
// and frozen there — never the viewer's own kind, never a join. So the
// treatment below is keyed on the turn's own `author_kind`, not on
// `currentUser.kind` (auth/index.ts), which is a different account entirely
// most of the time.
//
// CHRN-57's ticket comment is explicit that this canvas board (Chronicle.dc.html,
// `DISCUSSION, OPTION A`) is NOT followed literally here: the Scribe avatar
// there is painted the exact value tokens.css gives `--ch-generated`,
// CLAUDE.md invariant 1's steel, reserved for regenerated tier-1 content. An agent
// turn is authored tier 2, so steel would say the wrong thing about it. The
// square-vs-circle shape distinction the board draws IS kept — it needs no
// colour to read as a difference — and the colour becomes a neutral
// (`--ch-text-2`/`--ch-line`), with the mono `AGENT` tag next to the name
// carrying the fact a colour is not allowed to.
export interface AvatarTreatment {
  /** A person is a circle, matching the board; an agent is a square. */
  shape: 'circle' | 'square'
  /** Whether the mono `AGENT` tag renders beside the author's name. */
  agentTag: boolean
}

export function avatarTreatmentFor(kind: Turn['author_kind']): AvatarTreatment {
  return kind === 'agent' ? { shape: 'square', agentTag: true } : { shape: 'circle', agentTag: false }
}

/**
 * `Scribe` -> `SC`, matching the board's own two-letter badge; a multi-word
 * name takes the first letter of its first two words (`Jane Doe` -> `JD`),
 * the ordinary initials rule. Uppercased either way, and `?` for an empty
 * name rather than throwing on one.
 */
export function initialsFor(displayName: string): string {
  const words = displayName.trim().split(/\s+/).filter(Boolean)
  if (words.length === 0) return '?'
  if (words.length === 1) return words[0].slice(0, 2).toUpperCase()
  return (words[0][0] + words[1][0]).toUpperCase()
}

// ── "N REPLIES · M PARTICIPANTS" ────────────────────────────────────────
//
// Read directly off the board: turn 1 (the opener) plus two more turns
// renders as "3 REPLIES", so this counts every turn in the thread, opening
// turn included — it is the board's own word for "turn count", not
// "replies to the opener". Participants counts who is CURRENTLY on the
// thread (no `removed_at`) — `Thread.participants` is "everybody ever",
// removed ones included (openapi.yaml), and the summary line is about who
// is on it now, not the full history.
export function activeParticipantCount(participants: readonly Participant[]): number {
  return participants.filter((p) => !p.removed_at).length
}

export function threadCountsLabel(turns: readonly Turn[], participants: readonly Participant[]): string {
  return `${turns.length} REPLIES · ${activeParticipantCount(participants)} PARTICIPANTS`
}

// ── Resolved footer ──────────────────────────────────────────────────────
//
// `discussion.resolved.note` is absent when the thread concluded with no
// note (`into: "nothing"`, openapi.yaml's ResolveDiscussionRequest) — CHRN-43
// ruling 6 made that a deliberate, completable-later choice, not an error,
// so the footer says "RESOLVED" plainly rather than pretending a note ref it
// does not have.
export function resolvedFooterLabel(discussion: Pick<Discussion, 'resolved'>): string | null {
  if (!discussion.resolved) return null
  return discussion.resolved.note ? `RESOLVED → ${discussion.resolved.note}` : 'RESOLVED'
}

/** CH093: a resolved thread takes no more turns. The composer keys on this. */
export function canReply(discussion: Pick<Discussion, 'resolved'>): boolean {
  return !discussion.resolved
}

// ── Unread ────────────────────────────────────────────────────────────────
//
// `Thread.unread` is already `max(seq) − last_read_seq`, computed by the
// store (openapi.yaml's `getDiscussion`). Inverting that gives the seq of
// the first unread turn without needing to find the caller's own
// `Participant` row at all: `firstUnreadSeq = max(seq) − unread + 1`. Absent
// (not a participant, or an agent — both read the same "no marker to show")
// or zero (nothing unread) both mean no rule renders.
export function firstUnreadSeq(turns: readonly Turn[], unread: number | undefined): number | null {
  if (!turns.length || unread === undefined || unread <= 0) return null
  const maxSeq = turns.reduce((max, t) => Math.max(max, t.seq), turns[0].seq)
  const minSeq = turns.reduce((min, t) => Math.min(min, t.seq), turns[0].seq)
  const first = maxSeq - unread + 1
  // Defensive clamp: an `unread` larger than the turns on hand (should not
  // happen — the store computed both from the same row) still points at a
  // real turn rather than one that does not exist.
  return Math.max(first, minSeq)
}

/**
 * Whether opening the thread should report a read position at all.
 * `Thread.unread` is absent for exactly the two cases `markRead` itself
 * refuses (openapi.yaml: `403 not_a_participant`, `403 agent_has_no_marker`)
 * — so "the field is present" is also "the call is legal", with no second
 * rule to keep in sync.
 */
export function hasReadMarker(thread: Pick<Thread, 'unread'>): boolean {
  return thread.unread !== undefined
}
