<script setup lang="ts">
// `/app/account` (CHRN-106, board 1d): display name, sessions, invites,
// people -- four blocks, in the board's own order. This view does its own
// fetches (Tier1PageView.vue / DiscussionView.vue precedent: the smart
// route view fetches, the dumb pieces -- InviteQr, InviteReveal -- render
// what they are given).
import { computed, onMounted, ref } from 'vue'
import InviteReveal from '@/components/InviteReveal.vue'
import { api } from '@/api/client'
import { currentUser } from '@/auth'
import { formatDate } from '@/lib/format'
import { canSeePeople, isInvitedNeverSignedIn, seenLabel, sortSessions } from '@/lib/account'
import type { components } from '@/api/schema.d.ts'

type User = components['schemas']['User']
type DeviceSession = components['schemas']['DeviceSession']
type Member = components['schemas']['Member']
type Invite = components['schemas']['Invite']

// ── Account ──────────────────────────────────────────────────────────────
// Seeded from the global ref auth/index.ts already resolved before the shell
// mounted, so the block never flashes empty -- then refreshed from the
// server, since `is_owner` gates PEOPLE below and this is the one read that
// has to be current, not whatever the app booted with.
const me = ref<User | null>(currentUser.value)
const displayNameDraft = ref(currentUser.value?.display_name ?? '')
const savingName = ref(false)
const nameError = ref<string | null>(null)

async function loadMe(): Promise<void> {
  const res = await api.GET('/auth/me')
  if (res.data) {
    me.value = res.data
    currentUser.value = res.data
    displayNameDraft.value = res.data.display_name
  }
}

const canSaveName = computed(() => {
  const draft = displayNameDraft.value.trim()
  return !savingName.value && draft.length > 0 && draft !== me.value?.display_name
})

async function saveDisplayName(): Promise<void> {
  if (!canSaveName.value) return
  savingName.value = true
  nameError.value = null
  const res = await api.PATCH('/auth/me', { body: { display_name: displayNameDraft.value.trim() } })
  savingName.value = false
  if (res.data) {
    me.value = res.data
    currentUser.value = res.data
    displayNameDraft.value = res.data.display_name
    return
  }
  nameError.value = res.error?.message ?? 'Could not save that name.'
}

const kindTag = computed(() => (me.value?.kind === 'agent' ? 'AGENT' : 'PERSON'))

// ── Devices ──────────────────────────────────────────────────────────────
const sessions = ref<DeviceSession[] | null>(null)
const sessionsError = ref(false)
const confirmingRevokeId = ref<string | null>(null)
const revokingId = ref<string | null>(null)

async function loadSessions(): Promise<void> {
  const res = await api.GET('/auth/sessions')
  if (res.data) {
    sessions.value = res.data
    sessionsError.value = false
    return
  }
  sessionsError.value = true
}

const sortedSessions = computed(() => (sessions.value ? sortSessions(sessions.value) : []))

function beginRevoke(id: string): void {
  confirmingRevokeId.value = id
}

function cancelRevoke(): void {
  confirmingRevokeId.value = null
}

async function confirmRevoke(id: string): Promise<void> {
  revokingId.value = id
  await api.DELETE('/auth/sessions/{id}', { params: { path: { id } } })
  revokingId.value = null
  confirmingRevokeId.value = null
  // Refresh from the server rather than splicing the row out locally -- the
  // ticket's own Done-when: "the device list agrees with the server after a
  // reload."
  await loadSessions()
}

// ── Add device ───────────────────────────────────────────────────────────
const selfInvite = ref<Invite | null>(null)
const selfInviteError = ref(false)
const mintingSelfInvite = ref(false)

async function mintSelfInvite(): Promise<void> {
  mintingSelfInvite.value = true
  selfInviteError.value = false
  const res = await api.POST('/auth/invite')
  mintingSelfInvite.value = false
  if (res.data) {
    selfInvite.value = res.data
    return
  }
  selfInvite.value = null
  selfInviteError.value = true
}

// ── People (owner only) ─────────────────────────────────────────────────
const people = ref<Member[] | null>(null)
// Whether the block renders at ALL -- distinct from `peopleExpanded` below.
// Only flips true on a successful listUsers read; a 403 (should not happen
// once `canSeePeople(me)` gates the attempt, but the ticket is explicit:
// "if the API answers 403 anyway, render nothing rather than an error")
// leaves this false, same as never having tried.
const peopleVisible = ref(false)
const peopleExpanded = ref(false)

async function loadPeople(): Promise<void> {
  if (!me.value || !canSeePeople(me.value)) return
  const res = await api.GET('/admin/users')
  if (res.data) {
    people.value = res.data
    peopleVisible.value = true
  }
}

const memberInvites = ref<Record<string, Invite>>({})
const memberInviteBusyId = ref<string | null>(null)

async function mintMemberInvite(userId: string): Promise<void> {
  memberInviteBusyId.value = userId
  const res = await api.POST('/admin/users/{id}/invite', { params: { path: { id: userId } } })
  memberInviteBusyId.value = null
  if (res.data) {
    memberInvites.value = { ...memberInvites.value, [userId]: res.data }
  }
}

const newPersonEmail = ref('')
const newPersonBusy = ref(false)
const newPersonError = ref<string | null>(null)
const newPersonInvite = ref<Invite | null>(null)

async function createPerson(): Promise<void> {
  const email = newPersonEmail.value.trim()
  if (!email || newPersonBusy.value) return
  newPersonBusy.value = true
  newPersonError.value = null
  const res = await api.POST('/admin/users', { body: { email } })
  newPersonBusy.value = false
  if (res.data) {
    newPersonInvite.value = res.data
    newPersonEmail.value = ''
    await loadPeople()
    return
  }
  newPersonError.value = res.error?.message ?? 'Could not create that account.'
}

onMounted(async () => {
  await loadMe()
  await Promise.all([loadSessions(), mintSelfInvite(), loadPeople()])
})
</script>

<template>
  <div class="ch-account-route">
    <h1 class="ch-account-heading">Account</h1>

    <!-- ── Account ──────────────────────────────────────────────────── -->
    <section class="ch-account-section">
      <div class="ch-account-section-label">ACCOUNT</div>
      <div class="ch-account-name-row">
        <div class="ch-account-name-field">
          <div class="ch-account-field-label">DISPLAY NAME</div>
          <input
            v-model="displayNameDraft"
            class="ch-account-name-input"
            type="text"
            maxlength="200"
            :disabled="savingName"
            @keyup.enter="saveDisplayName"
          />
        </div>
        <button type="button" class="ch-account-save" :disabled="!canSaveName" @click="saveDisplayName">
          {{ savingName ? 'SAVING…' : 'SAVE' }}
        </button>
        <div class="ch-account-tags">
          <span v-if="me?.is_owner" class="ch-account-tag ch-account-tag--owner">OWNER</span>
          <span class="ch-account-tag">{{ kindTag }}</span>
        </div>
      </div>
      <p v-if="nameError" class="ch-account-error">{{ nameError }}</p>

      <div class="ch-account-identity">
        <div class="ch-account-field-label">ALSO YOUR ACCESS IDENTITY</div>
        <div class="ch-account-email">{{ me?.email }}</div>
      </div>
    </section>

    <!-- ── Devices ──────────────────────────────────────────────────── -->
    <section class="ch-account-section">
      <div class="ch-account-devices-header">
        <span class="ch-account-section-label">DEVICES</span>
        <span v-if="sessions" class="ch-account-devices-count">{{ sessions.length }} SIGNED IN</span>
      </div>
      <p v-if="sessionsError" class="ch-account-error">Could not load the device list.</p>

      <div v-else class="ch-account-devices">
        <div
          v-for="row in sortedSessions"
          :key="row.id"
          class="ch-account-device-row"
          :class="{ 'is-confirming': confirmingRevokeId === row.id }"
        >
          <div class="ch-account-device-main">
            <div class="ch-account-device-info">
              <span class="ch-account-device-label">{{ row.device_label || 'Unlabelled device' }}</span>
              <span v-if="row.current" class="ch-account-device-tag">THIS DEVICE</span>
            </div>
            <div class="ch-account-device-meta">
              SINCE {{ formatDate(row.created_at) }} · SEEN {{ seenLabel(row.last_seen_at) }}
            </div>
          </div>
          <button
            v-if="!row.current && confirmingRevokeId !== row.id"
            type="button"
            class="ch-account-revoke"
            @click="beginRevoke(row.id)"
          >
            REVOKE
          </button>

          <div v-if="confirmingRevokeId === row.id" class="ch-account-revoke-confirm">
            <span class="ch-account-revoke-confirm-text">
              Revoke {{ row.device_label || 'this device' }}? It stays signed out until someone re-adds it.
            </span>
            <span class="ch-account-revoke-confirm-actions">
              <button
                type="button"
                class="ch-account-revoke-confirm-revoke"
                :disabled="revokingId === row.id"
                @click="confirmRevoke(row.id)"
              >
                REVOKE
              </button>
              <button
                type="button"
                class="ch-account-revoke-confirm-keep"
                :disabled="revokingId === row.id"
                @click="cancelRevoke"
              >
                KEEP
              </button>
            </span>
          </div>
        </div>
      </div>
    </section>

    <!-- ── Add device ───────────────────────────────────────────────── -->
    <section class="ch-account-section">
      <h2 class="ch-account-subheading">Add device</h2>
      <InviteReveal
        v-if="selfInvite"
        :invite-token="selfInvite.invite_token"
        :sign-in-url="selfInvite.sign_in_url"
        :expires-in="selfInvite.expires_in"
      />
      <p v-else-if="selfInviteError" class="ch-account-error">Could not mint an invite.</p>
      <p v-else class="ch-account-error">Minting an invite…</p>
      <button
        type="button"
        class="ch-account-new-invite"
        :disabled="mintingSelfInvite"
        @click="mintSelfInvite"
      >
        NEW INVITE
      </button>
    </section>

    <!-- ── People (owner only) ─────────────────────────────────────── -->
    <section v-if="peopleVisible && people" class="ch-account-section">
      <button type="button" class="ch-account-people-toggle" @click="peopleExpanded = !peopleExpanded">
        <span class="ch-account-people-caret">{{ peopleExpanded ? '⌄' : '›' }}</span>
        <span class="ch-account-section-label">PEOPLE · {{ people.length }} · OWNER ONLY</span>
      </button>

      <div v-if="peopleExpanded" class="ch-account-people-expanded">
        <div v-for="member in people" :key="member.id" class="ch-account-member-row">
          <div class="ch-account-member-main">
            <div class="ch-account-member-email">{{ member.email }}</div>
            <div v-if="!isInvitedNeverSignedIn(member)" class="ch-account-member-seen">
              SEEN {{ seenLabel(member.last_seen_at) }}
            </div>
          </div>
          <span v-if="member.is_owner" class="ch-account-tag ch-account-tag--owner">OWNER</span>
          <span class="ch-account-tag">{{ member.kind === 'agent' ? 'AGENT' : 'PERSON' }}</span>

          <template v-if="isInvitedNeverSignedIn(member)">
            <div class="ch-account-member-invited-row">
              <span class="ch-account-member-invited">
                INVITED · EXPIRES {{ formatDate(member.invite_expires_at as string) }} · NOT YET SIGNED IN
              </span>
              <button
                type="button"
                class="ch-account-new-invite ch-account-member-new-invite"
                :disabled="memberInviteBusyId === member.id"
                @click="mintMemberInvite(member.id)"
              >
                NEW INVITE
              </button>
            </div>
            <InviteReveal
              v-if="memberInvites[member.id]"
              class="ch-account-member-invite-reveal"
              :invite-token="memberInvites[member.id].invite_token"
              :sign-in-url="memberInvites[member.id].sign_in_url"
              :expires-in="memberInvites[member.id].expires_in"
            />
          </template>
        </div>

        <form class="ch-account-new-person" @submit.prevent="createPerson">
          <input
            v-model="newPersonEmail"
            class="ch-account-new-person-input"
            type="email"
            placeholder="email"
            :disabled="newPersonBusy"
          />
          <button type="submit" class="ch-account-new-invite" :disabled="newPersonBusy || !newPersonEmail.trim()">
            NEW PERSON
          </button>
        </form>
        <p v-if="newPersonError" class="ch-account-error">{{ newPersonError }}</p>
        <InviteReveal
          v-if="newPersonInvite"
          class="ch-account-member-invite-reveal"
          :invite-token="newPersonInvite.invite_token"
          :sign-in-url="newPersonInvite.sign_in_url"
          :expires-in="newPersonInvite.expires_in"
        />
      </div>
    </section>
  </div>
</template>

<style scoped>
.ch-account-route {
  padding: 34px 46px 46px;
  max-width: 900px;
}

.ch-account-heading {
  margin: 0;
  font-family: var(--ch-font-serif);
  font-size: 36px;
  font-weight: 400;
  line-height: 1.16;
  color: var(--ch-text);
}

.ch-account-section {
  margin-top: 38px;
  padding-top: 20px;
  border-top: 1px solid var(--ch-line);
}

.ch-account-section-label {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-2xs);
  letter-spacing: var(--ch-track-wide);
  color: var(--ch-text-meta);
}

.ch-account-field-label {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-2xs);
  letter-spacing: 0.12em;
  color: var(--ch-text-meta);
}

.ch-account-error {
  margin: 10px 0 0;
  font-size: var(--ch-size-xs);
  color: var(--ch-text-2);
}

/* ── Account block ─────────────────────────────────────────────────── */
.ch-account-name-row {
  margin-top: 16px;
  display: flex;
  align-items: flex-end;
  gap: 14px;
}

.ch-account-name-field {
  flex: 1;
  max-width: 340px;
}

.ch-account-name-input {
  margin-top: 7px;
  width: 100%;
  height: 38px;
  border: 1px solid var(--ch-line);
  background: var(--ch-raised);
  color: var(--ch-text);
  font-size: var(--ch-size-md);
  padding: 0 12px;
}

.ch-account-name-input:focus {
  outline: none;
  border-color: var(--ch-signal);
}

.ch-account-save {
  height: 38px;
  padding: 0 16px;
  border: 1px solid var(--ch-line);
  background: transparent;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  letter-spacing: 0.11em;
  color: var(--ch-text-2);
  cursor: pointer;
}

.ch-account-save:hover:not(:disabled) {
  color: var(--ch-signal);
  border-color: var(--ch-signal);
}

.ch-account-save:disabled {
  color: var(--ch-text-meta);
  cursor: default;
}

.ch-account-tags {
  margin-left: auto;
  display: flex;
  gap: 7px;
}

.ch-account-tag {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-2xs);
  letter-spacing: 0.1em;
  color: var(--ch-text-2);
  border: 1px solid var(--ch-line);
  padding: 4px 9px;
}

.ch-account-tag--owner {
  color: var(--ch-signal);
  border-color: color-mix(in srgb, var(--ch-signal) 40%, transparent);
}

.ch-account-identity {
  margin-top: 18px;
}

.ch-account-email {
  margin-top: 7px;
  font-family: var(--ch-font-mono);
  font-size: 13.5px;
  color: var(--ch-text-2);
}

/* ── Devices block ─────────────────────────────────────────────────── */
.ch-account-devices-header {
  display: flex;
  align-items: baseline;
  gap: 12px;
}

.ch-account-devices-count {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-2xs);
  color: var(--ch-text-meta);
}

.ch-account-devices {
  margin-top: 14px;
  display: flex;
  flex-direction: column;
}

.ch-account-device-row {
  display: flex;
  align-items: center;
  gap: 16px;
  padding: 15px 2px;
  border-top: 1px solid var(--ch-line);
  flex-wrap: wrap;
}

.ch-account-device-row.is-confirming {
  background: var(--ch-raised);
  padding: 15px 16px;
  flex-direction: column;
  align-items: stretch;
}

.ch-account-device-main {
  min-width: 0;
  flex: 1;
}

.ch-account-device-info {
  display: flex;
  align-items: center;
  gap: 10px;
}

.ch-account-device-label {
  font-size: var(--ch-size-md);
  color: var(--ch-text);
}

.ch-account-device-tag {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-2xs);
  letter-spacing: 0.1em;
  color: var(--ch-base);
  background: var(--ch-signal);
  padding: 2px 7px;
}

.ch-account-device-meta {
  margin-top: 6px;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  color: var(--ch-text-meta);
}

.ch-account-revoke {
  flex: none;
  border: none;
  background: transparent;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  letter-spacing: 0.11em;
  color: var(--ch-text-2);
  cursor: pointer;
}

.ch-account-revoke:hover {
  color: var(--ch-signal);
}

.ch-account-revoke-confirm {
  width: 100%;
  margin-top: 13px;
  padding-top: 13px;
  border-top: 1px solid var(--ch-line);
  display: flex;
  align-items: center;
  gap: 16px;
  flex-wrap: wrap;
}

.ch-account-revoke-confirm-text {
  font-size: 13.5px;
  color: var(--ch-text-2);
}

.ch-account-revoke-confirm-actions {
  margin-left: auto;
  display: flex;
  gap: 9px;
}

.ch-account-revoke-confirm-revoke,
.ch-account-revoke-confirm-keep {
  height: 32px;
  padding: 0 14px;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  letter-spacing: 0.11em;
  cursor: pointer;
}

.ch-account-revoke-confirm-revoke {
  border: none;
  background: var(--ch-signal);
  font-weight: 600;
  color: var(--ch-base);
}

.ch-account-revoke-confirm-keep {
  border: 1px solid var(--ch-line);
  background: transparent;
  color: var(--ch-text-2);
}

.ch-account-revoke-confirm-revoke:disabled,
.ch-account-revoke-confirm-keep:disabled {
  opacity: 0.6;
  cursor: default;
}

/* ── Add device block ─────────────────────────────────────────────── */
.ch-account-subheading {
  margin: 0 0 8px;
  font-size: 19px;
  font-weight: 600;
  color: var(--ch-text);
}

.ch-account-new-invite {
  margin-top: 18px;
  border: none;
  background: transparent;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  letter-spacing: 0.11em;
  color: var(--ch-text-2);
  cursor: pointer;
  padding: 0;
}

.ch-account-new-invite:hover:not(:disabled) {
  color: var(--ch-signal);
}

.ch-account-new-invite:disabled {
  color: var(--ch-text-meta);
  cursor: default;
}

/* ── People block ──────────────────────────────────────────────────── */
.ch-account-people-toggle {
  display: flex;
  align-items: center;
  gap: 12px;
  border: none;
  background: transparent;
  padding: 0;
  cursor: pointer;
}

.ch-account-people-caret {
  font-family: var(--ch-font-mono);
  font-size: 11px;
  color: var(--ch-text-meta);
}

.ch-account-people-expanded {
  margin-top: 14px;
  display: flex;
  flex-direction: column;
}

.ch-account-member-row {
  padding: 13px 0;
  border-top: 1px solid var(--ch-line);
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 12px;
}

.ch-account-member-main {
  min-width: 0;
  flex: 1;
}

.ch-account-member-email {
  font-family: var(--ch-font-mono);
  font-size: 12.5px;
  color: var(--ch-text);
}

.ch-account-member-seen {
  margin-top: 5px;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-2xs);
  color: var(--ch-text-meta);
}

.ch-account-member-invited-row {
  width: 100%;
  margin-top: 9px;
  display: flex;
  align-items: center;
  gap: 12px;
}

.ch-account-member-invited {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-2xs);
  color: var(--ch-text-meta);
}

.ch-account-member-new-invite {
  margin-top: 0;
  margin-left: auto;
}

.ch-account-member-invite-reveal {
  width: 100%;
  margin-top: 14px;
}

.ch-account-new-person {
  margin-top: 18px;
  padding-top: 16px;
  border-top: 1px solid var(--ch-line);
  display: flex;
  gap: 10px;
}

.ch-account-new-person-input {
  flex: 1;
  height: 36px;
  border: 1px solid var(--ch-line);
  background: var(--ch-raised);
  color: var(--ch-text);
  padding: 0 12px;
  font-size: var(--ch-size-body);
}

.ch-account-new-person-input:focus {
  outline: none;
  border-color: var(--ch-signal);
}

.ch-account-new-person .ch-account-new-invite {
  flex: none;
  height: 36px;
  padding: 0 14px;
  margin-top: 0;
  border: 1px solid var(--ch-line);
}
</style>
