<script setup lang="ts">
// `/app/notes/:ref` (CHRN-56): a note, read and edited, with its revision
// history and its live reference cards -- board 1c's note half, built under
// the same AppShell chrome PageTreeView/Tier1PageView/DiscussionView already
// establish (CHRN-58's "the smart route view fetches, the dumb component
// renders what it is given" precedent, extended here to two more of its own
// dumb components: Tier1Pane for the pane beside the note, DiscussionThread
// for the discussions embedded under it).
//
// PROVENANCE (the memo it came from, its transcript, its audio and
// `PRUNES <date>`) LANDED HERE IN CHRN-109, against the contract CHRN-107
// shipped. The rule the placeholder existed to protect did not change -- it
// moved a layer down and got a test: every state is read off a
// `MemoProvenance` entry, and no memo id, duration or prune date is computed
// in this file. provenanceBlock.ts holds the rules and its own vitest; this
// view fetches and renders what it is handed.
import { computed, onMounted, onUnmounted, ref, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import Tier1Pane from '@/components/Tier1Pane.vue'
import DiscussionThread from '@/components/DiscussionThread.vue'
import ReferenceCard from '@/components/ReferenceCard.vue'
import { api } from '@/api/client'
import { formatTimestamp } from '@/lib/format'
import { buildReferenceCard, dedupeReferences, MAX_RESOLVE_BATCH } from '@/lib/referenceCard'
import { tier1PathForNotePage } from '@/lib/notePane'
import {
  restorePayloadFor,
  revisionLabel,
  revisionsCountLabel,
  sortRevisionsNewestFirst,
  type Revision,
} from '@/lib/noteRevisions'
import {
  leadProvenance,
  provenanceBlock,
  type AudioControl,
  type MemoProvenance,
  type RevisionMeta,
} from '@/lib/provenanceBlock'
import type { components } from '@/api/schema.d.ts'

type Note = components['schemas']['Note']
type NoteTombstone = components['schemas']['NoteTombstone']
type Resolution = components['schemas']['Resolution']
type Backlink = components['schemas']['Backlink']
type Generated = components['schemas']['Generated']
type Tier1Page = components['schemas']['Tier1Page']

const route = useRoute()
const noteRef = computed(() => String(route.params.ref))

// `getNote`, `appendRevision`, `listNoteRevisions` and `listNoteBacklinks` all
// carry `410 Gone` (openapi.yaml), whose body is a `NoteTombstone` rather
// than an `Error` -- so openapi-fetch's `res.error` type on these calls is a
// union that does not always have `.message`. The 410 case is always handled
// as its own branch before this ever runs; this is just the type-safe way to
// read the ordinary `Error.message` on every other refusal.
function errorMessage(err: unknown, fallback: string): string {
  if (err && typeof err === 'object' && 'message' in err && typeof (err as { message?: unknown }).message === 'string') {
    return (err as { message: string }).message
  }
  return fallback
}

// ── The note itself ─────────────────────────────────────────────────────
const note = ref<Note | null>(null)
const loading = ref(true)
const notFound = ref(false)
const tombstone = ref<NoteTombstone | null>(null)
const loadError = ref<string | null>(null)

type Mode = 'read' | 'edit' | 'history' | 'history-read'
const mode = ref<Mode>('read')

// Bumped on every load and captured per-request (CHRN-58's Tier1PageView.vue
// / DiscussionView.vue precedent), so a slower answer to an earlier ref
// cannot land after a faster one and show the wrong note under the URL.
let loadSeq = 0

// `silent` is what saveEdit/restoreRevision pass: a post-save or
// post-restore reload of the SAME note, not a navigation to a different one.
// Review finding on the CHRN-56 PR: without it, this set `loading.value = true` on
// every reload, and the template's top-level `v-if="loading"` gates the
// WHOLE note view (topbar, editor/history panel, aside) -- so saving an edit
// or restoring a revision blanked the entire screen to "Loading…" and back,
// discarding whatever panel the person was just looking at. A silent reload
// leaves `loading` alone; the old content stays on screen until the new
// note replaces it in place.
async function loadNote(ref_: string, opts: { silent?: boolean } = {}): Promise<void> {
  const seq = ++loadSeq
  if (!opts.silent) loading.value = true
  loadError.value = null
  notFound.value = false
  tombstone.value = null
  const res = await api.GET('/notes/{ref}', { params: { path: { ref: ref_ } } })
  if (seq !== loadSeq) return // superseded by a later navigation
  if (!opts.silent) loading.value = false
  if (res.data) {
    note.value = res.data
    mode.value = 'read'
    // `seq` -- the SAME captured value this function just checked -- is
    // passed down rather than let each loader capture its own (CHRN-110):
    // one guard to keep consistent instead of four, and the next loader
    // added to this fan-out inherits it instead of a chance to forget it.
    loadReferences(note.value.references, seq)
    loadBacklinks(ref_, seq)
    loadTier1(note.value.page, seq)
    loadProvenance(ref_, seq)
    return
  }
  note.value = null
  if (res.response.status === 404) {
    notFound.value = true
    return
  }
  if (res.response.status === 410) {
    // Gone's content is a NoteTombstone, not an Error -- openapi.yaml. Cast
    // rather than re-fetch: the body already arrived on this response.
    tombstone.value = (res.error as unknown as NoteTombstone) ?? null
    return
  }
  loadError.value = errorMessage(res.error, 'This note could not be read.')
}

// Wrapped rather than passed directly: `watch`'s callback signature is
// `(newValue, oldValue)`, and `loadNote`'s own second parameter is now an
// options object -- passing `loadNote` straight to `watch` would hand it the
// PREVIOUS ref string as `opts` on every navigation.
watch(
  noteRef,
  (ref_) => loadNote(ref_),
  { immediate: true },
)

// A live clock so a card's cache age keeps telling the truth for as long as
// somebody stays on the page, rather than freezing at the instant of load --
// CLAUDE.md invariant 2's "a cache with no visible staleness is a copy that
// lies" applies just as much to a card going stale IN THE BROWSER as to one
// the server never refreshed.
const now = ref(Date.now())
let nowTimer: ReturnType<typeof setInterval> | undefined
onMounted(() => {
  nowTimer = setInterval(() => {
    now.value = Date.now()
  }, 30_000)
})
onUnmounted(() => {
  if (nowTimer) clearInterval(nowTimer)
})

const revisionsCount = computed(() => (note.value ? revisionsCountLabel(note.value.revision.seq) : ''))

// ── Provenance: the memo behind the text (CHRN-109) ─────────────────────
//
// The region CHRN-56 left marked and unrendered. `getNoteProvenance` is a
// SIBLING of `getNote` and is fetched on its own, for the reason openapi.yaml
// gives: `retention_status`, `prunes_at` and `audio_pruned_at` all move while
// no revision is appended -- the pruner sweeps at 03:00 and nothing about the
// note has changed -- so folding this into the note's own ETag'd payload
// would answer `304` carrying a `PRUNES` date for audio that went hours ago.
const provenance = ref<MemoProvenance[]>([])

// The player is revealed by the control rather than mounted with the note: an
// `<audio>` carrying a `src` fetches on mount, and that would pull the
// irreplaceable bytes of every memo on the page for a reader who never asked
// to hear one.
const playingMemoId = ref<string | null>(null)

const transcriptFor = ref<string | null>(null)
const transcriptText = ref<string | null>(null)
const transcriptLoading = ref(false)
const transcriptError = ref<string | null>(null)

function closeTranscript(): void {
  transcriptFor.value = null
  transcriptText.value = null
  transcriptError.value = null
  transcriptLoading.value = false
}

// `ROUTED BY SCRIBE` is `RevisionMeta.verb`, which sits on the REVISION and
// not on the provenance entry, so the two lists are zipped by `revision_seq`
// -- openapi.yaml's own instruction. `note.revision` is the current revision
// and answers the ordinary note (one memo, one revision) without a second
// request; a note whose first revision came from a memo and whose latest is a
// hand edit is the case that needs the history page, and that page is
// oldest-first, so it leads with exactly the revisions the entries name. A
// revision this map does not hold contributes no verb, which renders as
// nothing rather than as a guess.
const provenanceRevisions = ref<Map<number, RevisionMeta>>(new Map())

async function loadProvenance(ref_: string, seq: number): Promise<void> {
  provenance.value = []
  provenanceRevisions.value = new Map()
  playingMemoId.value = null
  closeTranscript()
  const res = await api.GET('/notes/{ref}/provenance', { params: { path: { ref: ref_ } } })
  if (seq !== loadSeq) return // superseded by a later navigation
  // No error surface, deliberately. This block is supplementary to a note
  // that has already rendered, and the one thing it must never do is say
  // something about a recording it could not read. Absent is the honest
  // failure, and it is the same render a typed note gets.
  if (!res.data) return
  provenance.value = res.data.items
  const current = note.value?.revision
  if (current) provenanceRevisions.value = new Map([[current.seq, current]])
  if (!res.data.items.some((entry) => entry.revision_seq !== current?.seq)) return
  const page = await api.GET('/notes/{ref}/revisions', { params: { path: { ref: ref_ } } })
  if (seq !== loadSeq) return // superseded by a later navigation (second await)
  if (!page.data) return
  const zipped = new Map(provenanceRevisions.value)
  for (const rev of page.data.items) zipped.set(rev.seq, rev)
  provenanceRevisions.value = zipped
}

/** One block per memo, oldest first -- the list's own order. */
const provenanceBlocks = computed(() =>
  provenance.value.map((entry) => provenanceBlock(entry, provenanceRevisions.value.get(entry.revision_seq))),
)

// The header renders `items[0]`: the memo that STARTED the note.
const leadBlock = computed(() => {
  const entry = leadProvenance({ items: provenance.value })
  return entry ? provenanceBlock(entry, provenanceRevisions.value.get(entry.revision_seq)) : null
})

// `▶ PLAY SOURCE AUDIO 1:44`. The board keeps the label and the duration in
// ONE span separated by a single space -- the `·` separators either side of it
// divide the label from the retention date, not the label from its own length
// -- and `audioControlFor` hands them over separately precisely so that a memo
// nobody has measured yet renders the label alone rather than `0:00`.
function audioControlLabel(audio: Extract<AudioControl, { state: 'playable' }>): string {
  return audio.duration ? `${audio.label} ${audio.duration}` : audio.label
}

function toggleAudio(memoId: string): void {
  playingMemoId.value = playingMemoId.value === memoId ? null : memoId
}

async function toggleTranscript(memoId: string): Promise<void> {
  const reopening = transcriptFor.value !== memoId
  closeTranscript()
  if (!reopening) return
  transcriptFor.value = memoId
  transcriptLoading.value = true
  const res = await api.GET('/transcripts/{memo_id}', { params: { path: { memo_id: memoId } } })
  if (transcriptFor.value !== memoId) return // closed or switched while in flight
  transcriptLoading.value = false
  if (res.data) {
    transcriptText.value = res.data.text
    return
  }
  transcriptError.value = errorMessage(res.error, 'The transcript could not be read.')
}

// ── References, resolved live (CLAUDE.md invariant 2) ──────────────────
const resolutions = ref<Map<string, Resolution>>(new Map())
const referencesLoading = ref(false)
const referencesError = ref<string | null>(null)

async function loadReferences(descriptors: Note['references'], seq: number): Promise<void> {
  const deduped = dedupeReferences(descriptors)
  resolutions.value = new Map()
  referencesError.value = null
  if (deduped.length === 0) return
  referencesLoading.value = true
  const res = await api.POST('/references/resolve', {
    body: { references: deduped.slice(0, MAX_RESOLVE_BATCH) },
  })
  if (seq !== loadSeq) return // superseded by a later navigation
  referencesLoading.value = false
  if (res.data) {
    resolutions.value = new Map(res.data.resolutions.map((r) => [r.token, r]))
    return
  }
  referencesError.value = errorMessage(res.error, 'References could not be resolved.')
}

const referenceCards = computed(() => {
  if (!note.value) return []
  return dedupeReferences(note.value.references).map((d) =>
    buildReferenceCard(d, resolutions.value.get(d.token), now.value),
  )
})

// ── Tier-1 pane beside the note ─────────────────────────────────────────
const tier1Page = ref<Tier1Page | null>(null)
const tier1Loading = ref(false)
const tier1NotFound = ref(false)
const tier1NoticeError = ref<string | null>(null)

async function loadTier1(pagePath: string, seq: number): Promise<void> {
  tier1Page.value = null
  tier1NotFound.value = false
  tier1NoticeError.value = null
  const path = tier1PathForNotePage(pagePath)
  if (!path) {
    tier1NotFound.value = true
    return
  }
  tier1Loading.value = true
  const res = await api.GET('/tier1/page', { params: { query: { path } } })
  if (seq !== loadSeq) return // superseded by a later navigation
  tier1Loading.value = false
  if (res.data) {
    tier1Page.value = res.data
    return
  }
  if (res.response.status === 404) {
    tier1NotFound.value = true
    return
  }
  tier1NoticeError.value = errorMessage(res.error, 'The tier-1 pane could not be read.')
}

// ── Backlinks -- generated, steel (CLAUDE.md invariant 1) ───────────────
const backlinks = ref<Backlink[] | null>(null)
const backlinksGenerated = ref<Generated | null>(null)
const backlinksLoading = ref(false)
const backlinksError = ref<string | null>(null)
const backlinksCursor = ref<string | undefined>(undefined)

async function loadBacklinks(ref_: string, seq: number, cursor?: string): Promise<void> {
  backlinksLoading.value = true
  backlinksError.value = null
  const res = await api.GET('/notes/{ref}/backlinks', {
    params: { path: { ref: ref_ }, query: cursor ? { cursor } : {} },
  })
  if (seq !== loadSeq) return // superseded by a later navigation
  backlinksLoading.value = false
  if (res.data) {
    backlinks.value = cursor ? [...(backlinks.value ?? []), ...res.data.items] : res.data.items
    backlinksGenerated.value = res.data.generated
    backlinksCursor.value = res.data.next_cursor
    return
  }
  if (!cursor) backlinks.value = null
  backlinksError.value = errorMessage(res.error, 'Backlinks could not be read.')
}

// `loadMoreBacklinks` is a user click on a "Load more" button, not part of
// `loadNote`'s own fan-out -- there is no freshly-captured `seq` to inherit.
// It reads the LIVE `loadSeq` at click time instead, which is exactly the
// sequence number of the note currently on screen (nothing else bumps it),
// so the same guard still drops the page if a navigation lands while this
// page is in flight.
function loadMoreBacklinks(): void {
  if (backlinksCursor.value) loadBacklinks(noteRef.value, loadSeq, backlinksCursor.value)
}

// ── Discussions this note resolved from ─────────────────────────────────
// The board draws "OPEN QUESTIONS" here, but the only thing `Note` carries
// is `resolved_from` -- discussions that already CONCLUDED into this note
// (DiscussionSummary.resolved_at is required, never absent). Calling an
// already-resolved thread an "open question" would be dishonest about data
// the contract does give us, so this renders under a label that says what it
// actually is; named as a deviation from the board in the PR.
const expandedDiscussions = ref<Set<string>>(new Set())
function toggleDiscussion(ref_: string): void {
  const next = new Set(expandedDiscussions.value)
  if (next.has(ref_)) next.delete(ref_)
  else next.add(ref_)
  expandedDiscussions.value = next
}

// ── Editor (CHRN-39: nothing lands in authored text unattended -- the
// person's own session, submitting this form, is the confirmer) ──────────
const editTitle = ref('')
const editBody = ref('')
const saving = ref(false)
const saveError = ref<string | null>(null)

function openEditor(): void {
  if (!note.value) return
  editTitle.value = note.value.title
  editBody.value = note.value.body
  saveError.value = null
  mode.value = 'edit'
}

function cancelEdit(): void {
  saveError.value = null
  mode.value = 'read'
}

async function saveEdit(): Promise<void> {
  if (!note.value || !editBody.value.trim()) return
  saving.value = true
  saveError.value = null
  const res = await api.POST('/notes/{ref}/revisions', {
    params: { path: { ref: noteRef.value } },
    body: { title: editTitle.value.trim(), body: editBody.value },
  })
  saving.value = false
  if (res.data) {
    await loadNote(noteRef.value, { silent: true })
    return
  }
  // Shown verbatim -- a guard refusal (CH041 PersonRequired, or any other
  // 4xx) must not be swallowed or reworded.
  saveError.value = errorMessage(res.error, 'The edit was refused.')
}

// ── History, and restore-as-append (never a rewrite) ────────────────────
const revisions = ref<Revision[]>([])
const revisionsLoading = ref(false)
const revisionsError = ref<string | null>(null)
const revisionsNextCursor = ref<string | undefined>(undefined)
const selectedRevision = ref<Revision | null>(null)
const restoring = ref(false)
const restoreError = ref<string | null>(null)

// Not part of `loadNote`'s fan-out either -- opened and paginated entirely
// by user clicks on the note already on screen (CHRN-110). Same reasoning as
// `loadMoreBacklinks` above: the caller hands down the LIVE `loadSeq` at
// click time rather than a value captured from `loadNote`, because there is
// no such capture to inherit here, and the live counter still names exactly
// the note currently displayed.
async function loadRevisions(seq: number, cursor?: string): Promise<void> {
  revisionsLoading.value = true
  revisionsError.value = null
  const res = await api.GET('/notes/{ref}/revisions', {
    params: { path: { ref: noteRef.value }, query: cursor ? { cursor } : {} },
  })
  if (seq !== loadSeq) return // superseded by a later navigation
  revisionsLoading.value = false
  if (res.data) {
    const page = sortRevisionsNewestFirst(res.data.items)
    revisions.value = cursor ? [...revisions.value, ...page] : page
    revisionsNextCursor.value = res.data.next_cursor
    return
  }
  revisionsError.value = errorMessage(res.error, 'History could not be read.')
}

function openHistory(): void {
  mode.value = 'history'
  revisions.value = []
  revisionsNextCursor.value = undefined
  loadRevisions(loadSeq)
}

function loadMoreRevisions(): void {
  if (revisionsNextCursor.value) loadRevisions(loadSeq, revisionsNextCursor.value)
}

function readRevision(rev: Revision): void {
  selectedRevision.value = rev
  restoreError.value = null
  mode.value = 'history-read'
}

function backToHistory(): void {
  selectedRevision.value = null
  mode.value = 'history'
}

async function restoreRevision(): Promise<void> {
  if (!selectedRevision.value) return
  restoring.value = true
  restoreError.value = null
  const res = await api.POST('/notes/{ref}/revisions', {
    params: { path: { ref: noteRef.value } },
    body: restorePayloadFor(selectedRevision.value),
  })
  restoring.value = false
  if (res.data) {
    selectedRevision.value = null
    await loadNote(noteRef.value, { silent: true })
    return
  }
  restoreError.value = errorMessage(res.error, 'The restore was refused.')
}
</script>

<template>
  <div class="ch-note-route">
    <p v-if="loading" class="ch-note-status">Loading…</p>
    <p v-else-if="notFound" class="ch-note-status">No note at {{ noteRef }}.</p>
    <div v-else-if="tombstone" class="ch-note-tombstone">
      <div class="ch-note-tombstone-ref">{{ tombstone.ref }}</div>
      <p>This note was withdrawn on {{ formatTimestamp(tombstone.deleted_at) }}.</p>
    </div>
    <p v-else-if="loadError" class="ch-note-status">{{ loadError }}</p>

    <template v-else-if="note">
      <div class="ch-note-topbar">
        <RouterLink :to="`/pages/${note.page}`" class="ch-note-breadcrumb">{{ note.page }}</RouterLink>
        <div class="ch-note-topbar-actions">
          <button
            type="button"
            class="ch-note-topbar-btn"
            :class="{ 'is-active': mode === 'edit' }"
            @click="mode === 'edit' ? cancelEdit() : openEditor()"
          >
            EDIT
          </button>
          <button
            type="button"
            class="ch-note-topbar-btn"
            :class="{ 'is-active': mode === 'history' || mode === 'history-read' }"
            @click="mode === 'history' || mode === 'history-read' ? (mode = 'read') : openHistory()"
          >
            HISTORY
          </button>
        </div>
      </div>

      <div class="ch-note-body-col">
        <!-- ══ READ ══ -->
        <template v-if="mode === 'read'">
          <div class="ch-note-header">
            <span class="ch-note-handle">A NOTE · {{ note.ref }}</span>
            <!-- CHRN-109: board 1c's "FROM MEMO 12:55 · 1:44 · ROUTED BY
                 SCRIBE", composed by provenanceBlock.ts from `items[0]` and
                 that revision's `verb`. A note somebody typed has no entry
                 and renders nothing here, which is not an error. -->
            <span v-if="leadBlock" class="ch-note-provenance-from">{{ leadBlock.header }}</span>
          </div>
          <h1 class="ch-note-title">{{ note.title }}</h1>

          <!-- The renderer passes no raw HTML through (openapi.yaml: "safe
               to embed") -- the one v-html in this file, matching the
               established pattern (Tier1Pane.vue, DiscussionThread.vue). -->
          <!-- eslint-disable-next-line vue/no-v-html -->
          <div class="ch-note-html ch-md" v-html="note.html"></div>

          <div v-if="referenceCards.length > 0" class="ch-note-references">
            <p v-if="referencesLoading" class="ch-note-references-status">Resolving references…</p>
            <p v-else-if="referencesError" class="ch-note-references-status">{{ referencesError }}</p>
            <ReferenceCard v-for="card in referenceCards" :key="card.token" :card="card" />
          </div>

          <div class="ch-note-footer">
            <!-- CHRN-109: one row per memo, oldest first. Every state is the
                 entry's own -- nothing here computes a date, and nothing is
                 read off a failed request, because an `<audio>` element never
                 sees the body of the 410 that would say the bytes are gone. -->
            <div v-if="provenanceBlocks.length > 0" class="ch-note-provenance">
              <div v-for="block in provenanceBlocks" :key="block.memoId" class="ch-note-provenance-entry">
                <div class="ch-note-provenance-row">
                  <template v-if="block.audio.state === 'playable'">
                    <button type="button" class="ch-note-provenance-control" @click="toggleAudio(block.memoId)">
                      {{ audioControlLabel(block.audio) }}
                    </button>
                    <span class="ch-note-provenance-sep">·</span>
                    <span class="ch-note-provenance-meta">{{ block.audio.retention }}</span>
                  </template>
                  <!-- `absent` draws NOTHING -- not a disabled control. A
                       control somebody can see but not use tells them a
                       recording they may not hear exists, which is a fact
                       about another account's activity. -->
                  <span v-else-if="block.audio.state === 'pruned'" class="ch-note-provenance-meta">{{
                    block.audio.label
                  }}</span>
                  <template v-if="block.transcript.state === 'readable'">
                    <span v-if="block.audio.state !== 'absent'" class="ch-note-provenance-sep">·</span>
                    <button type="button" class="ch-note-provenance-control" @click="toggleTranscript(block.memoId)">
                      {{ transcriptFor === block.memoId ? 'HIDE TRANSCRIPT' : 'READ TRANSCRIPT' }}
                    </button>
                  </template>
                </div>
                <audio
                  v-if="block.audio.state === 'playable' && playingMemoId === block.memoId"
                  class="ch-note-provenance-audio"
                  controls
                  autoplay
                  :src="block.audio.href"
                ></audio>
                <div v-if="transcriptFor === block.memoId" class="ch-note-transcript">
                  <p class="ch-note-transcript-label">{{ block.transcript.label }}</p>
                  <p v-if="transcriptLoading" class="ch-note-provenance-meta">Loading…</p>
                  <p v-else-if="transcriptError" class="ch-note-provenance-meta">{{ transcriptError }}</p>
                  <!-- Empty is a TRUE and complete answer, not a failure:
                       "a memo that is forty seconds of silence has a true and
                       complete answer, and the answer is no speech". -->
                  <p v-else-if="transcriptText === ''" class="ch-note-provenance-meta">NO SPEECH</p>
                  <p v-else class="ch-note-transcript-text">{{ transcriptText }}</p>
                </div>
              </div>
            </div>
            <span class="ch-note-footer-revisions">{{ revisionsCount }}</span>
          </div>
        </template>

        <!-- ══ EDIT ══ -->
        <template v-else-if="mode === 'edit'">
          <div class="ch-note-header">
            <span class="ch-note-handle">EDITING · {{ note.ref }}</span>
          </div>
          <form class="ch-note-editor" @submit.prevent="saveEdit">
            <input v-model="editTitle" class="ch-note-editor-title" placeholder="Title" :disabled="saving" required />
            <textarea
              v-model="editBody"
              class="ch-note-editor-body"
              placeholder="Markdown…"
              rows="16"
              :disabled="saving"
              required
            ></textarea>
            <div class="ch-note-editor-row">
              <button type="submit" :disabled="saving || !editBody.trim()">{{ saving ? 'Saving…' : 'Save' }}</button>
              <button type="button" :disabled="saving" @click="cancelEdit">Cancel</button>
            </div>
            <!-- The guard's own message, verbatim -- CH041 (PersonRequired)
                 or any other refusal must not be reworded or swallowed. -->
            <p v-if="saveError" class="ch-note-error">{{ saveError }}</p>
          </form>
        </template>

        <!-- ══ HISTORY (list) ══ -->
        <template v-else-if="mode === 'history'">
          <div class="ch-note-header">
            <span class="ch-note-handle">HISTORY · {{ note.ref }}</span>
          </div>
          <p v-if="revisionsLoading && revisions.length === 0" class="ch-note-status">Loading…</p>
          <p v-else-if="revisionsError" class="ch-note-status">{{ revisionsError }}</p>
          <ul v-else class="ch-note-revision-list">
            <li v-for="rev in revisions" :key="rev.id">
              <button type="button" class="ch-note-revision-row" @click="readRevision(rev)">
                <span class="ch-note-revision-seq">{{ revisionLabel(rev) }}</span>
                <span class="ch-note-revision-title">{{ rev.title }}</span>
              </button>
            </li>
          </ul>
          <button
            v-if="revisionsNextCursor"
            type="button"
            class="ch-note-load-more"
            :disabled="revisionsLoading"
            @click="loadMoreRevisions"
          >
            {{ revisionsLoading ? 'Loading…' : 'Load more' }}
          </button>
        </template>

        <!-- ══ HISTORY (reading one revision, raw) ══ -->
        <template v-else-if="mode === 'history-read' && selectedRevision">
          <div class="ch-note-header">
            <button type="button" class="ch-note-back" @click="backToHistory">← HISTORY</button>
            <span class="ch-note-handle ch-note-revision-banner">{{ revisionLabel(selectedRevision) }}</span>
          </div>
          <h1 class="ch-note-title">{{ selectedRevision.title }}</h1>
          <!-- Raw markdown, deliberately NOT rendered: `Revision` carries no
               `html`, only the text as written, and this client has no
               sanitising renderer of its own to pass it through safely --
               so it is shown as plain text, never v-html'd. -->
          <pre class="ch-note-revision-body">{{ selectedRevision.body }}</pre>
          <div class="ch-note-revision-actions">
            <button type="button" :disabled="restoring" @click="restoreRevision">
              {{ restoring ? 'Restoring…' : 'Restore' }}
            </button>
            <span class="ch-note-revision-actions-note">Restoring appends this text as a new revision. Nothing is overwritten.</span>
          </div>
          <p v-if="restoreError" class="ch-note-error">{{ restoreError }}</p>
        </template>
      </div>

      <aside class="ch-note-aside">
        <template v-if="!tier1NotFound && !tier1NoticeError">
          <Tier1Pane :page="tier1Page" :loading="tier1Loading" :error="null" />
        </template>
        <p v-else-if="tier1NoticeError" class="ch-note-aside-notice">{{ tier1NoticeError }}</p>

        <div class="ch-note-backlinks">
          <div class="ch-note-aside-label">BACKLINKS{{ backlinks ? ` · ${backlinks.length}` : '' }}</div>
          <p v-if="backlinksLoading && !backlinks" class="ch-note-aside-status">Loading…</p>
          <p v-else-if="backlinksError" class="ch-note-aside-status">{{ backlinksError }}</p>
          <template v-else-if="backlinks">
            <p v-if="backlinks.length === 0" class="ch-note-aside-status">Nothing links here yet.</p>
            <RouterLink
              v-for="b in backlinks"
              :key="b.ref"
              :to="`/notes/${b.ref}`"
              class="ch-note-backlink-row"
            >
              {{ b.title }}
            </RouterLink>
            <button v-if="backlinksCursor" type="button" class="ch-note-load-more" @click="loadMoreBacklinks">
              Load more
            </button>
            <div v-if="backlinksGenerated" class="ch-note-aside-footer">{{ backlinksGenerated.notice }}</div>
          </template>
        </div>

        <div class="ch-note-discussions">
          <div class="ch-note-aside-label ch-note-aside-label--tier2">DISCUSSIONS · {{ note.resolved_from.length }}</div>
          <p v-if="note.resolved_from.length === 0" class="ch-note-aside-status">None resolved into this note.</p>
          <div v-for="d in note.resolved_from" :key="d.ref" class="ch-note-discussion">
            <button type="button" class="ch-note-discussion-row" @click="toggleDiscussion(d.ref)">
              <span class="ch-note-discussion-title">{{ d.title }}</span>
              <span class="ch-note-discussion-meta">{{ formatTimestamp(d.resolved_at) }}</span>
            </button>
            <DiscussionThread v-if="expandedDiscussions.has(d.ref)" :source="d.ref" :mark-read-on-open="false" />
          </div>
        </div>
      </aside>
    </template>
  </div>
</template>

<style scoped>
.ch-note-route {
  padding: 34px 46px;
  display: grid;
  grid-template-columns: minmax(0, 1fr) 320px;
  gap: 28px;
  align-items: start;
}

@media (max-width: 980px) {
  .ch-note-route {
    grid-template-columns: 1fr;
  }
}

.ch-note-status {
  margin: 0;
  grid-column: 1 / -1;
  font-size: var(--ch-size-body);
  color: var(--ch-text-meta);
}

.ch-note-tombstone {
  grid-column: 1 / -1;
}

.ch-note-tombstone-ref {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-sm);
  color: var(--ch-text-meta);
}

.ch-note-tombstone p {
  margin-top: 10px;
  color: var(--ch-text-2);
}

.ch-note-topbar {
  grid-column: 1 / -1;
  display: flex;
  align-items: center;
  gap: 14px;
  padding-bottom: 16px;
  margin-bottom: 8px;
  border-bottom: 1px solid var(--ch-line);
}

.ch-note-breadcrumb {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  letter-spacing: var(--ch-track-mid);
  color: var(--ch-text-meta);
  text-decoration: none;
}

.ch-note-breadcrumb:hover {
  color: var(--ch-text);
}

.ch-note-topbar-actions {
  margin-left: auto;
  display: flex;
  gap: 16px;
}

.ch-note-topbar-btn {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  letter-spacing: var(--ch-track-mid);
  color: var(--ch-text-2);
  background: none;
  border: none;
  cursor: pointer;
  padding: 4px 2px;
}

.ch-note-topbar-btn:hover {
  color: var(--ch-text);
}

.ch-note-topbar-btn.is-active {
  color: var(--ch-signal);
}

.ch-note-body-col {
  min-width: 0;
}

.ch-note-header {
  display: flex;
  align-items: center;
  gap: 12px;
}

.ch-note-handle {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  letter-spacing: var(--ch-track-wide);
  color: var(--ch-signal);
}

.ch-note-title {
  margin: 16px 0 0;
  font-family: var(--ch-font-serif);
  font-size: 34px;
  font-weight: 400;
  line-height: 1.18;
  color: var(--ch-text);
  max-width: 26ch;
}

.ch-note-html {
  margin-top: 22px;
  max-width: 66ch;
  font-family: var(--ch-font-serif);
  font-size: 18px;
  line-height: 1.72;
  color: var(--ch-text-2);
}

.ch-note-html :deep(p) {
  margin: 0 0 16px;
}

.ch-note-html :deep(p:last-child) {
  margin-bottom: 0;
}

/* The estate-reference span internal/markdown/markdown.go marks
   (`.ref.ref-<system>`) stays plain in the flowing body text -- the card
   list below carries the colour and the live state, per this ticket's own
   "choose the simpler correct option" call, named in the PR. */
.ch-note-html :deep(.ref) {
  font-family: var(--ch-font-mono);
  font-size: 0.9em;
}

.ch-note-references {
  margin-top: 24px;
  max-width: 66ch;
  display: flex;
  flex-direction: column;
  gap: 10px;
}

.ch-note-references-status {
  margin: 0;
  font-size: var(--ch-size-xs);
  color: var(--ch-text-meta);
}

.ch-note-footer {
  margin-top: 30px;
  padding-top: 18px;
  border-top: 1px solid var(--ch-line);
  max-width: 66ch;
  display: flex;
  /* Not `center`: CHRN-109's player and transcript disclosure grow DOWNWARD
   * out of this row, and centring would drag `N REVISIONS` into the middle of
   * an opened transcript. In the board's own case -- one line, one memo --
   * both children are one line tall and this is indistinguishable. */
  align-items: flex-start;
  gap: 12px;
}

.ch-note-footer-revisions {
  margin-left: auto;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  color: var(--ch-text-meta);
}

/* CHRN-109 · board 1c's provenance block. The row the board draws IS
 * `.ch-note-footer` above -- CHRN-56 built it at the board's own margin,
 * padding, rule and max-width with only `N REVISIONS` in it -- so the block
 * lands inside it, and the ordinary note (one memo, one revision) renders as
 * the board's single line: control at the left, revisions count at the right.
 * Several memos stack instead of crowding one line. */
.ch-note-provenance {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: var(--ch-space-2);
}

.ch-note-provenance-entry {
  display: flex;
  flex-direction: column;
  gap: var(--ch-space-1);
}

.ch-note-provenance-row {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  /* 12px, matching `.ch-note-header` above and the board's own row gap. */
  gap: 12px;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  letter-spacing: var(--ch-track-mid);
}

/* The header line, beside `A NOTE · CHR-0311`. */
.ch-note-provenance-from {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  color: var(--ch-text-meta);
}

.ch-note-provenance-meta {
  margin: 0;
  color: var(--ch-text-meta);
}

.ch-note-provenance-sep {
  color: var(--ch-line);
}

/* Vellum, per the ticket: the play control and the transcript disclosure are
 * the only things in this row a reader can act on, and vellum is Chronicle's
 * own signal. Deliberately NOT steel -- a Scribe-routed memo is authored tier
 * 2, and steel means regenerated tier 1 (DiscussionThread.vue took the same
 * decision about the same board's Scribe avatar). */
.ch-note-provenance-control {
  padding: 0;
  border: 0;
  background: none;
  font: inherit;
  letter-spacing: inherit;
  color: var(--ch-signal);
  cursor: pointer;
}

.ch-note-provenance-control:hover {
  color: var(--ch-text);
}

/* The NATIVE control, deliberately: it is the accessible, keyboard-operable,
 * range-request-aware player every browser already ships, and a hand-drawn
 * one would be a worse version of it. `color-scheme` is the one thing set --
 * it asks the browser for the dark variant of its own widget, rather than
 * trying to restyle shadow-DOM internals that differ per engine. */
.ch-note-provenance-audio {
  width: 100%;
  max-width: 420px;
  height: 32px;
  color-scheme: dark;
}

.ch-note-transcript {
  max-width: 66ch;
}

.ch-note-transcript-label {
  margin: 0 0 var(--ch-space-1);
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-2xs);
  letter-spacing: var(--ch-track-wide);
  color: var(--ch-text-meta);
}

/* `pre-wrap`: a transcript's own line breaks are what the ASR service
 * recorded, and reflowing them would be this view editing tier-2 text. */
.ch-note-transcript-text {
  margin: 0;
  font-size: var(--ch-size-body);
  line-height: 1.6;
  color: var(--ch-text-2);
  white-space: pre-wrap;
}

/* ── Editor ── */
.ch-note-editor {
  margin-top: 20px;
  max-width: 66ch;
  display: flex;
  flex-direction: column;
  gap: 12px;
}

.ch-note-editor-title {
  border: 1px solid var(--ch-line);
  background: var(--ch-raised);
  color: var(--ch-text);
  padding: 10px 12px;
  font-family: var(--ch-font-serif);
  font-size: 20px;
}

.ch-note-editor-body {
  border: 1px solid var(--ch-line);
  background: var(--ch-raised);
  color: var(--ch-text);
  padding: 12px;
  font-family: var(--ch-font-mono);
  font-size: 13px;
  line-height: 1.6;
  resize: vertical;
}

.ch-note-editor-row {
  display: flex;
  gap: 10px;
}

.ch-note-editor-row button,
.ch-note-revision-actions button {
  border: 1px solid var(--ch-line);
  background: var(--ch-base);
  color: var(--ch-text);
  padding: 8px 16px;
  cursor: pointer;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  letter-spacing: var(--ch-track-mid);
}

.ch-note-editor-row button:disabled,
.ch-note-revision-actions button:disabled {
  color: var(--ch-text-meta);
  cursor: default;
}

.ch-note-error {
  margin: 4px 0 0;
  font-size: var(--ch-size-xs);
  color: var(--ch-text-2);
}

/* ── History ── */
.ch-note-revision-list {
  margin: 18px 0 0;
  padding: 0;
  list-style: none;
  max-width: 66ch;
  border-top: 1px solid var(--ch-line);
}

.ch-note-revision-row {
  width: 100%;
  display: flex;
  align-items: baseline;
  gap: 14px;
  padding: 12px 2px;
  border-bottom: 1px solid var(--ch-line);
  background: none;
  border-left: none;
  border-right: none;
  border-top: none;
  text-align: left;
  cursor: pointer;
  color: inherit;
}

.ch-note-revision-row:hover {
  color: var(--ch-text);
}

.ch-note-revision-seq {
  flex: none;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  color: var(--ch-text-meta);
}

.ch-note-revision-title {
  font-size: var(--ch-size-md);
  color: var(--ch-text-2);
}

.ch-note-back {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  letter-spacing: var(--ch-track-mid);
  color: var(--ch-text-meta);
  background: none;
  border: none;
  cursor: pointer;
  padding: 0;
}

.ch-note-back:hover {
  color: var(--ch-text);
}

.ch-note-revision-banner {
  color: var(--ch-text-meta);
}

.ch-note-revision-body {
  margin-top: 20px;
  max-width: 66ch;
  padding: 16px;
  border: 1px solid var(--ch-line);
  background: var(--ch-raised);
  font-family: var(--ch-font-mono);
  font-size: 13px;
  line-height: 1.6;
  color: var(--ch-text-2);
  white-space: pre-wrap;
  word-break: break-word;
}

.ch-note-revision-actions {
  margin-top: 16px;
  display: flex;
  align-items: center;
  gap: 14px;
}

.ch-note-revision-actions-note {
  font-size: var(--ch-size-xs);
  color: var(--ch-text-meta);
}

.ch-note-load-more {
  margin-top: 14px;
  border: 1px solid var(--ch-line);
  background: none;
  color: var(--ch-text-2);
  padding: 6px 14px;
  cursor: pointer;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
}

.ch-note-load-more:disabled {
  color: var(--ch-text-meta);
  cursor: default;
}

/* ── Right column ── */
.ch-note-aside {
  display: flex;
  flex-direction: column;
  gap: 26px;
}

.ch-note-aside-notice {
  margin: 0;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  color: var(--ch-text-meta);
}

.ch-note-aside-label {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-2xs);
  letter-spacing: var(--ch-track-wide);
  margin-bottom: 10px;
  /* Steel: this list carries the Generated marking (CLAUDE.md invariant 1). */
  color: var(--ch-generated);
}

.ch-note-aside-label--tier2 {
  /* Discussions are authored, tier-2 content -- vellum, not steel. */
  color: var(--ch-signal);
}

.ch-note-aside-status {
  margin: 0;
  font-size: var(--ch-size-xs);
  color: var(--ch-text-meta);
}

.ch-note-backlinks {
  font-family: var(--ch-font-mono);
  color: var(--ch-generated);
}

.ch-note-backlink-row {
  display: block;
  padding: 8px 0;
  font-size: 13px;
  color: var(--ch-text-2);
  text-decoration: none;
  border-top: 1px solid var(--ch-line);
}

.ch-note-backlink-row:hover {
  color: var(--ch-generated);
}

.ch-note-aside-footer {
  margin-top: 14px;
  padding-top: 12px;
  border-top: 1px solid var(--ch-line);
  font-size: var(--ch-size-2xs);
  letter-spacing: var(--ch-track-mid);
  color: var(--ch-text-meta);
}

.ch-note-discussions {
  padding-top: 20px;
  border-top: 1px solid var(--ch-line);
}

.ch-note-discussion {
  margin-top: 10px;
}

.ch-note-discussion-row {
  width: 100%;
  display: flex;
  flex-direction: column;
  gap: 3px;
  align-items: flex-start;
  padding: 8px 10px;
  background: var(--ch-raised);
  border: 1px solid var(--ch-line);
  cursor: pointer;
  text-align: left;
}

.ch-note-discussion-title {
  font-size: 13px;
  color: var(--ch-text-2);
}

.ch-note-discussion-meta {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-2xs);
  color: var(--ch-text-meta);
}

.ch-note-discussion :deep(.ch-thread) {
  margin-top: 8px;
  padding: 14px 16px;
}
</style>
