<script setup lang="ts">
// `/app/discussions` (CHRN-57): the sidebar's DISCUSSIONS row lands here.
//
// `listDiscussions` (openapi.yaml) takes a REQUIRED `page` query param — "The
// threads filed against a page", not a corpus-wide index — and its own
// comment says why there is no whole-corpus listing: "A page-independent
// list needs a store query this ticket [CHRN-99] does not add." So this view
// is two things, not one list:
//
//   1. UNREAD — `listUnread`, which is genuinely account-wide and needs no
//      page. This is the reachable list the sidebar badge promises.
//   2. BROWSE A PAGE — pick a page (the tree AppShell already fetched) and
//      call `listDiscussions` for it, which is what actually satisfies "the
//      list: listDiscussions rows, unread ones marked, RESOLVED state where
//      resolved" from a page that has threads filed against it.
//
// `DiscussionSummary`/`Discussion` carry no reply/participant counts (only
// `getDiscussion` does, via its `turns`/`participants` arrays) — an N+1
// `getDiscussion` per row to backfill them is not worth it for a list, so
// this list omits counts. Named here and in the PR as a deviation from the
// ticket's row description.
import { computed, inject, ref, watch } from 'vue'
import { RouterLink, useRouter } from 'vue-router'
import { api } from '@/api/client'
import { formatTimestamp } from '@/lib/format'
import { PAGE_PATHS_KEY } from '@/layouts/shellData'
import type { components } from '@/api/schema.d.ts'

type Discussion = components['schemas']['Discussion']
type UnreadItem = components['schemas']['UnreadItem']

const router = useRouter()
const pagePaths = inject(PAGE_PATHS_KEY, ref<string[] | null>(null))

// ── Unread ────────────────────────────────────────────────────────────────
const unread = ref<UnreadItem[] | null>(null)
const unreadError = ref(false)

async function loadUnread(): Promise<void> {
  unreadError.value = false
  const res = await api.GET('/discussions/unread')
  if (res.data) {
    unread.value = res.data.items
    return
  }
  unread.value = null
  unreadError.value = true
}
loadUnread()

const unreadRefs = computed(() => new Set((unread.value ?? []).map((u) => u.ref)))

// ── Browse a page ────────────────────────────────────────────────────────
const selectedPage = ref('')
const pageDiscussions = ref<Discussion[] | null>(null)
const pageLoading = ref(false)
const pageError = ref<string | null>(null)

let browseSeq = 0
async function browse(page: string): Promise<void> {
  const seq = ++browseSeq
  pageDiscussions.value = null
  pageError.value = null
  if (!page) return
  pageLoading.value = true
  const res = await api.GET('/discussions', { params: { query: { page } } })
  if (seq !== browseSeq) return // superseded by a later selection
  pageLoading.value = false
  if (res.data) {
    pageDiscussions.value = res.data.items
    return
  }
  pageError.value =
    res.response.status === 404 ? `No page at ${page}.` : (res.error?.message ?? 'Could not list threads.')
}
watch(selectedPage, browse)

// ── New discussion ───────────────────────────────────────────────────────
// `openDiscussion` needs only title + body (page optional) — nothing here a
// member session cannot supply, so the form is real, not a stub.
const showNewForm = ref(false)
const newTitle = ref('')
const newBody = ref('')
const newPage = ref('')
const creating = ref(false)
const createError = ref<string | null>(null)

async function createDiscussion(): Promise<void> {
  if (!newTitle.value.trim() || !newBody.value.trim()) return
  creating.value = true
  createError.value = null
  const res = await api.POST('/discussions', {
    body: {
      title: newTitle.value.trim(),
      body: newBody.value.trim(),
      ...(newPage.value ? { page: newPage.value } : {}),
    },
  })
  creating.value = false
  if (res.data) {
    router.push(`/discussions/${res.data.discussion.ref}`)
    return
  }
  createError.value = res.error?.message ?? 'Could not open the thread.'
}
</script>

<template>
  <div class="ch-discussions-route">
    <h1 class="ch-discussions-heading">Discussions</h1>

    <section class="ch-discussions-section">
      <div class="ch-discussions-section-label">UNREAD</div>
      <p v-if="unreadError" class="ch-discussions-status">Could not load.</p>
      <p v-else-if="unread && unread.length === 0" class="ch-discussions-status">Nothing unread.</p>
      <ul v-else-if="unread" class="ch-discussions-rows">
        <li v-for="item in unread" :key="item.ref">
          <RouterLink :to="`/discussions/${item.ref}`" class="ch-discussions-row ch-discussions-row--unread">
            <span class="ch-discussions-dot" aria-hidden="true"></span>
            <span class="ch-discussions-ref">{{ item.ref }}</span>
            <span class="ch-discussions-title">{{ item.title }}</span>
            <span class="ch-discussions-unread-count">{{ item.unread }} unread</span>
          </RouterLink>
        </li>
      </ul>
    </section>

    <section class="ch-discussions-section">
      <div class="ch-discussions-section-label">BROWSE A PAGE</div>
      <select v-model="selectedPage" class="ch-discussions-page-picker">
        <option value="">Pick a page…</option>
        <option v-for="p in pagePaths ?? []" :key="p" :value="p">{{ p }}</option>
      </select>

      <p v-if="pageLoading" class="ch-discussions-status">Loading…</p>
      <p v-else-if="pageError" class="ch-discussions-status">{{ pageError }}</p>
      <p v-else-if="selectedPage && pageDiscussions && pageDiscussions.length === 0" class="ch-discussions-status">
        No threads filed against this page.
      </p>
      <ul v-else-if="pageDiscussions" class="ch-discussions-rows">
        <li v-for="d in pageDiscussions" :key="d.ref">
          <RouterLink
            :to="`/discussions/${d.ref}`"
            class="ch-discussions-row"
            :class="{ 'ch-discussions-row--unread': unreadRefs.has(d.ref) }"
          >
            <span v-if="unreadRefs.has(d.ref)" class="ch-discussions-dot" aria-hidden="true"></span>
            <span class="ch-discussions-ref">{{ d.ref }}</span>
            <span class="ch-discussions-title">{{ d.title }}</span>
            <span v-if="d.resolved" class="ch-discussions-resolved-tag">RESOLVED</span>
            <span class="ch-discussions-created">{{ formatTimestamp(d.created_at) }}</span>
          </RouterLink>
        </li>
      </ul>
    </section>

    <section class="ch-discussions-section">
      <button
        v-if="!showNewForm"
        type="button"
        class="ch-discussions-new-toggle"
        @click="showNewForm = true"
      >
        + New discussion
      </button>
      <form v-else class="ch-discussions-new-form" @submit.prevent="createDiscussion">
        <input v-model="newTitle" placeholder="Title" required maxlength="200" />
        <textarea v-model="newBody" placeholder="Opening turn…" rows="4" required></textarea>
        <select v-model="newPage">
          <option value="">No page (unfiled)</option>
          <option v-for="p in pagePaths ?? []" :key="p" :value="p">{{ p }}</option>
        </select>
        <div class="ch-discussions-new-form-row">
          <button type="submit" :disabled="creating">{{ creating ? 'Opening…' : 'Open thread' }}</button>
          <button type="button" @click="showNewForm = false">Cancel</button>
        </div>
        <p v-if="createError" class="ch-discussions-status">{{ createError }}</p>
      </form>
    </section>
  </div>
</template>

<style scoped>
.ch-discussions-route {
  padding: 34px 46px;
  max-width: 900px;
}

.ch-discussions-heading {
  margin: 0;
  font-family: var(--ch-font-serif);
  font-size: 32px;
  font-weight: 400;
  color: var(--ch-text);
}

.ch-discussions-section {
  margin-top: 30px;
}

.ch-discussions-section-label {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-2xs);
  letter-spacing: var(--ch-track-wide);
  color: var(--ch-text-meta);
  margin-bottom: 10px;
}

.ch-discussions-status {
  margin: 0;
  font-size: var(--ch-size-body);
  color: var(--ch-text-meta);
}

.ch-discussions-page-picker,
.ch-discussions-new-form select {
  height: 32px;
  border: 1px solid var(--ch-line);
  background: var(--ch-raised);
  color: var(--ch-text);
  padding: 0 10px;
  margin-bottom: 14px;
}

.ch-discussions-rows {
  margin: 0;
  padding: 0;
  list-style: none;
  border-top: 1px solid var(--ch-line);
}

.ch-discussions-row {
  display: flex;
  align-items: baseline;
  gap: 14px;
  padding: 12px 2px;
  border-bottom: 1px solid var(--ch-line);
  text-decoration: none;
}

.ch-discussions-dot {
  flex: none;
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--ch-signal);
}

.ch-discussions-row--unread .ch-discussions-title {
  font-weight: 600;
  color: var(--ch-text);
}

.ch-discussions-ref {
  flex: none;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-sm);
  color: var(--ch-signal);
}

.ch-discussions-title {
  flex: 1;
  min-width: 0;
  font-size: var(--ch-size-md);
  color: var(--ch-text-2);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ch-discussions-unread-count,
.ch-discussions-created {
  flex: none;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  color: var(--ch-text-meta);
}

.ch-discussions-resolved-tag {
  flex: none;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-2xs);
  letter-spacing: var(--ch-track-mid);
  color: var(--ch-resolved);
}

.ch-discussions-new-toggle {
  border: 1px dashed var(--ch-line);
  background: transparent;
  color: var(--ch-text-2);
  padding: 10px 14px;
  cursor: pointer;
  font-size: var(--ch-size-body);
}

.ch-discussions-new-toggle:hover {
  color: var(--ch-text);
  border-color: var(--ch-text-meta);
}

.ch-discussions-new-form {
  display: flex;
  flex-direction: column;
  gap: 10px;
  max-width: 460px;
}

.ch-discussions-new-form input,
.ch-discussions-new-form textarea {
  border: 1px solid var(--ch-line);
  background: var(--ch-raised);
  color: var(--ch-text);
  padding: 8px 10px;
  font-size: var(--ch-size-body);
}

.ch-discussions-new-form-row {
  display: flex;
  gap: 10px;
}

.ch-discussions-new-form-row button {
  border: 1px solid var(--ch-line);
  background: var(--ch-base);
  color: var(--ch-text);
  padding: 8px 14px;
  cursor: pointer;
}

.ch-discussions-new-form-row button:disabled {
  color: var(--ch-text-meta);
  cursor: default;
}
</style>
