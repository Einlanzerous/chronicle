<script setup lang="ts">
// `/app/discussions/:ref` (CHRN-57): a thread page -- header, the turns +
// footer that DiscussionThread.vue renders (factored out for CHRN-56's
// "open questions" embed), a reply composer, participants, and the
// resolve-into-a-note action.
//
// This view does its OWN fetch (Tier1PageView.vue's precedent: the smart
// route view fetches, the dumb component renders what it is given) and
// always passes DiscussionThread an already-loaded `Thread` object rather
// than a ref string -- one GET per load, not two -- because this view also
// needs the full object for its header, composer and participants panel.
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import DiscussionThread from '@/components/DiscussionThread.vue'
import { api } from '@/api/client'
import { canReply, initialsFor, threadCountsLabel } from '@/lib/discussionThread'
import type { components } from '@/api/schema.d.ts'

type Thread = components['schemas']['Thread']
type ResolveInto = components['schemas']['ResolveDiscussionRequest']['into']

const route = useRoute()
const router = useRouter()

const discussionRef = computed(() => String(route.params.ref))
const thread = ref<Thread | null>(null)
const loading = ref(true)
const loadError = ref<string | null>(null)

// Bumped on every load and captured per-request (CHRN-58's Tier1PageView.vue
// / SearchView.vue precedent), so a slower answer to an earlier ref cannot
// land after a faster one and show the wrong thread under the current URL.
let loadSeq = 0

async function load(ref_: string): Promise<void> {
  const seq = ++loadSeq
  loading.value = true
  loadError.value = null
  const res = await api.GET('/discussions/{ref}', { params: { path: { ref: ref_ } } })
  if (seq !== loadSeq) return // superseded by a later navigation
  loading.value = false
  if (res.data) {
    thread.value = res.data
    return
  }
  thread.value = null
  loadError.value =
    res.response.status === 404 ? `No thread at ${ref_}.` : (res.error?.message ?? 'This thread could not be read.')
}

watch(discussionRef, load, { immediate: true })

const headerCounts = computed(() => (thread.value ? threadCountsLabel(thread.value.turns, thread.value.participants) : ''))

// ── Reply ────────────────────────────────────────────────────────────────
const replyBody = ref('')
const replying = ref(false)
const replyError = ref<string | null>(null)

async function submitReply(): Promise<void> {
  if (!thread.value || !replyBody.value.trim()) return
  replying.value = true
  replyError.value = null
  const res = await api.POST('/discussions/{ref}/turns', {
    params: { path: { ref: thread.value.discussion.ref } },
    body: { body: replyBody.value.trim() },
  })
  replying.value = false
  if (res.data) {
    replyBody.value = ''
    await load(thread.value.discussion.ref)
    return
  }
  // Shown verbatim -- the ticket is explicit that a refusal (409
  // agent_after_agent / discussion_resolved / thread_busy) must not be
  // swallowed.
  replyError.value = res.error?.message ?? 'The reply was refused.'
}

// ── Participants ─────────────────────────────────────────────────────────
// `addParticipant` needs a `user_id` (ParticipantRequest), and the only
// member-reachable source of one is a participant ALREADY on this thread's
// own history -- `GET /admin/users`, the account directory, is owner-only
// (x-chronicle-policy: owner), so there is no way for this screen to pick
// someone who has never been on the thread. Re-adding someone removed is
// therefore offered; adding an arbitrary new person is not -- named as a
// deviation in the PR rather than left silent.
const participantBusyId = ref<string | null>(null)
const participantError = ref<string | null>(null)

async function removeParticipant(userId: string): Promise<void> {
  if (!thread.value) return
  participantBusyId.value = userId
  participantError.value = null
  const res = await api.DELETE('/discussions/{ref}/participants/{id}', {
    params: { path: { ref: thread.value.discussion.ref, id: userId } },
  })
  participantBusyId.value = null
  if (!res.error) {
    await load(thread.value.discussion.ref)
    return
  }
  participantError.value = res.error.message ?? 'Could not remove that participant.'
}

async function readdParticipant(userId: string): Promise<void> {
  if (!thread.value) return
  participantBusyId.value = userId
  participantError.value = null
  const res = await api.POST('/discussions/{ref}/participants', {
    params: { path: { ref: thread.value.discussion.ref } },
    body: { user_id: userId },
  })
  participantBusyId.value = null
  if (!res.error) {
    await load(thread.value.discussion.ref)
    return
  }
  participantError.value = res.error.message ?? 'Could not add that participant back.'
}

// ── Resolve ──────────────────────────────────────────────────────────────
const canResolve = computed(() => !thread.value?.discussion.resolved?.note)
const showResolveForm = ref(false)
const resolveInto = ref<ResolveInto>('new_note')
const resolveTitle = ref('')
const resolvePage = ref('')
const resolveNoteRef = ref('')
const resolveBody = ref('')
const resolving = ref(false)
const resolveError = ref<string | null>(null)

async function submitResolve(): Promise<void> {
  if (!thread.value) return
  resolving.value = true
  resolveError.value = null

  const body: components['schemas']['ResolveDiscussionRequest'] = { into: resolveInto.value }
  if (resolveInto.value === 'new_note') {
    body.title = resolveTitle.value.trim()
    body.page = resolvePage.value.trim() || thread.value.discussion.page
  } else if (resolveInto.value === 'existing_note') {
    body.note_ref = resolveNoteRef.value.trim()
    if (resolveTitle.value.trim()) body.title = resolveTitle.value.trim()
  }
  if (resolveInto.value !== 'nothing' && resolveBody.value.trim()) body.body = resolveBody.value.trim()

  const res = await api.POST('/discussions/{ref}/resolve', {
    params: { path: { ref: thread.value.discussion.ref } },
    body,
  })
  resolving.value = false
  if (res.data) {
    showResolveForm.value = false
    if (res.data.note) {
      // "on success navigate to /app/notes/<ref>" -- CHRN-56's placeholder
      // today, which the ticket says is fine.
      router.push(`/notes/${res.data.note.ref}`)
      return
    }
    // `into: "nothing"` -- no note produced, nowhere to navigate. Reload in
    // place so the footer picks up the RESOLVED marker.
    await load(thread.value.discussion.ref)
    return
  }
  resolveError.value = res.error?.message ?? 'Could not resolve this thread.'
}
</script>

<template>
  <div class="ch-discussion-route">
    <p v-if="loading" class="ch-discussion-status">Loading…</p>
    <p v-else-if="loadError" class="ch-discussion-status">{{ loadError }}</p>

    <template v-else-if="thread">
      <div class="ch-discussion-header">
        <h1 class="ch-discussion-title">{{ thread.discussion.title }}</h1>
        <div class="ch-discussion-header-meta">
          <span class="ch-discussion-handle">{{ thread.discussion.ref }}</span>
          <span class="ch-discussion-header-counts">{{ headerCounts }}</span>
        </div>
      </div>

      <DiscussionThread :source="thread" class="ch-discussion-thread" />

      <form
        v-if="canReply(thread.discussion)"
        class="ch-discussion-composer"
        @submit.prevent="submitReply"
      >
        <textarea
          v-model="replyBody"
          class="ch-discussion-composer-input"
          placeholder="Reply…"
          rows="3"
          :disabled="replying"
        ></textarea>
        <div class="ch-discussion-composer-row">
          <button type="submit" :disabled="replying || !replyBody.trim()">
            {{ replying ? 'Posting…' : 'Reply' }}
          </button>
        </div>
        <p v-if="replyError" class="ch-discussion-error">{{ replyError }}</p>
      </form>
      <p v-else class="ch-discussion-composer-closed">
        RESOLVED — a resolved thread takes no more turns. Open a new one to continue, and cite this one.
      </p>

      <section class="ch-discussion-participants">
        <div class="ch-discussion-section-label">PARTICIPANTS</div>
        <ul class="ch-discussion-participant-list">
          <li
            v-for="p in thread.participants"
            :key="p.user_id"
            class="ch-discussion-participant"
            :class="{ 'is-removed': p.removed_at }"
          >
            <span class="ch-discussion-participant-avatar">{{ initialsFor(p.display_name) }}</span>
            <span class="ch-discussion-participant-name">{{ p.display_name }}</span>
            <span v-if="p.kind === 'agent'" class="ch-discussion-participant-tag">AGENT</span>
            <span v-if="p.removed_at" class="ch-discussion-participant-removed">REMOVED</span>
            <button
              v-if="!p.removed_at"
              type="button"
              class="ch-discussion-participant-action"
              :disabled="participantBusyId === p.user_id"
              @click="removeParticipant(p.user_id)"
            >
              Remove
            </button>
            <button
              v-else
              type="button"
              class="ch-discussion-participant-action"
              :disabled="participantBusyId === p.user_id"
              @click="readdParticipant(p.user_id)"
            >
              Add back
            </button>
          </li>
        </ul>
        <p v-if="participantError" class="ch-discussion-error">{{ participantError }}</p>
        <p class="ch-discussion-participants-note">
          Adding someone who has never been on this thread needs an account directory this session cannot
          read (<code>GET /admin/users</code> is owner-only) — only re-adding someone removed is offered.
        </p>
      </section>

      <section class="ch-discussion-resolve">
        <button
          v-if="!showResolveForm && canResolve"
          type="button"
          class="ch-discussion-resolve-toggle"
          @click="showResolveForm = true"
        >
          Resolve → note
        </button>
        <form v-if="showResolveForm" class="ch-discussion-resolve-form" @submit.prevent="submitResolve">
          <label
            ><input type="radio" value="new_note" v-model="resolveInto" /> New note</label
          >
          <label
            ><input type="radio" value="existing_note" v-model="resolveInto" /> Existing note</label
          >
          <label
            ><input type="radio" value="nothing" v-model="resolveInto" /> No note — just record the
            conclusion</label
          >

          <template v-if="resolveInto === 'new_note'">
            <input v-model="resolveTitle" placeholder="Note title" required maxlength="200" />
            <input v-model="resolvePage" :placeholder="thread.discussion.page ?? 'Page path'" />
          </template>
          <template v-else-if="resolveInto === 'existing_note'">
            <input v-model="resolveNoteRef" placeholder="CHR-0311" required />
            <input v-model="resolveTitle" placeholder="Title (optional — keeps the note's own)" />
          </template>
          <textarea
            v-if="resolveInto !== 'nothing'"
            v-model="resolveBody"
            placeholder="What the thread concluded…"
            rows="3"
          ></textarea>

          <div class="ch-discussion-resolve-form-row">
            <button type="submit" :disabled="resolving">{{ resolving ? 'Resolving…' : 'Resolve' }}</button>
            <button type="button" @click="showResolveForm = false">Cancel</button>
          </div>
          <p v-if="resolveError" class="ch-discussion-error">{{ resolveError }}</p>
        </form>
      </section>
    </template>
  </div>
</template>

<style scoped>
.ch-discussion-route {
  padding: 34px 46px;
  max-width: 720px;
}

.ch-discussion-status {
  margin: 0;
  font-size: var(--ch-size-body);
  color: var(--ch-text-meta);
}

.ch-discussion-title {
  margin: 0;
  font-family: var(--ch-font-serif);
  font-size: 30px;
  font-weight: 400;
  color: var(--ch-text);
}

.ch-discussion-header-meta {
  margin-top: 10px;
  display: flex;
  align-items: baseline;
  gap: 14px;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  letter-spacing: var(--ch-track-mid);
}

.ch-discussion-handle {
  color: var(--ch-signal);
}

.ch-discussion-header-counts {
  color: var(--ch-text-meta);
}

.ch-discussion-thread {
  margin-top: 22px;
}

.ch-discussion-composer {
  margin-top: 18px;
}

.ch-discussion-composer-input {
  width: 100%;
  border: 1px solid var(--ch-line);
  background: var(--ch-raised);
  color: var(--ch-text);
  padding: 10px 12px;
  font-family: var(--ch-font-sans);
  font-size: var(--ch-size-body);
  resize: vertical;
}

.ch-discussion-composer-row {
  margin-top: 10px;
  display: flex;
}

.ch-discussion-composer-row button {
  border: 1px solid var(--ch-line);
  background: var(--ch-base);
  color: var(--ch-text);
  padding: 8px 16px;
  cursor: pointer;
}

.ch-discussion-composer-row button:disabled {
  color: var(--ch-text-meta);
  cursor: default;
}

/* A one-line mono explanation, per the ticket, in place of the composer --
 * never just a missing form with no reason given. */
.ch-discussion-composer-closed {
  margin-top: 18px;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  letter-spacing: var(--ch-track-mid);
  color: var(--ch-text-meta);
}

.ch-discussion-error {
  margin: 10px 0 0;
  font-size: var(--ch-size-xs);
  color: var(--ch-text-2);
}

.ch-discussion-participants,
.ch-discussion-resolve {
  margin-top: 30px;
}

.ch-discussion-section-label {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-2xs);
  letter-spacing: var(--ch-track-wide);
  color: var(--ch-text-meta);
  margin-bottom: 10px;
}

.ch-discussion-participant-list {
  margin: 0;
  padding: 0;
  list-style: none;
}

.ch-discussion-participant {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 7px 0;
}

.ch-discussion-participant.is-removed {
  opacity: 0.55;
}

.ch-discussion-participant-avatar {
  flex: none;
  width: 20px;
  height: 20px;
  border-radius: 50%;
  display: flex;
  align-items: center;
  justify-content: center;
  font-family: var(--ch-font-mono);
  font-size: 9px;
  font-weight: 600;
  background: color-mix(in srgb, var(--ch-signal) 22%, transparent);
  color: var(--ch-signal);
}

.ch-discussion-participant-name {
  font-size: var(--ch-size-body);
  color: var(--ch-text-2);
}

.ch-discussion-participant-tag,
.ch-discussion-participant-removed {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-2xs);
  letter-spacing: var(--ch-track-mid);
  color: var(--ch-text-meta);
  border: 1px solid var(--ch-line);
  padding: 1px 5px;
}

.ch-discussion-participant-action {
  margin-left: auto;
  background: none;
  border: none;
  color: var(--ch-text-meta);
  font-size: var(--ch-size-xs);
  cursor: pointer;
  text-decoration: underline;
}

.ch-discussion-participant-action:disabled {
  cursor: default;
  text-decoration: none;
}

.ch-discussion-participants-note {
  margin-top: 10px;
  font-size: var(--ch-size-xs);
  line-height: 1.5;
  color: var(--ch-text-meta);
}

.ch-discussion-participants-note code {
  font-family: var(--ch-font-mono);
}

.ch-discussion-resolve-toggle {
  border: 1px solid var(--ch-resolved);
  background: transparent;
  color: var(--ch-resolved);
  padding: 8px 14px;
  cursor: pointer;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  letter-spacing: var(--ch-track-mid);
}

.ch-discussion-resolve-form {
  display: flex;
  flex-direction: column;
  gap: 10px;
  max-width: 420px;
  font-size: var(--ch-size-body);
}

.ch-discussion-resolve-form label {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: var(--ch-size-xs);
  color: var(--ch-text-2);
}

.ch-discussion-resolve-form input[type='text'],
.ch-discussion-resolve-form input:not([type='radio']),
.ch-discussion-resolve-form textarea {
  border: 1px solid var(--ch-line);
  background: var(--ch-raised);
  color: var(--ch-text);
  padding: 8px 10px;
  font-size: var(--ch-size-body);
}

.ch-discussion-resolve-form-row {
  display: flex;
  gap: 10px;
}

.ch-discussion-resolve-form-row button {
  border: 1px solid var(--ch-line);
  background: var(--ch-base);
  color: var(--ch-text);
  padding: 8px 14px;
  cursor: pointer;
}
</style>
