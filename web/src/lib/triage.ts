// Pure batch-triage logic (CHRN-55, board 1e), kept out of TriageView.vue so
// the rules the screen exists to hold are unit testable without mounting
// anything: which rows a keypress may decide, what an override has to carry,
// what each of the POST's five per-item statuses does to a row, and what the
// header and footer are allowed to count.
//
// Three facts about the contract shape everything below (internal/triage):
//
//   - AN OVERRIDE IS NOT A PATCH. `overrideProposal` builds the decision from
//     the override alone and validates it like a model's, so an edit has to
//     send every field its destination requires -- title, and the body /
//     description / opening post -- not only the ones the person touched.
//   - A DISCARD IS TERMINAL and never accepted as shown: it takes an override.
//   - `confirm_edit` is legal on an append or a supersede and on nothing else;
//     it is the deliberate per-item confirmation those two verbs cost.
import type { components } from '@/api/schema.d.ts'

export type BatchItem = components['schemas']['BatchItem']
export type Proposal = components['schemas']['Proposal']
export type LinkState = components['schemas']['LinkState']
export type TriageResult = components['schemas']['TriageResult']
export type TriageDecision = components['schemas']['TriageDecision']
export type Override = components['schemas']['Override']
export type DeferredItem = components['schemas']['DeferredItem']

export type Destination = Proposal['destination']
export const DESTINATIONS: readonly Destination[] = ['NOTE', 'TICKET', 'DISCUSSION', 'DISCARD']

/** Switchyard's enum, as internal/scribe/scribe.go's TicketTypes holds it. */
export const TICKET_TYPES = ['task', 'bug', 'spike', 'epic'] as const

export type Verb = NonNullable<Proposal['verb']>
export const VERBS: readonly Verb[] = ['create', 'append', 'supersede', 'relate']

/** The board's `DISCARDED · UNDO 10 MIN`. See TriageView.vue for why the
 *  window is client-side: the server has no undo, because `discarded` is
 *  terminal, so the decision is HELD BACK for this long rather than recalled. */
export const DISCARD_UNDO_MS = 10 * 60 * 1000

/** A decision the server did not land, by its own per-item status. */
export type ProblemStatus = 'needs_input' | 'stale' | 'refused' | 'failed'

/** What this session has done to a row, on top of what the batch said. */
export type LocalState =
  | { kind: 'pending' }
  | { kind: 'sending' }
  | { kind: 'accepted'; edited: boolean; result: TriageResult; at: string }
  | { kind: 'held'; reason: string }
  | { kind: 'discarding'; dueMs: number }
  | { kind: 'discarded' }
  | { kind: 'problem'; status: ProblemStatus; reason: string }

export interface TriageRow {
  item: BatchItem
  local: LocalState
  /** A row built from GET /triage/deferred rather than from the batch: it has
   *  an excerpt and a reason and no proposal, so releasing it means re-reading
   *  the batch rather than un-greying the row. */
  fromDeferred?: boolean
  /** What the person typed, kept when an edit did not land so E reopens it
   *  rather than the proposal. */
  draft?: Draft
  /** One line carried across a re-read -- "the proposal changed". */
  notice?: string
}

/** The board's eight drawn states, plus the two this client adds while a
 *  request is out (`sending`) and for a link the sweep is still working. */
export type RowKind =
  | 'prefilled'
  | 'needs-input'
  | 'discard-proposed'
  | 'sending'
  | 'accepted'
  | 'held'
  | 'discarding'
  | 'discarded'
  | 'link-in-flight'
  | 'link-unresolved'
  | 'link-ambiguous'

export function rowKind(row: TriageRow): RowKind {
  switch (row.local.kind) {
    case 'sending':
    case 'accepted':
    case 'held':
    case 'discarding':
    case 'discarded':
      return row.local.kind
  }
  // A decision ALREADY RECORDED outranks the proposal: the memo is not waiting
  // for a person, it is waiting for the sweep. `refused` is the exception --
  // nothing was created, the server takes a new accept as a new attempt
  // (CHRN-141), and the row is decidable again.
  switch (row.item.link?.state) {
    case 'in_flight':
      return 'link-in-flight'
    case 'unresolved':
      return 'link-unresolved'
    case 'ambiguous':
      return 'link-ambiguous'
  }
  // A transient failure or a refusal of an accept-as-shown is retried as it
  // was (⏎ again): a refusal created nothing, and the server attempts the same
  // decision afresh (CHRN-141). Everything else the server sent back -- a
  // cleared target, a stale proposal, an EDIT whose draft is kept on the row --
  // needs the editor.
  const retriable = row.local.kind === 'problem' && (row.local.status === 'failed' || row.local.status === 'refused')
  if (row.local.kind === 'problem' && (!retriable || row.draft)) return 'needs-input'
  if (row.item.status !== 'valid' || !row.item.proposal) return 'needs-input'
  if (row.item.proposal.destination === 'DISCARD') return 'discard-proposed'
  return 'prefilled'
}

/** Whether a person can still decide this row from this screen. */
export function isDecidable(row: TriageRow): boolean {
  const k = rowKind(row)
  return k === 'prefilled' || k === 'needs-input' || k === 'discard-proposed'
}

/** Whether the row still wants a decision from somebody. A discard inside
 *  its undo window does not: the person has decided, and the row says so. */
export function isWaiting(row: TriageRow): boolean {
  const k = row.local.kind
  return !row.fromDeferred && k !== 'accepted' && k !== 'held' && k !== 'discarding' && k !== 'discarded'
}

/** Whether ACCEPT ALL takes this row. `pre_acceptable` is the server's -- the
 *  one reader of the confidence threshold -- and this only adds "and nobody
 *  has touched it here". A row the server already answered about is never
 *  swept up again by a batch key. */
export function isPrefilled(row: TriageRow): boolean {
  return row.local.kind === 'pending' && rowKind(row) === 'prefilled' && row.item.pre_acceptable
}

/** append and supersede write into a note somebody already wrote. */
export function editsExistingNote(p: Proposal | undefined): boolean {
  return p?.destination === 'NOTE' && (p.verb === 'append' || p.verb === 'supersede')
}

export function verbNeedsTarget(verb: Verb): boolean {
  return verb !== 'create'
}

// ── Decisions ────────────────────────────────────────────────────────────

/** Accept as shown. `confirmEdit` is set for ONE row at a time and never by
 *  the batch path -- the half of the rule the server cannot check. */
export function acceptDecision(item: BatchItem, single: boolean): TriageDecision {
  const d: TriageDecision = {
    memo_id: item.memo_id,
    proposer: item.proposer,
    generation: item.generation ?? null,
  }
  if (single && editsExistingNote(item.proposal)) d.confirm_edit = true
  return d
}

export function discardDecision(item: BatchItem): TriageDecision {
  return {
    memo_id: item.memo_id,
    proposer: item.proposer,
    generation: item.generation ?? null,
    override: { destination: 'DISCARD' },
  }
}

/** What the editor holds. One `text` for the three destinations that carry
 *  prose, so changing NOTE to TICKET does not throw the draft away. */
export interface Draft {
  destination: Destination
  title: string
  pagePath: string
  projectKey: string
  ticketType: string
  verb: Verb
  targetNote: string
  text: string
}

export function draftFor(item: BatchItem): Draft {
  const p = item.proposal
  return {
    destination: p?.destination ?? 'NOTE',
    title: p?.title ?? '',
    // `page_path` when the proposal still has one; otherwise the board's
    // "pre-filled from nearest_page". Both are empty on the live corpus's NOTE
    // rows, where stage 2 cleared the page -- then the picker starts blank.
    pagePath: p?.page_path ?? p?.nearest_page ?? '',
    projectKey: p?.project_key ?? '',
    ticketType: p?.ticket_type || 'task',
    verb: p?.verb ?? 'create',
    targetNote: p?.target_note ?? '',
    text: p?.body || p?.description || p?.opening_post || item.excerpt,
  }
}

/** The label over the editor's prose field, per destination. */
export function textLabel(dest: Destination): string {
  switch (dest) {
    case 'NOTE':
      return 'BODY'
    case 'TICKET':
      return 'DESCRIPTION'
    case 'DISCUSSION':
      return 'OPENING POST'
    default:
      return 'TEXT'
  }
}

/** The first thing wrong with a draft, in a person's words, or null. The
 *  server validates again and is the authority; this only saves a round trip
 *  for the blanks it would refuse anyway. */
export function validateDraft(d: Draft): string | null {
  if (d.destination === 'DISCARD') return null
  if (!d.title.trim()) return 'A title is required.'
  if (!d.text.trim()) return `The ${textLabel(d.destination).toLowerCase()} is required.`
  if (d.destination === 'TICKET' && !d.projectKey.trim()) {
    return 'A ticket needs a project key — it cannot be moved between projects afterwards.'
  }
  if (d.destination === 'NOTE') {
    if (verbNeedsTarget(d.verb) && !d.targetNote.trim()) return `A note reference is required to ${d.verb}.`
    if (d.verb === 'create' && !d.pagePath.trim()) return 'A note must name the page it belongs on.'
  }
  return null
}

export function buildOverride(d: Draft): Override {
  switch (d.destination) {
    case 'DISCARD':
      return { destination: 'DISCARD' }
    case 'TICKET':
      return {
        destination: 'TICKET',
        title: d.title.trim(),
        project_key: d.projectKey.trim().toUpperCase(),
        ticket_type: d.ticketType,
        description: d.text,
      }
    case 'DISCUSSION':
      return { destination: 'DISCUSSION', title: d.title.trim(), opening_post: d.text }
    case 'NOTE': {
      const o: Override = { destination: 'NOTE', title: d.title.trim(), verb: d.verb, body: d.text }
      if (verbNeedsTarget(d.verb)) o.target_note = d.targetNote.trim()
      if (d.pagePath.trim()) o.page_path = normalisePagePath(d.pagePath)
      return o
    }
  }
}

/** The board draws a path as `estate / storage / amber`; the API takes
 *  `estate/storage/amber`. Accept either from a person's hands. */
export function normalisePagePath(path: string): string {
  return path
    .split('/')
    .map((s) => s.trim())
    .filter(Boolean)
    .join('/')
}

export function editDecision(item: BatchItem, d: Draft): TriageDecision {
  return {
    memo_id: item.memo_id,
    proposer: item.proposer,
    generation: item.generation ?? null,
    override: buildOverride(d),
  }
}

// ── Results ──────────────────────────────────────────────────────────────

/**
 * Folds one per-item result back into its row. FIVE STATUSES AND THEY STAY
 * FIVE on the screen: only `applied` takes the row out of the batch. The
 * other four leave it VISIBLY STILL PENDING, each saying why -- that is the
 * ticket's second Done-when, and the reason there is no batch-wide status to
 * report instead.
 */
export function applyResult(row: TriageRow, result: TriageResult, edited: boolean, nowIso: string): TriageRow {
  switch (result.status) {
    case 'applied':
      return { ...row, notice: undefined, local: { kind: 'accepted', edited, result, at: nowIso } }
    case 'needs_input':
      // Carries the POST-BUMP generation, so the completed resend is not
      // `stale` without an intervening GET -- and what stage 2 cleared, which
      // is what the person now has to supply.
      return {
        ...row,
        item: {
          ...row.item,
          status: 'needs_input',
          pre_acceptable: false,
          generation: result.generation ?? row.item.generation,
          cleared_fields: result.cleared?.length ? result.cleared : row.item.cleared_fields,
        },
        local: {
          kind: 'problem',
          status: 'needs_input',
          reason: result.reason || 'A target no longer resolves — supply it and confirm.',
        },
      }
    case 'stale':
      return {
        ...row,
        local: { kind: 'problem', status: 'stale', reason: 'The proposal changed since this screen read it.' },
      }
    case 'refused':
      return { ...row, local: { kind: 'problem', status: 'refused', reason: result.reason || 'Refused.' } }
    default:
      return {
        ...row,
        local: { kind: 'problem', status: 'failed', reason: result.reason || 'It did not land. Nothing was lost.' },
      }
  }
}

/** A request that never produced per-item results at all. */
export function failRow(row: TriageRow, reason: string): TriageRow {
  return { ...row, local: { kind: 'problem', status: 'failed', reason } }
}

/**
 * Folds a fresh GET /triage/batch into the rows on screen.
 *
 * What this session decided stays as it is drawn. A row still waiting takes
 * the server's current word -- which is what un-sticks a `stale` one -- and a
 * waiting row the server no longer lists was decided somewhere else, so it
 * leaves. New memos append, oldest first, as the batch orders them.
 */
export function mergeBatch(rows: readonly TriageRow[], items: readonly BatchItem[]): TriageRow[] {
  const fresh = new Map(items.map((it) => [it.memo_id, it]))
  const out: TriageRow[] = []
  const seen = new Set<string>()
  for (const row of rows) {
    if (row.fromDeferred) continue
    seen.add(row.item.memo_id)
    const it = fresh.get(row.item.memo_id)
    const settled = row.local.kind !== 'pending' && row.local.kind !== 'problem'
    if (settled) {
      out.push(row)
    } else if (it) {
      const wasStale = row.local.kind === 'problem' && row.local.status === 'stale'
      const moved = it.generation !== row.item.generation
      out.push({
        ...row,
        item: it,
        // A refusal or a failure is about THIS decision and outlives a
        // re-read; a stale one is answered by the re-read itself.
        local: wasStale || (moved && row.local.kind === 'problem') ? { kind: 'pending' } : row.local,
        notice: wasStale || moved ? 'PROPOSAL CHANGED SINCE YOU READ IT' : row.notice,
      })
    }
  }
  for (const it of items) {
    if (!seen.has(it.memo_id)) out.push({ item: it, local: { kind: 'pending' } })
  }
  return out
}

export function deferredRow(d: DeferredItem): TriageRow {
  return {
    fromDeferred: true,
    item: {
      memo_id: d.memo_id,
      captured_at: d.captured_at,
      duration_ms: d.duration_ms,
      excerpt: d.excerpt ?? '',
      proposer: '',
      generation: null,
      status: 'absent',
      pre_acceptable: false,
    },
    local: { kind: 'held', reason: d.reason ?? '' },
  }
}

// ── Counts ───────────────────────────────────────────────────────────────

export interface TriageCounts {
  /** Rows the server is still holding for a decision. */
  waiting: number
  prefilled: number
  needInput: number
  /** Decisions that did not finish landing: this session's refusals and
   *  failures, and the links the sweep has not resolved. */
  failed: number
  accepted: number
  edited: number
  held: number
  discarded: number
}

export function countRows(rows: readonly TriageRow[]): TriageCounts {
  const c: TriageCounts = {
    waiting: 0,
    prefilled: 0,
    needInput: 0,
    failed: 0,
    accepted: 0,
    edited: 0,
    held: 0,
    discarded: 0,
  }
  for (const row of rows) {
    if (row.fromDeferred) continue
    if (isWaiting(row)) c.waiting++
    const kind = rowKind(row)
    switch (kind) {
      case 'accepted':
        if (row.local.kind === 'accepted' && row.local.edited) c.edited++
        else c.accepted++
        break
      case 'held':
        c.held++
        break
      case 'discarding':
      case 'discarded':
        c.discarded++
        break
      case 'link-unresolved':
      case 'link-ambiguous':
        c.failed++
        break
      case 'prefilled':
      case 'needs-input':
      case 'discard-proposed': {
        const problem = row.local.kind === 'problem' ? row.local.status : null
        if (problem === 'failed' || problem === 'refused' || row.item.link?.state === 'refused') c.failed++
        else if (isPrefilled(row)) c.prefilled++
        else c.needInput++
        break
      }
    }
  }
  return c
}

/** `40 MEMOS · 31 PRE-FILLED · 6 NEED INPUT · 3 FAILED`. `more` is the batch
 *  cap talking: the server hands over one screen (25) and no total, so a full
 *  screen reads `25+` rather than claiming 25 is all there is. */
export function headerCounts(c: TriageCounts, more: boolean): string {
  const memos = `${c.waiting}${more ? '+' : ''} ${c.waiting === 1 && !more ? 'MEMO' : 'MEMOS'}`
  return `${memos} · ${c.prefilled} PRE-FILLED · ${c.needInput} NEED INPUT · ${c.failed} FAILED`
}

export function footerCounts(c: TriageCounts, more: boolean): string {
  return (
    `${c.accepted} ACCEPTED · ${c.edited} EDITED · ${c.held} HELD · ${c.discarded} DISCARDED · ` +
    `${c.waiting}${more ? '+' : ''} REMAINING`
  )
}

// ── Formatting ───────────────────────────────────────────────────────────

/** `0:41`, `12:05`, `1:02:09` -- or an em dash when nothing measured it. */
export function formatDuration(ms: number | undefined | null): string {
  if (ms == null || ms <= 0) return '—'
  const total = Math.round(ms / 1000)
  const h = Math.floor(total / 3600)
  const m = Math.floor((total % 3600) / 60)
  const s = String(total % 60).padStart(2, '0')
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${s}` : `${m}:${s}`
}

export function formatConfidence(c: number): string {
  return c.toFixed(2)
}

/** The proposal cell's steel footer: `SCRIBE · GEMMA4:E4B · GEN 2`. The
 *  proposer is the server's own runner-qualified string, with the runner
 *  prefix dropped -- which model proposed is the fact a person reads. */
export function proposerLabel(item: BatchItem): string {
  const model = item.proposer.includes('/') ? item.proposer.slice(item.proposer.indexOf('/') + 1) : item.proposer
  const parts = ['SCRIBE']
  if (model) parts.push(model.toUpperCase())
  if (item.generation != null) parts.push(`GEN ${item.generation}`)
  return parts.join(' · ')
}

/** `DISC` in the tag, as the board abbreviates it; the rest are themselves. */
export function destinationTag(dest: string): string {
  return dest === 'DISCUSSION' ? 'DISC' : dest
}

/** The proposal cell's second line. The model's own sentence first; a note's
 *  nearest page only when there is no reason to show. */
export function reasonLine(p: Proposal): string {
  if (p.reason.trim()) return p.reason
  return p.nearest_page ? `Nearest existing page is ${p.nearest_page.split('/').join(' / ')}` : ''
}

/** Whole minutes left in a discard's undo window, never below 1 while it is
 *  still open -- "0 MIN" would read as already gone. */
export function undoMinutesLeft(dueMs: number, nowMs: number): number {
  return Math.max(1, Math.ceil((dueMs - nowMs) / 60000))
}

/** `CHRN-55` → the descriptor POST /references/resolve takes for a Switchyard
 *  ticket. Null for anything that is not `KEY-N`, which is then drawn as an
 *  unchecked key rather than sent and refused. */
export function switchyardDescriptor(
  ticketKey: string,
): { system: 'switchyard'; token: string; key: string; number: number } | null {
  const m = /^([A-Z][A-Z0-9]*)-(\d+)$/.exec(ticketKey)
  if (!m) return null
  return { system: 'switchyard', token: ticketKey, key: m[1], number: Number(m[2]) }
}
