import { describe, expect, it } from 'vitest'
import {
  acceptDecision,
  applyResult,
  buildOverride,
  countRows,
  deferredRow,
  discardDecision,
  draftFor,
  editDecision,
  footerCounts,
  formatDuration,
  headerCounts,
  isDecidable,
  isPrefilled,
  mergeBatch,
  normalisePagePath,
  proposerLabel,
  rowKind,
  switchyardDescriptor,
  undoMinutesLeft,
  validateDraft,
  type BatchItem,
  type LocalState,
  type Proposal,
  type TriageRow,
} from './triage'
import { formatTriageWaiting, triageScopeLabel } from './triageCount'

// CHRN-55. The screen's rules, checked against the shapes the live corpus
// actually sends (CHRN-126's rollout comment): every NOTE arrives
// `needs_input` with its page cleared, and some TICKETs arrive with an empty
// project_key.

let n = 0
function proposal(overrides: Partial<Proposal> = {}): Proposal {
  return {
    generated: { tier: 1, source: 'chronicle', regenerable: true, notice: 'Regenerated.' } as Proposal['generated'],
    destination: 'NOTE',
    confidence: 0.9,
    reason: 'Names a decision already made',
    nearest_page: null,
    title: 'Shard sizing for index rebuilds',
    body: 'Two gigabytes.',
    verb: 'create',
    page_path: 'estate/storage',
    ...overrides,
  }
}

function item(overrides: Partial<BatchItem> = {}): BatchItem {
  n++
  return {
    memo_id: `00000000-0000-4000-8000-${String(n).padStart(12, '0')}`,
    captured_at: '2026-10-03T20:16:00-05:00',
    duration_ms: 64000,
    excerpt: 'Shard sizing — eight gigabytes was too big.',
    proposer: 'ollama/gemma4:e4b',
    generation: 2,
    status: 'valid',
    pre_acceptable: true,
    proposal: proposal(),
    ...overrides,
  }
}

function row(overrides: Partial<BatchItem> = {}, local: LocalState = { kind: 'pending' }): TriageRow {
  return { item: item(overrides), local }
}

describe('rowKind', () => {
  it('a valid proposal is pre-filled and decidable', () => {
    const r = row()
    expect(rowKind(r)).toBe('prefilled')
    expect(isDecidable(r)).toBe(true)
    expect(isPrefilled(r)).toBe(true)
  })

  it('needs_input, invalid and absent all need the editor', () => {
    expect(rowKind(row({ status: 'needs_input', pre_acceptable: false }))).toBe('needs-input')
    expect(rowKind(row({ status: 'invalid', proposal: undefined, error: 'no speech detected' }))).toBe('needs-input')
    expect(rowKind(row({ status: 'absent', proposal: undefined, generation: null }))).toBe('needs-input')
  })

  it('a proposed DISCARD is never accept-as-shown', () => {
    const r = row({ proposal: proposal({ destination: 'DISCARD', title: undefined }), pre_acceptable: false })
    expect(rowKind(r)).toBe('discard-proposed')
    expect(isPrefilled(r)).toBe(false)
  })

  it('a recorded decision outranks the proposal, except a refusal', () => {
    const link = { destination: 'TICKET', decided_at: '2026-10-03T21:06:00-05:00' }
    expect(rowKind(row({ link: { ...link, state: 'unresolved' } }))).toBe('link-unresolved')
    expect(rowKind(row({ link: { ...link, state: 'ambiguous', candidate_keys: ['SWY-1', 'SWY-2'] } }))).toBe('link-ambiguous')
    expect(rowKind(row({ link: { ...link, state: 'in_flight' } }))).toBe('link-in-flight')
    expect(isDecidable(row({ link: { ...link, state: 'unresolved' } }))).toBe(false)
    // Switchyard caches a refusal: the remedy is a changed decision.
    expect(isDecidable(row({ link: { ...link, state: 'refused' } }))).toBe(true)
  })

  it('a transient failure retries as shown; a refusal needs the editor', () => {
    expect(rowKind(row({}, { kind: 'problem', status: 'failed', reason: 'x' }))).toBe('prefilled')
    expect(rowKind(row({}, { kind: 'problem', status: 'refused', reason: 'x' }))).toBe('needs-input')
    const failedEdit: TriageRow = { ...row({}, { kind: 'problem', status: 'failed', reason: 'x' }), draft: draftFor(item()) }
    expect(rowKind(failedEdit)).toBe('needs-input')
  })

  it('ACCEPT ALL never sweeps up a row the server already answered about', () => {
    expect(isPrefilled(row({}, { kind: 'problem', status: 'failed', reason: 'x' }))).toBe(false)
    expect(isPrefilled(row({ pre_acceptable: false }))).toBe(false)
  })
})

describe('decisions', () => {
  it('accept echoes the proposer and the generation, and nothing else', () => {
    const it0 = item()
    expect(acceptDecision(it0, true)).toEqual({ memo_id: it0.memo_id, proposer: it0.proposer, generation: 2 })
  })

  it('a null generation is echoed as null -- it is an assertion, not a blank', () => {
    expect(acceptDecision(item({ generation: null }), true).generation).toBeNull()
  })

  it('confirm_edit is set for an append or supersede accepted on its own, never by the batch path', () => {
    const it0 = item({ proposal: proposal({ verb: 'append', target_note: 'CHR-0311' }) })
    expect(acceptDecision(it0, true).confirm_edit).toBe(true)
    expect(acceptDecision(it0, false).confirm_edit).toBeUndefined()
    // ...and never on a create, where the server refuses it.
    expect(acceptDecision(item(), true).confirm_edit).toBeUndefined()
  })

  it('a discard is an override carrying only its destination', () => {
    expect(discardDecision(item()).override).toEqual({ destination: 'DISCARD' })
  })
})

describe('the editor draft', () => {
  it('starts from the proposal, and from nearest_page when the page was cleared', () => {
    const d = draftFor(item({ proposal: proposal({ page_path: undefined, nearest_page: 'estate/storage/amber' }) }))
    expect(d.pagePath).toBe('estate/storage/amber')
    expect(d.text).toBe('Two gigabytes.')
  })

  it('falls back to the excerpt when the Scribe drafted nothing', () => {
    const d = draftFor(item({ status: 'invalid', proposal: undefined }))
    expect(d.destination).toBe('NOTE')
    expect(d.text).toBe('Shard sizing — eight gigabytes was too big.')
  })

  it('the live NOTE shape -- page cleared, nothing near -- is refused until a page is named', () => {
    const d = draftFor(item({ status: 'needs_input', proposal: proposal({ page_path: undefined }) }))
    expect(validateDraft(d)).toMatch(/page/)
    expect(validateDraft({ ...d, pagePath: 'estate' })).toBeNull()
  })

  it('the live TICKET shape -- no project chosen -- is refused until one is', () => {
    const d = draftFor(
      item({ status: 'needs_input', proposal: proposal({ destination: 'TICKET', project_key: '', ticket_type: 'task', description: 'Do it.' }) }),
    )
    expect(validateDraft(d)).toMatch(/project/)
    expect(validateDraft({ ...d, projectKey: 'chrn' })).toBeNull()
  })

  it('an override is whole, not a patch: every field its destination requires', () => {
    const base = draftFor(item())
    expect(buildOverride({ ...base, pagePath: ' estate / storage / amber ' })).toEqual({
      destination: 'NOTE',
      title: 'Shard sizing for index rebuilds',
      verb: 'create',
      body: 'Two gigabytes.',
      page_path: 'estate/storage/amber',
    })
    expect(buildOverride({ ...base, destination: 'TICKET', projectKey: 'chrn', ticketType: 'bug' })).toEqual({
      destination: 'TICKET',
      title: 'Shard sizing for index rebuilds',
      project_key: 'CHRN',
      ticket_type: 'bug',
      description: 'Two gigabytes.',
    })
    expect(buildOverride({ ...base, destination: 'DISCUSSION' })).toEqual({
      destination: 'DISCUSSION',
      title: 'Shard sizing for index rebuilds',
      opening_post: 'Two gigabytes.',
    })
    expect(buildOverride({ ...base, destination: 'DISCARD' })).toEqual({ destination: 'DISCARD' })
  })

  it('a verb that acts on an existing note carries the note, and create never does', () => {
    const base = draftFor(item({ proposal: proposal({ verb: 'append', target_note: 'CHR-0311' }) }))
    expect(buildOverride(base).target_note).toBe('CHR-0311')
    expect(validateDraft({ ...base, targetNote: '' })).toMatch(/note reference/)
    expect(buildOverride({ ...base, verb: 'create' }).target_note).toBeUndefined()
  })

  it('an edit never sets confirm_edit -- the override is the deliberate act', () => {
    const it0 = item()
    expect(editDecision(it0, draftFor(it0)).confirm_edit).toBeUndefined()
  })

  it('normalises a path typed the way the board draws it', () => {
    expect(normalisePagePath('estate / storage/ amber/')).toBe('estate/storage/amber')
  })
})

describe('applyResult', () => {
  const at = '2026-10-03T21:06:00Z'

  it('only `applied` takes the row out of the batch', () => {
    const r = row()
    const done = applyResult(r, { memo_id: r.item.memo_id, status: 'applied', note_ref: 'CHR-0412' }, false, at)
    expect(rowKind(done)).toBe('accepted')
    for (const status of ['needs_input', 'stale', 'refused', 'failed']) {
      const still = applyResult(r, { memo_id: r.item.memo_id, status, reason: 'why' }, false, at)
      expect(still.local.kind).toBe('problem')
      expect(isDecidable(still)).toBe(true)
    }
  })

  it('needs_input carries the post-bump generation and what was cleared', () => {
    const r = row()
    const cleared = [{ field: 'project_key', value: 'ENGR', reason: 'no such live Switchyard project' }]
    const next = applyResult(r, { memo_id: r.item.memo_id, status: 'needs_input', generation: 3, cleared }, false, at)
    expect(next.item.generation).toBe(3)
    expect(next.item.cleared_fields).toEqual(cleared)
    expect(next.item.pre_acceptable).toBe(false)
    expect(rowKind(next)).toBe('needs-input')
  })

  it('an unknown status is drawn as a failure, never as a success', () => {
    const r = row()
    expect(applyResult(r, { memo_id: r.item.memo_id, status: 'something_new' }, false, at).local.kind).toBe('problem')
  })
})

describe('mergeBatch', () => {
  it('keeps what this session decided and appends new memos', () => {
    const accepted = row({}, { kind: 'accepted', edited: false, result: { memo_id: 'x', status: 'applied' }, at: 'x' })
    const waiting = row()
    const fresh = item()
    const out = mergeBatch([accepted, waiting], [waiting.item, fresh])
    expect(out.map((r) => r.item.memo_id)).toEqual([accepted.item.memo_id, waiting.item.memo_id, fresh.memo_id])
    expect(out[0].local.kind).toBe('accepted')
  })

  it('a stale row takes the new proposal and says it changed', () => {
    const r = row({}, { kind: 'problem', status: 'stale', reason: 'changed' })
    const out = mergeBatch([r], [{ ...r.item, generation: 3 }])
    expect(out[0].item.generation).toBe(3)
    expect(out[0].local.kind).toBe('pending')
    expect(out[0].notice).toMatch(/CHANGED/)
  })

  it('a refusal outlives a re-read of an unchanged proposal', () => {
    const r = row({}, { kind: 'problem', status: 'refused', reason: 'no' })
    expect(mergeBatch([r], [r.item])[0].local.kind).toBe('problem')
  })

  it('a waiting row the server no longer lists was decided elsewhere, and leaves', () => {
    expect(mergeBatch([row()], [])).toEqual([])
  })

  it('a discard inside its undo window survives a re-read', () => {
    const r = row({}, { kind: 'discarding', dueMs: 1 })
    expect(mergeBatch([r], [r.item])[0].local.kind).toBe('discarding')
  })
})

describe('counts', () => {
  it('counts the header and footer from the rows, and a failed row stays in REMAINING', () => {
    const link = { destination: 'TICKET', decided_at: '2026-10-03T21:06:00-05:00', state: 'unresolved' }
    const rows: TriageRow[] = [
      row(),
      row(),
      row({ status: 'needs_input', pre_acceptable: false }),
      row({}, { kind: 'problem', status: 'failed', reason: 'x' }),
      row({ link }),
      row({}, { kind: 'accepted', edited: false, result: { memo_id: 'x', status: 'applied' }, at: 'x' }),
      row({}, { kind: 'accepted', edited: true, result: { memo_id: 'x', status: 'applied' }, at: 'x' }),
      row({}, { kind: 'held', reason: '' }),
      row({}, { kind: 'discarding', dueMs: 1 }),
      deferredRow({ memo_id: 'd', captured_at: 'x', held_by: 'u', held_at: 'x', age_seconds: 1 }),
    ]
    const c = countRows(rows)
    expect(headerCounts(c, false)).toBe('5 MEMOS · 2 PRE-FILLED · 1 NEED INPUT · 2 FAILED')
    expect(footerCounts(c, false)).toBe('1 ACCEPTED · 1 EDITED · 1 HELD · 1 DISCARDED · 5 REMAINING')
  })

  it('a full batch is "at least", here and on the badge', () => {
    const c = countRows([row()])
    expect(headerCounts(c, true)).toMatch(/^1\+ MEMOS/)
    expect(footerCounts(c, true)).toMatch(/1\+ REMAINING$/)
    expect(formatTriageWaiting({ count: 25, more: true })).toBe('25+')
    expect(formatTriageWaiting({ count: 3, more: false })).toBe('3')
  })

  it('the scope is said in one wording', () => {
    expect(triageScopeLabel(true)).toBe('all memos awaiting a decision')
    expect(triageScopeLabel(false)).toBe('your memos awaiting a decision')
  })
})

describe('formatting', () => {
  it('formats a duration, and an em dash when nothing measured it', () => {
    expect(formatDuration(41000)).toBe('0:41')
    expect(formatDuration(138000)).toBe('2:18')
    expect(formatDuration(3729000)).toBe('1:02:09')
    expect(formatDuration(undefined)).toBe('—')
  })

  it('labels the proposer without its runner prefix', () => {
    expect(proposerLabel(item())).toBe('SCRIBE · GEMMA4:E4B · GEN 2')
    expect(proposerLabel(item({ generation: null }))).toBe('SCRIBE · GEMMA4:E4B')
  })

  it('never shows an open undo window as zero minutes', () => {
    expect(undoMinutesLeft(600_000, 0)).toBe(10)
    expect(undoMinutesLeft(600_000, 599_000)).toBe(1)
  })

  it('builds a Switchyard descriptor only from KEY-N', () => {
    expect(switchyardDescriptor('SWY-412')).toEqual({ system: 'switchyard', token: 'SWY-412', key: 'SWY', number: 412 })
    expect(switchyardDescriptor('not a key')).toBeNull()
  })
})
