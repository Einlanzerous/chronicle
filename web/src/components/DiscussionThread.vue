<script setup lang="ts">
// The thread body — turns in order, plus the resolved/counts footer — factored
// out of the `/app/discussions/:ref` route (CHRN-57) so CHRN-56 can embed the
// same rendering under a note for its "open questions" block without
// duplicating the turn/avatar/unread logic. Takes EITHER a discussion ref
// (self-fetches, for a standalone embed) OR an already-loaded `Thread` (the
// route view passes its own fetch's result, avoiding a second GET — the same
// controlled-component shape Tier1Pane.vue uses for `page`).
import { computed, ref, watch } from 'vue'
import { api } from '@/api/client'
import { formatTimestamp } from '@/lib/format'
import {
  avatarTreatmentFor,
  canReply,
  firstUnreadSeq,
  hasReadMarker,
  initialsFor,
  resolvedFooterLabel,
  threadCountsLabel,
  type Thread,
  type Turn,
} from '@/lib/discussionThread'

const props = withDefaults(
  defineProps<{
    source: string | Thread
    /**
     * Calls `markRead` through the last turn once the thread loads —
     * "posting is reading" (openapi.yaml) extended to "opening is reading".
     * On by default; a future preview embed that must not move the reader's
     * own cursor can pass `false`.
     */
    markReadOnOpen?: boolean
  }>(),
  { markReadOnOpen: true },
)

const emit = defineEmits<{
  /** Fired whenever a Thread becomes available — initial self-fetch, or the
   *  `source` object prop changing under the caller. Lets the route view
   *  read discussion metadata (title, ref, resolved state) for its own
   *  header without a second GET. */
  loaded: [thread: Thread]
}>()

const thread = ref<Thread | null>(typeof props.source === 'string' ? null : props.source)
const loading = ref(typeof props.source === 'string')
const error = ref<string | null>(null)

// Bumped on every load and captured per-request (CHRN-58's precedent in
// Tier1PageView.vue / SearchView.vue), so a slower answer to an earlier ref
// cannot land after a faster one and show the wrong thread.
let loadSeq = 0

async function load(source: string | Thread): Promise<void> {
  if (typeof source !== 'string') {
    thread.value = source
    loading.value = false
    error.value = null
    emit('loaded', source)
    return
  }
  const seq = ++loadSeq
  loading.value = true
  error.value = null
  const res = await api.GET('/discussions/{ref}', { params: { path: { ref: source } } })
  if (seq !== loadSeq) return // superseded by a later request
  loading.value = false
  if (res.data) {
    thread.value = res.data
    emit('loaded', res.data)
    return
  }
  thread.value = null
  error.value = res.error?.message ?? 'This thread could not be read.'
}

watch(() => props.source, load, { immediate: true })

// Mark read once the thread we are actually rendering settles — after the
// NEW-rule seq below has already been computed from the PRE-read `unread`
// count, so marking read does not erase the marker out from under whoever is
// still looking at it. A reload (a fresh GET, openapi.yaml's getDiscussion)
// is what shows the server's post-read state; this call does not repaint
// the current view.
watch(
  thread,
  (t) => {
    if (!t || !props.markReadOnOpen || !hasReadMarker(t)) return
    const lastSeq = t.turns.reduce((max, turn) => Math.max(max, turn.seq), 0)
    if (lastSeq === 0) return
    api
      .POST('/discussions/{ref}/read', {
        params: { path: { ref: t.discussion.ref } },
        body: { through_seq: lastSeq },
      })
      .catch(() => {
        // Best-effort: the Done-when is "a reload agrees with the server",
        // not "this call never fails" — a failed mark-read leaves the
        // marker where it was, which a later open retries.
      })
  },
  { immediate: true },
)

const participantByUserId = computed(
  () => new Map((thread.value?.participants ?? []).map((p) => [p.user_id, p])),
)

function authorName(turn: Turn): string {
  return participantByUserId.value.get(turn.author_id)?.display_name ?? 'Removed account'
}

const newRuleSeq = computed(() =>
  thread.value ? firstUnreadSeq(thread.value.turns, thread.value.unread) : null,
)
const resolvedLabel = computed(() => (thread.value ? resolvedFooterLabel(thread.value.discussion) : null))
const countsLabel = computed(() =>
  thread.value ? threadCountsLabel(thread.value.turns, thread.value.participants) : '',
)

defineExpose({ canReply: computed(() => (thread.value ? canReply(thread.value.discussion) : false)) })
</script>

<template>
  <div class="ch-thread">
    <p v-if="loading" class="ch-thread-status">Loading…</p>
    <p v-else-if="error" class="ch-thread-status">{{ error }}</p>

    <template v-else-if="thread">
      <div class="ch-thread-turns">
        <template v-for="t in thread.turns" :key="t.id">
          <div v-if="t.seq === newRuleSeq" class="ch-thread-new-rule">
            <span class="ch-thread-new-label">NEW</span>
            <span class="ch-thread-new-line" aria-hidden="true"></span>
          </div>
          <div class="ch-thread-turn">
            <span
              class="ch-thread-avatar"
              :class="`ch-thread-avatar--${avatarTreatmentFor(t.author_kind).shape}`"
              aria-hidden="true"
              >{{ initialsFor(authorName(t)) }}</span
            >
            <div class="ch-thread-turn-body">
              <div class="ch-thread-turn-meta">
                <span class="ch-thread-turn-author">{{ authorName(t) }}</span>
                <span v-if="avatarTreatmentFor(t.author_kind).agentTag" class="ch-thread-agent-tag"
                  >AGENT</span
                >
                <span class="ch-thread-turn-time">{{ formatTimestamp(t.created_at) }}</span>
              </div>
              <!-- eslint-disable-next-line vue/no-v-html -->
              <div class="ch-thread-turn-html ch-md" v-html="t.html"></div>
            </div>
          </div>
        </template>
      </div>

      <div class="ch-thread-footer">
        <span v-if="resolvedLabel" class="ch-thread-resolved">{{ resolvedLabel }}</span>
        <span class="ch-thread-counts">{{ countsLabel }}</span>
      </div>
    </template>
  </div>
</template>

<style scoped>
.ch-thread {
  border: 1px solid var(--ch-line);
  background: var(--ch-raised);
  padding: 22px 24px;
}

.ch-thread-status {
  margin: 0;
  font-size: var(--ch-size-body);
  color: var(--ch-text-meta);
}

.ch-thread-turns {
  display: flex;
  flex-direction: column;
}

.ch-thread-turn {
  display: flex;
  gap: 13px;
  padding: 16px 0;
}

.ch-thread-turn + .ch-thread-turn,
.ch-thread-new-rule + .ch-thread-turn {
  border-top: 1px solid var(--ch-line);
}

.ch-thread-avatar {
  flex: none;
  width: 26px;
  height: 26px;
  display: flex;
  align-items: center;
  justify-content: center;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  font-weight: 600;
}

/* A person is a circle in vellum -- Chronicle's own signal colour, never
 * reserved (CLAUDE.md invariant 2 reserves only coral/gold). */
.ch-thread-avatar--circle {
  border-radius: 50%;
  background: color-mix(in srgb, var(--ch-signal) 22%, transparent);
  color: var(--ch-signal);
}

/* An agent is a square -- the board's own shape distinction for Scribe kept
 * verbatim -- in a NEUTRAL colour rather than the board's steel: steel
 * (--ch-generated) means regenerated tier-1 content, and an agent's turn is
 * authored tier 2 (CH092 freezes author_kind at write time; this is not
 * "generated" in that sense at all). The AGENT tag beside the name is what
 * actually carries the distinction. */
.ch-thread-avatar--square {
  background: color-mix(in srgb, var(--ch-text-2) 16%, transparent);
  border: 1px solid color-mix(in srgb, var(--ch-text-2) 42%, transparent);
  color: var(--ch-text-2);
}

.ch-thread-turn-body {
  min-width: 0;
  flex: 1;
}

.ch-thread-turn-meta {
  display: flex;
  align-items: baseline;
  gap: 9px;
}

.ch-thread-turn-author {
  font-size: 13.5px;
  font-weight: 600;
  color: var(--ch-text);
}

.ch-thread-agent-tag {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-2xs);
  letter-spacing: var(--ch-track-mid);
  color: var(--ch-text-2);
  border: 1px solid var(--ch-line);
  padding: 1px 5px;
}

.ch-thread-turn-time {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  color: var(--ch-text-meta);
}

.ch-thread-turn-html {
  margin-top: 6px;
  font-family: var(--ch-font-serif);
  font-size: 16.5px;
  line-height: 1.6;
  color: var(--ch-text-2);
}

.ch-thread-turn-html :deep(p) {
  margin: 0;
}

.ch-thread-turn-html :deep(p + p) {
  margin-top: 12px;
}

/* The hairline rule labelled NEW, above the first unread turn -- computed
 * from the server's own `unread` count (discussionThread.ts's
 * firstUnreadSeq), never from anything remembered locally, so a reload
 * agrees with the server by construction rather than by care taken here. */
.ch-thread-new-rule {
  display: flex;
  align-items: center;
  gap: 10px;
  padding-top: 16px;
}

.ch-thread-new-label {
  flex: none;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-2xs);
  letter-spacing: var(--ch-track-wide);
  color: var(--ch-signal);
}

.ch-thread-new-line {
  flex: 1;
  height: 1px;
  background: color-mix(in srgb, var(--ch-signal) 55%, transparent);
}

.ch-thread-footer {
  margin-top: 20px;
  padding-top: 15px;
  border-top: 1px solid var(--ch-line);
  display: flex;
  align-items: center;
  gap: 12px;
}

.ch-thread-resolved {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  letter-spacing: var(--ch-track-mid);
  color: var(--ch-resolved);
}

.ch-thread-counts {
  margin-left: auto;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  color: var(--ch-text-meta);
}
</style>
