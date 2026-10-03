<script setup lang="ts">
// `/app/triage` -- batch triage at a keyboard (CHRN-55, board 1e). The
// phone's B2 is for accepting; this is the same batch at a desk, for the
// memos that need work before they land.
//
// The rules are lib/triage.ts's and unit tested there; this file reads the
// batch, sends decisions, and draws rows. What it holds to:
//
//   - THE PROPOSAL IS THE ONLY STEEL ON THE SCREEN. It is generated (tier 1,
//     CLAUDE.md invariant 1) and a person's decision beside it is vellum.
//     Coral appears only on a Switchyard ticket that exists (invariant 2).
//   - A ROW LEAVES THE BATCH ONLY ON `applied`. The POST answers one status
//     per item and four of the five leave the memo undecided, so each is
//     drawn still pending with the server's own reason.
//   - A DISCARD IS HELD BACK, NOT RECALLED. `discarded` is terminal on the
//     server and there is no undo operation, so the board's `UNDO 10 MIN` is
//     honest only one way: the decision is not sent until the window closes
//     (or this screen is left, or the next batch is loaded). Close the tab
//     inside the window and the memo is simply still waiting -- the failure
//     is on the side of keeping what somebody said.
//   - THE BATCH IS ONE SCREEN. The server hands over 25 and no total, so the
//     next memos load when this screen's are decided, and a full batch counts
//     as "at least" (`25+`) here and on the sidebar badge.
import { computed, inject, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { RouterLink } from 'vue-router'
import { api } from '@/api/client'
import { currentUser } from '@/auth'
import TriageEditor from '@/components/TriageEditor.vue'
import { PAGE_PATHS_KEY } from '@/layouts/shellData'
import { formatClock } from '@/lib/format'
import { buildReferenceCard, type Resolution } from '@/lib/referenceCard'
import {
  DISCARD_UNDO_MS,
  acceptDecision,
  applyResult,
  countRows,
  deferredRow,
  destinationTag,
  discardDecision,
  draftFor,
  editDecision,
  failRow,
  footerCounts,
  formatConfidence,
  formatDuration,
  headerCounts,
  isDecidable,
  isPrefilled,
  mergeBatch,
  proposerLabel,
  reasonLine,
  rowKind,
  switchyardDescriptor,
  undoMinutesLeft,
  type DeferredItem,
  type Draft,
  type TriageDecision,
  type TriageRow,
} from '@/lib/triage'
import { setTriageWaiting, triageScopeLabel } from '@/lib/triageCount'

const pagePaths = inject(PAGE_PATHS_KEY, ref(null))

function errorMessage(err: unknown, fallback: string): string {
  if (err && typeof err === 'object' && 'message' in err && typeof (err as { message?: unknown }).message === 'string') {
    return (err as { message: string }).message
  }
  return fallback
}

// ── The batch ────────────────────────────────────────────────────────────
const rows = ref<TriageRow[]>([])
const more = ref(false)
const loading = ref(true)
const loadError = ref<string | null>(null)
const loaded = ref(false)

async function loadBatch(): Promise<void> {
  loading.value = true
  try {
    const res = await api.GET('/triage/batch')
    if (res.data) {
      rows.value = mergeBatch(rows.value, res.data.items)
      more.value = res.data.items.length >= res.data.limit
      loadError.value = null
      loaded.value = true
      ensureFocus()
      void resolveCandidates()
    } else {
      loadError.value = errorMessage(res.error, 'The triage batch could not be read.')
    }
  } catch {
    loadError.value = 'Chronicle could not be reached. Nothing was decided.'
  } finally {
    loading.value = false
  }
}

// ── Deferred: what has been parked, behind the header's DEFERRED · N ─────
const deferred = ref<DeferredItem[] | null>(null)
const deferredMore = ref(false)
const showDeferred = ref(false)

async function loadDeferred(): Promise<void> {
  try {
    const res = await api.GET('/triage/deferred')
    if (res.data) {
      deferred.value = res.data.items
      deferredMore.value = res.data.items.length >= res.data.limit
    }
  } catch {
    // No count is drawn rather than a wrong one.
  }
}

// Rows held in this session are already on screen as held rows; the list
// adds only the ones parked on an earlier evening.
const deferredRows = computed<TriageRow[]>(() => {
  if (!showDeferred.value || !deferred.value) return []
  const onScreen = new Set(rows.value.map((r) => r.item.memo_id))
  return deferred.value.filter((d) => !onScreen.has(d.memo_id)).map(deferredRow)
})

const deferredCount = computed(() => {
  if (!deferred.value) return null
  const listed = new Set(deferred.value.map((d) => d.memo_id))
  let n = deferred.value.length
  // Held or released here since the list was read.
  for (const r of rows.value) {
    if (r.local.kind === 'held' && !listed.has(r.item.memo_id)) n++
    if (r.local.kind !== 'held' && listed.has(r.item.memo_id)) n--
  }
  return n
})

const visible = computed<TriageRow[]>(() => [...rows.value, ...deferredRows.value])

// ── Counts, and the sidebar badge that shares them ───────────────────────
const counts = computed(() => countRows(rows.value))
const scopeLabel = computed(() => triageScopeLabel(currentUser.value?.is_owner ?? false).toUpperCase())

watch([counts, more, loaded], () => {
  if (loaded.value) setTriageWaiting(counts.value.waiting, more.value)
})

function updateRow(memoId: string, fn: (row: TriageRow) => TriageRow): void {
  rows.value = rows.value.map((r) => (r.item.memo_id === memoId ? fn(r) : r))
}

// ── Focus ────────────────────────────────────────────────────────────────
const focusId = ref<string | null>(null)
const editingId = ref<string | null>(null)
const listEl = ref<HTMLElement | null>(null)

const focused = computed(() => visible.value.find((r) => r.item.memo_id === focusId.value) ?? null)

/** After a read: onto the first row that wants a decision, unless the
 *  focus is already on one. A fresh screen of memos arriving under a focus
 *  parked on an accepted row would otherwise need a J before the first ⏎. */
function ensureFocus(): void {
  if (focused.value && isDecidable(focused.value)) return
  const next = visible.value.find(isDecidable) ?? focused.value ?? visible.value[0]
  focusId.value = next?.item.memo_id ?? null
}

function move(delta: number): void {
  const list = visible.value
  if (list.length === 0) return
  const at = list.findIndex((r) => r.item.memo_id === focusId.value)
  const next = Math.min(list.length - 1, Math.max(0, (at < 0 ? 0 : at) + delta))
  focusId.value = list[next].item.memo_id
}

/** After a decision, on to the next row that still wants one -- forty memos
 *  is forty keypresses only if nobody has to press J between them. */
function advanceFrom(memoId: string): void {
  if (focusId.value !== memoId) return
  const list = visible.value
  const at = list.findIndex((r) => r.item.memo_id === memoId)
  const next = list.slice(at + 1).find(isDecidable) ?? list.slice(0, Math.max(at, 0)).find(isDecidable)
  if (next) focusId.value = next.item.memo_id
}

watch(focusId, async (id) => {
  if (!id) return
  await nextTick()
  const el = listEl.value?.querySelector<HTMLElement>(`[data-memo="${id}"]`)
  el?.scrollIntoView?.({ block: 'nearest' })
})

// ── Sending decisions ────────────────────────────────────────────────────
interface Outgoing {
  memoId: string
  decision: TriageDecision
  edited: boolean
  discard: boolean
}

async function send(out: Outgoing[]): Promise<void> {
  if (out.length === 0) return
  for (const o of out) updateRow(o.memoId, (r) => ({ ...r, local: { kind: 'sending' } }))

  let anyStale = false
  try {
    const res = await api.POST('/triage/accept', { body: { items: out.map((o) => o.decision) } })
    if (res.data) {
      // One result per item, in request order, each carrying its memo_id --
      // matched by id so a short or reordered answer cannot land a status on
      // the wrong row.
      const byMemo = new Map(res.data.results.map((r) => [r.memo_id, r]))
      const now = new Date().toISOString()
      for (const o of out) {
        const result = byMemo.get(o.memoId)
        updateRow(o.memoId, (r) => {
          if (!result) return failRow(r, 'The server did not answer for this memo. It is still pending.')
          if (result.status === 'stale') anyStale = true
          if (o.discard && result.status === 'applied') return { ...r, notice: undefined, local: { kind: 'discarded' } }
          const next = applyResult(r, result, o.edited, now)
          return next.local.kind === 'accepted' ? { ...next, draft: undefined } : next
        })
      }
    } else {
      const reason = errorMessage(res.error, 'The decision was not accepted.')
      for (const o of out) updateRow(o.memoId, (r) => failRow(r, reason))
    }
  } catch {
    // A network failure says nothing about whether the server started the
    // work: an item that has started is detached and durable, and a replay
    // answers `applied` from the recorded decision. So: still pending, retry.
    for (const o of out) {
      updateRow(o.memoId, (r) => failRow(r, 'Chronicle could not be reached. Retry — a decision that did land answers as landed.'))
    }
  }

  // A stale item must be re-shown, not decided blind.
  if (anyStale) await loadBatch()
  else void refillIfDone()
}

function accept(row: TriageRow): void {
  const kind = rowKind(row)
  if (kind !== 'prefilled') return
  advanceFrom(row.item.memo_id)
  void send([{ memoId: row.item.memo_id, decision: acceptDecision(row.item, true), edited: false, discard: false }])
}

const prefilledRows = computed(() => rows.value.filter(isPrefilled))

function acceptAll(): void {
  const batch = prefilledRows.value
  if (batch.length === 0) return
  void send(
    batch.map((r) => ({
      memoId: r.item.memo_id,
      // NEVER `single`: confirm_edit is the per-item confirmation an append
      // or a supersede costs, and a batch key must not be able to set it.
      decision: acceptDecision(r.item, false),
      edited: false,
      discard: false,
    })),
  )
  const here = focused.value
  if (here && !isDecidable(here)) advanceFrom(here.item.memo_id)
}

// ── Edit ─────────────────────────────────────────────────────────────────
const editing = computed(() => rows.value.find((r) => r.item.memo_id === editingId.value) ?? null)

function startEdit(row: TriageRow): void {
  if (!isDecidable(row)) return
  focusId.value = row.item.memo_id
  editingId.value = row.item.memo_id
}

function cancelEdit(): void {
  editingId.value = null
}

function confirmEdit(draft: Draft): void {
  const row = editing.value
  if (!row) return
  editingId.value = null
  if (draft.destination === 'DISCARD') {
    discard(row)
    return
  }
  updateRow(row.item.memo_id, (r) => ({ ...r, draft }))
  advanceFrom(row.item.memo_id)
  void send([{ memoId: row.item.memo_id, decision: editDecision(row.item, draft), edited: true, discard: false }])
}

const projectKeys = computed(() => {
  const keys = new Set<string>()
  for (const r of rows.value) {
    const k = r.item.proposal?.project_key
    if (k) keys.add(k)
  }
  return [...keys].sort()
})

// ── Hold and release ─────────────────────────────────────────────────────
async function hold(row: TriageRow): Promise<void> {
  if (!isDecidable(row)) return
  const id = row.item.memo_id
  const before = row.local
  advanceFrom(id)
  updateRow(id, (r) => ({ ...r, local: { kind: 'sending' } }))
  try {
    const res = await api.POST('/triage/hold', { body: { memo_id: id } })
    if (res.data) {
      const reason = res.data.reason ?? ''
      updateRow(id, (r) => ({ ...r, local: { kind: 'held', reason } }))
      void refillIfDone()
      return
    }
    // Not a failed DECISION: the row goes back to what it was, and says so.
    const reason = errorMessage(res.error, 'the hold was not recorded')
    updateRow(id, (r) => ({ ...r, local: before, notice: `HOLD NOT RECORDED · ${reason}` }))
  } catch {
    updateRow(id, (r) => ({ ...r, local: before, notice: 'HOLD NOT RECORDED · CHRONICLE UNREACHABLE' }))
  }
}

async function release(row: TriageRow): Promise<void> {
  if (row.local.kind !== 'held') return
  const id = row.item.memo_id
  try {
    const res = await api.POST('/triage/release', { body: { memo_id: id } })
    if (res.error) {
      updateRow(id, (r) => ({ ...r, notice: `NOT RELEASED · ${errorMessage(res.error, 'still held')}` }))
      return
    }
  } catch {
    updateRow(id, (r) => ({ ...r, notice: 'NOT RELEASED · CHRONICLE UNREACHABLE' }))
    return
  }
  if (row.fromDeferred) {
    // It comes back with its proposal, which the deferred list never had.
    deferred.value = (deferred.value ?? []).filter((d) => d.memo_id !== id)
    await loadBatch()
    return
  }
  updateRow(id, (r) => ({ ...r, notice: undefined, local: { kind: 'pending' } }))
}

// ── Discard, with its undo window ────────────────────────────────────────
const now = ref(Date.now())

function discard(row: TriageRow): void {
  if (!isDecidable(row)) return
  advanceFrom(row.item.memo_id)
  // The label counts down from the clock the window was opened on.
  now.value = Date.now()
  const dueMs = now.value + DISCARD_UNDO_MS
  updateRow(row.item.memo_id, (r) => ({ ...r, notice: undefined, local: { kind: 'discarding', dueMs } }))
  void refillIfDone()
}

function undoDiscard(row: TriageRow): void {
  if (row.local.kind !== 'discarding') return
  updateRow(row.item.memo_id, (r) => ({ ...r, local: { kind: 'pending' } }))
}

/** Sends the discards whose window has closed -- or all of them. */
function flushDiscards(all: boolean): Promise<void> {
  const due = rows.value.filter((r) => r.local.kind === 'discarding' && (all || r.local.dueMs <= Date.now()))
  return send(due.map((r) => ({ memoId: r.item.memo_id, decision: discardDecision(r.item), edited: false, discard: true })))
}

// ── The next screen ──────────────────────────────────────────────────────
let refilling = false

/** When every row here is decided and the batch came back full, the next
 *  memos are waiting behind it. Open discards go first: they still occupy the
 *  server's 25, so the next batch would otherwise be the same one. */
async function refillIfDone(): Promise<void> {
  if (refilling || !more.value || rows.value.some(isDecidable) || rows.value.some((r) => r.local.kind === 'sending')) return
  refilling = true
  try {
    await flushDiscards(true)
    await loadBatch()
  } finally {
    refilling = false
  }
}

// ── Ambiguous links: the candidates, resolved live (invariant 2) ─────────
const resolutions = ref<Map<string, Resolution>>(new Map())

async function resolveCandidates(): Promise<void> {
  const keys = new Set<string>()
  for (const r of rows.value) {
    if (r.item.link?.state === 'ambiguous') for (const k of r.item.link.candidate_keys ?? []) keys.add(k)
  }
  const references = [...keys].map(switchyardDescriptor).filter((d) => d !== null)
  if (references.length === 0) return
  try {
    const res = await api.POST('/references/resolve', { body: { references: references.slice(0, 50) } })
    if (res.data) resolutions.value = new Map(res.data.resolutions.map((r) => [r.token, r]))
  } catch {
    // The keys are still drawn, as not checked.
  }
}

function candidateCard(key: string) {
  const d = switchyardDescriptor(key) ?? { system: 'switchyard' as const, token: key }
  return buildReferenceCard(d, resolutions.value.get(key), now.value)
}

// ── Keyboard: J/K ⏎ E H D A R esc ────────────────────────────────────────
function onKey(e: KeyboardEvent): void {
  if (e.metaKey || e.ctrlKey || e.altKey) return
  const t = e.target as HTMLElement | null
  const typing = !!t && (t.tagName === 'INPUT' || t.tagName === 'TEXTAREA' || t.tagName === 'SELECT' || t.isContentEditable)

  if (e.key === 'Escape') {
    if (editingId.value) {
      e.preventDefault()
      cancelEdit()
    }
    return
  }
  // While a row is being edited the keys belong to the form; a letter typed
  // into a title must never hold or discard the row underneath it.
  if (typing || editingId.value) return
  // ⏎ on a focused button or link is that control's own.
  if (e.key === 'Enter' && t && (t.tagName === 'BUTTON' || t.tagName === 'A')) return

  const row = focused.value
  const key = e.key.length === 1 ? e.key.toLowerCase() : e.key
  // A HELD KEY MOVES, IT NEVER DECIDES. Focus advances after every decision,
  // so an auto-repeating ⏎ would accept a run of proposals nobody looked at
  // -- notes and tickets that cannot be taken back. One press, one decision.
  const moving = key === 'j' || key === 'k' || key === 'ArrowDown' || key === 'ArrowUp'
  if (e.repeat && !moving) return
  switch (key) {
    case 'j':
    case 'ArrowDown':
      move(1)
      break
    case 'k':
    case 'ArrowUp':
      move(-1)
      break
    case 'a':
      acceptAll()
      break
    case 'Enter':
      if (!row) return
      if (rowKind(row) === 'prefilled') accept(row)
      else if (rowKind(row) === 'needs-input') startEdit(row)
      else return
      break
    case 'e':
      if (!row || !isDecidable(row)) return
      startEdit(row)
      break
    case 'h':
      if (!row || !isDecidable(row)) return
      void hold(row)
      break
    case 'd':
      if (!row || !isDecidable(row)) return
      discard(row)
      break
    case 'r':
      if (!row) return
      if (row.local.kind === 'held') void release(row)
      else if (row.local.kind === 'discarding') undoDiscard(row)
      else return
      break
    default:
      return
  }
  e.preventDefault()
}

function hintFor(row: TriageRow): string {
  switch (rowKind(row)) {
    case 'prefilled':
      return 'J/K MOVE · ⏎ ACCEPT · E EDIT · H HOLD · D DISCARD · A ACCEPT ALL'
    case 'needs-input':
      return 'J/K MOVE · ⏎ OR E EDIT · H HOLD · D DISCARD · A ACCEPT ALL'
    case 'discard-proposed':
      return 'J/K MOVE · D DISCARD · E EDIT · H HOLD · A ACCEPT ALL'
    case 'held':
      return 'J/K MOVE · R RELEASE · A ACCEPT ALL'
    case 'discarding':
      return 'J/K MOVE · R UNDO · A ACCEPT ALL'
    default:
      return 'J/K MOVE · A ACCEPT ALL'
  }
}

/** The grey left rule the board gives a decision that did not finish. */
function isStuck(row: TriageRow): boolean {
  const k = rowKind(row)
  if (k === 'link-unresolved' || k === 'link-ambiguous' || k === 'link-in-flight') return true
  if (row.item.link?.state === 'refused') return true
  return row.local.kind === 'problem' && (row.local.status === 'failed' || row.local.status === 'refused')
}

const PROBLEM_LABEL = {
  failed: 'FAILED · STILL PENDING',
  refused: 'REFUSED · STILL PENDING',
  needs_input: 'NEEDS INPUT · STILL PENDING',
  stale: 'PROPOSAL CHANGED · STILL PENDING',
} as const

let ticker: ReturnType<typeof setInterval> | undefined

onMounted(() => {
  window.addEventListener('keydown', onKey)
  void loadBatch()
  void loadDeferred()
  ticker = setInterval(() => {
    now.value = Date.now()
    void flushDiscards(false)
  }, 15_000)
})

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKey)
  if (ticker) clearInterval(ticker)
  // Leaving the screen ends the undo window: the row said DISCARDED, and
  // there is nowhere left to undo it from.
  void flushDiscards(true)
})
</script>

<template>
  <div class="ch-tri">
    <header class="ch-tri-head">
      <div class="ch-tri-head-row">
        <h2 class="ch-tri-title">Evening triage</h2>
        <span v-if="loaded" class="ch-tri-counts" :title="scopeLabel.toLowerCase()">{{ headerCounts(counts, more) }}</span>
        <span class="ch-tri-head-actions">
          <button
            v-if="deferredCount !== null"
            type="button"
            class="ch-tri-quiet"
            :aria-pressed="showDeferred"
            @click="showDeferred = !showDeferred"
          >
            DEFERRED · {{ deferredCount }}{{ deferredMore ? '+' : '' }}
          </button>
          <button type="button" class="ch-tri-primary ch-tri-primary--lg" :disabled="prefilledRows.length === 0" @click="acceptAll">
            ACCEPT ALL PRE-FILLED
          </button>
        </span>
      </div>
      <p v-if="loaded" class="ch-tri-sub">
        {{ scopeLabel }}.
        <template v-if="counts.waiting > 0">
          ACCEPT ALL TAKES THE {{ counts.prefilled }} THE SCRIBE IS CONFIDENT ABOUT. THE OTHER
          {{ counts.waiting - counts.prefilled }} STAY HERE UNTIL YOU DECIDE.
        </template>
        <template v-if="more"> THE NEXT MEMOS LOAD WHEN THESE ARE DECIDED.</template>
      </p>
    </header>

    <div class="ch-tri-cols ch-tri-grid">
      <div></div>
      <div class="ch-tri-col ch-tri-col--captured">CAPTURED</div>
      <div class="ch-tri-col">EXCERPT</div>
      <div class="ch-tri-col ch-tri-col--proposal">PROPOSAL</div>
      <div class="ch-tri-col ch-tri-col--decision">DECISION</div>
    </div>

    <p v-if="loadError" class="ch-tri-state" role="alert">{{ loadError }}</p>
    <p v-else-if="loading && !loaded" class="ch-tri-state">Reading the batch…</p>
    <p v-else-if="loaded && visible.length === 0" class="ch-tri-state">Nothing is waiting for a decision.</p>

    <div ref="listEl" class="ch-tri-list">
      <template v-for="row in visible" :key="row.item.memo_id">
        <div
          class="ch-tri-row ch-tri-grid"
          :class="[
            `is-${rowKind(row)}`,
            { 'is-focused': focusId === row.item.memo_id, 'is-stuck': isStuck(row), 'is-problem': row.local.kind === 'problem' },
          ]"
          :data-memo="row.item.memo_id"
          :aria-current="focusId === row.item.memo_id ? 'true' : undefined"
          @click="focusId = row.item.memo_id"
        >
          <div class="ch-tri-rule"></div>

          <div class="ch-tri-captured">
            {{ formatClock(row.item.captured_at) }} · {{ formatDuration(row.item.duration_ms) }}
          </div>

          <div class="ch-tri-excerpt">{{ row.item.excerpt || '…' }}</div>

          <!-- PROPOSAL: generated, and the only steel on the screen. -->
          <div class="ch-tri-proposal">
            <template v-if="rowKind(row) === 'discarding' || rowKind(row) === 'discarded'">
              <span class="ch-tri-tag ch-tri-tag--bare">DISCARD</span>
            </template>
            <template v-else-if="row.item.proposal">
              <div class="ch-tri-proposal-top">
                <span class="ch-tri-tag">{{ destinationTag(row.item.proposal.destination) }}</span>
                <span class="ch-tri-confidence">{{ formatConfidence(row.item.proposal.confidence) }}</span>
              </div>
              <div v-if="row.item.proposal.title" class="ch-tri-proposal-title">{{ row.item.proposal.title }}</div>
              <div v-if="reasonLine(row.item.proposal)" class="ch-tri-proposal-reason">{{ reasonLine(row.item.proposal) }}</div>
              <div
                v-if="row.item.proposal.destination === 'NOTE' && row.item.proposal.verb && row.item.proposal.verb !== 'create'"
                class="ch-tri-proposal-mono"
              >
                {{ row.item.proposal.verb.toUpperCase() }} → {{ row.item.proposal.target_note || 'NO NOTE NAMED' }}
              </div>
              <div
                v-else-if="row.item.proposal.destination === 'NOTE' && row.item.proposal.page_path"
                class="ch-tri-proposal-mono"
              >
                PAGE {{ row.item.proposal.page_path }}
              </div>
              <div v-for="c in row.item.cleared_fields ?? []" :key="c.field" class="ch-tri-proposal-mono">
                <template v-if="c.value">REMOVED {{ c.field }} = {{ c.value }} · {{ c.reason }}</template>
                <template v-else>MISSING {{ c.field }} · {{ c.reason }}</template>
              </div>
              <div
                v-if="
                  row.item.status === 'needs_input' &&
                  row.item.proposal.destination === 'TICKET' &&
                  !row.item.proposal.project_key &&
                  !(row.item.cleared_fields ?? []).some((c) => c.field === 'project_key')
                "
                class="ch-tri-proposal-mono"
              >
                MISSING project_key · the Scribe did not choose a project
              </div>
              <div class="ch-tri-proposal-foot">{{ proposerLabel(row.item) }}</div>
            </template>
            <template v-else-if="row.fromDeferred">
              <span class="ch-tri-proposal-none">Parked. Release it to see its proposal.</span>
            </template>
            <template v-else>
              <span class="ch-tri-proposal-none">
                No usable proposal: {{ row.item.error || 'the Scribe has not proposed for this memo yet' }}
              </span>
            </template>
            <div v-if="row.notice" class="ch-tri-proposal-mono">{{ row.notice }}</div>
          </div>

          <!-- DECISION: what a person does. Vellum. -->
          <div class="ch-tri-decision">
            <template v-if="rowKind(row) === 'sending'">
              <div class="ch-tri-status">SENDING…</div>
            </template>

            <template v-else-if="row.local.kind === 'accepted'">
              <div class="ch-tri-status">
                ACCEPTED {{ formatClock(row.local.at) }}{{ row.local.edited ? ' · EDITED' : '' }}
              </div>
              <div class="ch-tri-landed">
                <RouterLink v-if="row.local.result.note_ref" :to="`/notes/${row.local.result.note_ref}`" class="ch-tri-landed-ref">
                  {{ row.local.result.note_ref }}
                </RouterLink>
                <RouterLink
                  v-else-if="row.local.result.discussion_ref"
                  :to="`/discussions/${row.local.result.discussion_ref}`"
                  class="ch-tri-landed-ref"
                >
                  {{ row.local.result.discussion_ref }}
                </RouterLink>
                <a
                  v-else-if="row.local.result.ticket_key"
                  :href="row.local.result.ticket_url || undefined"
                  target="_blank"
                  rel="noopener noreferrer"
                  class="ch-tri-ticket"
                >
                  <span class="ch-tri-ticket-key">{{ row.local.result.ticket_key }}</span>
                  <span class="ch-tri-ticket-arrow">↗</span>
                </a>
              </div>
            </template>

            <template v-else-if="row.local.kind === 'held'">
              <div class="ch-tri-status">HELD · "{{ row.local.reason || 'not now' }}"</div>
              <button type="button" class="ch-tri-quiet ch-tri-quiet--block" tabindex="-1" @click.stop="release(row)">RELEASE R</button>
            </template>

            <template v-else-if="row.local.kind === 'discarding'">
              <button type="button" class="ch-tri-quiet" tabindex="-1" @click.stop="undoDiscard(row)">
                DISCARDED · UNDO R · {{ undoMinutesLeft(row.local.dueMs, now) }} MIN
              </button>
            </template>

            <template v-else-if="row.local.kind === 'discarded'">
              <div class="ch-tri-status ch-tri-status--meta">DISCARDED</div>
            </template>

            <template v-else-if="rowKind(row) === 'link-in-flight' && row.item.link">
              <div class="ch-tri-status">ACCEPTED {{ formatClock(row.item.link.decided_at) }} · {{ row.item.link.destination }} IN FLIGHT</div>
              <div class="ch-tri-explain">The decision is recorded and is landing now. Nothing to redo.</div>
            </template>

            <template v-else-if="rowKind(row) === 'link-unresolved' && row.item.link">
              <div class="ch-tri-status">
                ACCEPTED {{ formatClock(row.item.link.decided_at) }} · {{ row.item.link.destination }} UNRESOLVED · SWEEP RETRIES
              </div>
              <div class="ch-tri-explain">The decision is recorded. The ticket does not exist yet. Nothing to redo.</div>
            </template>

            <template v-else-if="rowKind(row) === 'link-ambiguous' && row.item.link">
              <div class="ch-tri-status">{{ (row.item.link.candidate_keys ?? []).length }} TICKETS CARRY THIS MEMO · PICK ONE</div>
              <div class="ch-tri-candidates">
                <template v-for="key in row.item.link.candidate_keys ?? []" :key="key">
                  <a
                    :href="candidateCard(key).url || undefined"
                    target="_blank"
                    rel="noopener noreferrer"
                    class="ch-tri-ticket"
                    :title="candidateCard(key).title"
                  >
                    <span class="ch-tri-ticket-key">{{ key }}</span>
                    <span v-if="candidateCard(key).stateWord" class="ch-tri-ticket-state">{{ candidateCard(key).stateWord }}</span>
                    <span class="ch-tri-ticket-arrow">↗</span>
                  </a>
                </template>
              </div>
              <div class="ch-tri-explain">Take this memo off one of them in Switchyard; the sweep then links the other.</div>
            </template>

            <template v-else>
              <template v-if="row.local.kind === 'problem'">
                <div class="ch-tri-status">{{ PROBLEM_LABEL[row.local.status] }}</div>
                <div class="ch-tri-explain ch-tri-explain--gap">{{ row.local.reason }}</div>
              </template>
              <template v-else-if="row.item.link?.state === 'refused'">
                <div class="ch-tri-status">
                  REFUSED{{ row.item.link.refused_status ? ` ${row.item.link.refused_status}` : '' }} · STILL PENDING
                </div>
                <div class="ch-tri-explain ch-tri-explain--gap">
                  {{ row.item.link.refused_reason || 'Switchyard refused this decision.' }} Change it — the same decision is refused the same way.
                </div>
              </template>

              <template v-if="editingId === row.item.memo_id">
                <div class="ch-tri-editing">EDITING</div>
                <div class="ch-tri-key">ESC TO CANCEL</div>
              </template>
              <template v-else-if="rowKind(row) === 'prefilled'">
                <button type="button" class="ch-tri-primary ch-tri-primary--fill" tabindex="-1" @click.stop="accept(row)">
                  {{ row.local.kind === 'problem' ? 'RETRY' : 'ACCEPT' }}
                </button>
                <div class="ch-tri-key">⏎</div>
                <div class="ch-tri-secondary">
                  <button type="button" tabindex="-1" @click.stop="startEdit(row)">EDIT E</button>
                  <button type="button" tabindex="-1" @click.stop="hold(row)">HOLD H</button>
                  <button type="button" tabindex="-1" @click.stop="discard(row)">DISCARD D</button>
                </div>
              </template>
              <template v-else-if="rowKind(row) === 'discard-proposed'">
                <button type="button" class="ch-tri-primary ch-tri-primary--fill" tabindex="-1" @click.stop="discard(row)">DISCARD</button>
                <div class="ch-tri-key">D</div>
                <div class="ch-tri-secondary">
                  <button type="button" tabindex="-1" @click.stop="startEdit(row)">EDIT E</button>
                  <button type="button" tabindex="-1" @click.stop="hold(row)">HOLD H</button>
                </div>
              </template>
              <template v-else>
                <button type="button" class="ch-tri-primary ch-tri-primary--fill" tabindex="-1" @click.stop="startEdit(row)">EDIT</button>
                <div class="ch-tri-key">E</div>
                <div class="ch-tri-secondary">
                  <button type="button" tabindex="-1" @click.stop="hold(row)">HOLD H</button>
                  <button type="button" tabindex="-1" @click.stop="discard(row)">DISCARD D</button>
                </div>
              </template>
            </template>
          </div>
        </div>

        <div v-if="editingId === row.item.memo_id" class="ch-tri-under is-focused">
          <div class="ch-tri-rule"></div>
          <div class="ch-tri-under-body ch-tri-under-body--editor">
            <TriageEditor
              :memo-id="row.item.memo_id"
              :initial="row.draft ?? draftFor(row.item)"
              :page-paths="pagePaths ?? []"
              :project-keys="projectKeys"
              :show-verb="row.item.proposal?.destination === 'NOTE' && !!row.item.proposal.verb && row.item.proposal.verb !== 'create'"
              :server-error="row.local.kind === 'problem' ? row.local.reason : null"
              @confirm="confirmEdit"
              @cancel="cancelEdit"
            />
          </div>
        </div>
        <div v-else-if="focusId === row.item.memo_id" class="ch-tri-under is-focused">
          <div class="ch-tri-rule"></div>
          <div class="ch-tri-under-body ch-tri-hint">{{ hintFor(row) }}</div>
        </div>
      </template>
    </div>

    <footer class="ch-tri-foot">
      <span class="ch-tri-foot-counts">{{ loaded ? footerCounts(counts, more) : '' }}</span>
      <span class="ch-tri-foot-actions">
        <span class="ch-tri-foot-prefilled">{{ prefilledRows.length }} REMAINING PRE-FILLED</span>
        <button type="button" class="ch-tri-primary" :disabled="prefilledRows.length === 0" @click="acceptAll">
          ACCEPT ALL PRE-FILLED
        </button>
      </span>
    </footer>
  </div>
</template>

<style scoped>
.ch-tri {
  min-height: 100vh;
  display: flex;
  flex-direction: column;
  font-family: var(--ch-font-sans);
  /* The proposal column's rule: steel at the board's 28%, from the token
   * rather than a second literal for the same colour. */
  --ch-tri-steel-rule: color-mix(in srgb, var(--ch-generated) 28%, transparent);
}

/* ── Header ─────────────────────────────────────────────────────────── */
.ch-tri-head {
  padding: 26px 30px 18px;
  border-bottom: 1px solid var(--ch-line);
}

.ch-tri-head-row {
  display: flex;
  align-items: center;
  gap: 16px;
}

.ch-tri-title {
  margin: 0;
  font-family: var(--ch-font-serif);
  font-size: 30px;
  font-weight: 400;
  color: var(--ch-text);
  white-space: nowrap;
}

.ch-tri-counts {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  letter-spacing: 0.11em;
  color: var(--ch-text-meta);
}

.ch-tri-head-actions {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 18px;
  flex: none;
}

.ch-tri-sub {
  margin: 12px 0 0;
  font-family: var(--ch-font-mono);
  font-size: 10px;
  letter-spacing: 0.09em;
  color: var(--ch-text-meta);
}

/* ── Buttons ────────────────────────────────────────────────────────── */
.ch-tri-primary {
  height: 34px;
  padding: 0 16px;
  border: 0;
  border-radius: 0;
  background: var(--ch-signal);
  color: var(--ch-base);
  cursor: pointer;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  font-weight: 600;
  letter-spacing: 0.12em;
  white-space: nowrap;
}

.ch-tri-primary--lg {
  height: 36px;
}

.ch-tri-primary--fill {
  display: block;
  width: 100%;
}

.ch-tri-primary:disabled {
  background: none;
  border: 1px solid var(--ch-line);
  color: var(--ch-text-meta);
  cursor: default;
}

.ch-tri-primary:focus-visible {
  outline: 1px solid var(--ch-text);
  outline-offset: 2px;
}

.ch-tri-quiet {
  border: 0;
  background: none;
  padding: 0;
  cursor: pointer;
  text-align: left;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  letter-spacing: 0.11em;
  color: var(--ch-text-2);
}

.ch-tri-quiet--block {
  display: block;
  margin-top: 9px;
}

.ch-tri-quiet:hover,
.ch-tri-quiet:focus-visible,
.ch-tri-quiet[aria-pressed='true'] {
  color: var(--ch-signal);
  outline: none;
}

/* ── The table ──────────────────────────────────────────────────────── */
.ch-tri-grid {
  display: grid;
  grid-template-columns: 3px 116px minmax(0, 1fr) 356px 212px;
}

.ch-tri-cols {
  position: sticky;
  top: 0;
  z-index: 1;
  background: var(--ch-base);
  border-bottom: 1px solid var(--ch-line);
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-2xs);
  letter-spacing: 0.14em;
  color: var(--ch-text-meta);
}

.ch-tri-col {
  padding: 9px 14px;
}

.ch-tri-col--captured {
  padding: 9px 0 9px 14px;
}

.ch-tri-col--proposal {
  padding: 9px 18px;
  border-left: 1px solid var(--ch-tri-steel-rule);
  color: var(--ch-generated);
}

.ch-tri-col--decision {
  padding: 9px 16px;
  border-left: 1px solid var(--ch-line);
}

.ch-tri-state {
  margin: 0;
  padding: 28px 30px;
  font-size: var(--ch-size-body);
  color: var(--ch-text-2);
}

.ch-tri-list {
  flex: 1;
}

.ch-tri-row {
  border-bottom: 1px solid var(--ch-line);
  /* Clear of the sticky column header and footer when J/K scrolls to it. */
  scroll-margin: 40px 0 120px;
}

.ch-tri-row.is-focused,
.ch-tri-under.is-focused {
  background: var(--ch-raised);
}

/* The strip underneath continues the focused row: no line between them, and
 * the vellum rule runs unbroken down both. */
.ch-tri-row.is-focused {
  border-bottom: 0;
}

.ch-tri-row.is-stuck .ch-tri-rule {
  background: var(--ch-text-meta);
}

.ch-tri-row.is-focused .ch-tri-rule,
.ch-tri-under .ch-tri-rule {
  background: var(--ch-signal);
}

.ch-tri-row.is-held {
  opacity: 0.5;
}

.ch-tri-row.is-held.is-focused {
  opacity: 0.8;
}

.ch-tri-captured {
  padding: 16px 0 16px 14px;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  color: var(--ch-text-meta);
  white-space: nowrap;
}

.ch-tri-row.is-focused .ch-tri-captured {
  color: var(--ch-text-2);
}

.ch-tri-excerpt {
  padding: 16px 18px 16px 14px;
  font-size: 13.5px;
  line-height: 1.5;
  color: var(--ch-text);
  text-wrap: pretty;
  overflow-wrap: anywhere;
}

.ch-tri-row.is-held .ch-tri-excerpt {
  color: var(--ch-text-2);
}

.ch-tri-row.is-discarding .ch-tri-excerpt,
.ch-tri-row.is-discarded .ch-tri-excerpt {
  color: var(--ch-text-meta);
  text-decoration: line-through;
}

/* ── Proposal cell ──────────────────────────────────────────────────── */
.ch-tri-proposal {
  padding: 14px 18px;
  border-left: 1px solid var(--ch-tri-steel-rule);
  min-width: 0;
}

.ch-tri-proposal-top {
  display: flex;
  align-items: center;
  gap: 10px;
}

.ch-tri-tag {
  font-family: var(--ch-font-mono);
  font-size: 10px;
  letter-spacing: 0.12em;
  color: var(--ch-text);
  border: 1px solid var(--ch-line);
  padding: 2px 7px;
}

.ch-tri-tag--bare {
  border: 0;
  padding: 0;
  color: var(--ch-text-meta);
}

.ch-tri-row.is-held .ch-tri-tag {
  color: var(--ch-text-2);
}

.ch-tri-confidence {
  margin-left: auto;
  font-family: var(--ch-font-mono);
  font-size: 10px;
  color: var(--ch-generated);
}

.ch-tri-proposal-title {
  margin-top: 8px;
  font-size: var(--ch-size-body);
  line-height: 1.45;
  color: var(--ch-text);
  overflow-wrap: anywhere;
}

.ch-tri-proposal-reason {
  margin-top: 6px;
  font-size: 12px;
  line-height: 1.45;
  color: var(--ch-text-2);
}

.ch-tri-proposal-mono {
  margin-top: 10px;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-2xs);
  letter-spacing: 0.1em;
  line-height: 1.6;
  color: var(--ch-text-2);
  overflow-wrap: anywhere;
}

.ch-tri-proposal-foot {
  margin-top: 10px;
  font-family: var(--ch-font-mono);
  font-size: 9px;
  letter-spacing: 0.11em;
  color: var(--ch-generated);
}

.ch-tri-proposal-none {
  font-size: 12.5px;
  line-height: 1.5;
  color: var(--ch-text-2);
}

/* ── Decision cell ──────────────────────────────────────────────────── */
.ch-tri-decision {
  padding: 14px 16px;
  border-left: 1px solid var(--ch-line);
  min-width: 0;
}

.ch-tri-key {
  margin-top: 6px;
  text-align: center;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-2xs);
  color: var(--ch-text-meta);
}

.ch-tri-row.is-focused .ch-tri-key {
  color: var(--ch-text-2);
}

.ch-tri-secondary {
  margin-top: 8px;
  display: flex;
  gap: 12px;
  justify-content: center;
}

.ch-tri-secondary button {
  border: 0;
  background: none;
  padding: 0;
  cursor: pointer;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-2xs);
  letter-spacing: 0.08em;
  color: var(--ch-text-2);
  white-space: nowrap;
}

.ch-tri-row.is-focused .ch-tri-secondary button,
.ch-tri-secondary button:hover {
  color: var(--ch-text);
}

.ch-tri-editing {
  height: 34px;
  border: 1px solid var(--ch-line);
  display: flex;
  align-items: center;
  justify-content: center;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  letter-spacing: 0.12em;
  color: var(--ch-signal);
}

.ch-tri-status {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-2xs);
  letter-spacing: 0.1em;
  line-height: 1.7;
  color: var(--ch-text-2);
}

.ch-tri-status--meta {
  color: var(--ch-text-meta);
}

.ch-tri-explain {
  margin-top: 8px;
  font-size: 12px;
  line-height: 1.5;
  color: var(--ch-text-meta);
  text-wrap: pretty;
  overflow-wrap: anywhere;
}

.ch-tri-explain--gap {
  margin-top: 4px;
  margin-bottom: 10px;
}

.ch-tri-landed {
  margin-top: 8px;
}

.ch-tri-landed-ref {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  color: var(--ch-signal);
  text-decoration: none;
}

.ch-tri-landed-ref:hover {
  color: var(--ch-text);
}

.ch-tri-candidates {
  margin-top: 9px;
  display: flex;
  flex-direction: column;
  gap: 6px;
}

/* A Switchyard ticket that exists: the one place this screen is coral
 * (CLAUDE.md invariant 2 -- the reserved token, with its outbound arrow). */
.ch-tri-ticket {
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 7px 9px;
  background: var(--ch-raised);
  border-left: 2px solid var(--ch-ref-switchyard);
  text-decoration: none;
}

.ch-tri-row.is-focused .ch-tri-ticket {
  background: var(--ch-base);
}

.ch-tri-ticket-key {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  color: var(--ch-ref-switchyard);
}

.ch-tri-ticket-state {
  font-family: var(--ch-font-mono);
  font-size: 9px;
  letter-spacing: 0.08em;
  color: var(--ch-text-2);
  text-transform: uppercase;
}

.ch-tri-ticket-arrow {
  margin-left: auto;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-2xs);
  color: var(--ch-text-meta);
}

/* ── The strip under the focused row: the key hint, or the editor ───── */
.ch-tri-under {
  display: grid;
  grid-template-columns: 3px 1fr;
  border-bottom: 1px solid var(--ch-line);
}

.ch-tri-under-body {
  padding: 6px 30px 14px 130px;
}

.ch-tri-under-body--editor {
  padding: 4px 30px 22px 130px;
}

.ch-tri-hint {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-2xs);
  letter-spacing: 0.11em;
  color: var(--ch-text-meta);
}

/* ── Footer, fixed to the bottom of the column ──────────────────────── */
.ch-tri-foot {
  position: sticky;
  bottom: 0;
  display: flex;
  align-items: center;
  gap: 18px;
  padding: 14px 30px;
  border-top: 1px solid var(--ch-line);
  background: var(--ch-raised);
}

.ch-tri-foot-counts {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  letter-spacing: 0.1em;
  color: var(--ch-text-2);
}

.ch-tri-foot-actions {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 14px;
  flex: none;
}

.ch-tri-foot-prefilled {
  font-family: var(--ch-font-mono);
  font-size: 10px;
  letter-spacing: 0.1em;
  color: var(--ch-text-meta);
}

/* The board is drawn at 1440. Below it the two fixed columns give ground
 * before the excerpt does -- the excerpt is the evidence. */
@media (max-width: 1280px) {
  .ch-tri-grid {
    grid-template-columns: 3px 104px minmax(0, 1fr) 290px 190px;
  }

  .ch-tri-under-body,
  .ch-tri-under-body--editor {
    padding-left: 118px;
  }
}
</style>
