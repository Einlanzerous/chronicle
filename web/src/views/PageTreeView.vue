<script setup lang="ts">
// `/app/pages/:path*` (CHRN-58): a page's live notes, tier 2. `page` on
// `/notes` is REQUIRED and non-empty (openapi.yaml's PagePath parameter),
// so there is no listNotes call for the bare `/app/pages/` root -- that
// state ("the root page list") renders the page tree's own root entries
// instead, reusing AppShell's already-fetched path list via injection
// rather than a second GET /pages.
import { computed, inject, ref, watch } from 'vue'
import { RouterLink, useRoute, useRouter } from 'vue-router'
import { buildPageTree, formatMovedNotice } from '@/lib/pageTree'
import { formatTimestamp } from '@/lib/format'
import { api } from '@/api/client'
import { PAGE_PATHS_ERROR_KEY, PAGE_PATHS_KEY } from '@/layouts/shellData'
import type { components } from '@/api/schema.d.ts'

type NoteSummary = components['schemas']['NoteSummary']

const route = useRoute()
const router = useRouter()

const pagePaths = inject(PAGE_PATHS_KEY, ref<string[] | null>(null))
const pagePathsError = inject(PAGE_PATHS_ERROR_KEY, ref(false))
const rootPages = computed(() => buildPageTree(pagePaths.value ?? []))

const path = computed(() => {
  const raw = route.params.pathMatch
  return Array.isArray(raw) ? raw.filter(Boolean).join('/') : (raw ?? '')
})

const notes = ref<NoteSummary[] | null>(null)
const loading = ref(false)
const notFound = ref(false)
const errorMessage = ref<string | null>(null)
const movedNotice = ref<string | null>(null)

// Set right before a router.replace() this component itself triggers (the
// moved_from correction below), so the watcher does not re-fetch data it
// already has for the canonical path that replace navigates to.
let skipNextLoad = false

// Bumped on every load and captured per-request, so a slower answer to an
// earlier path cannot land after a faster answer to a later one and show
// stale notes under the new breadcrumb (two sidebar rows clicked quickly).
let loadSeq = 0

async function loadNotes(requestedPath: string): Promise<void> {
  const seq = ++loadSeq
  loading.value = true
  notFound.value = false
  errorMessage.value = null
  movedNotice.value = null
  const res = await api.GET('/notes', { params: { query: { page: requestedPath } } })
  if (seq !== loadSeq) return // superseded by a later request
  loading.value = false
  if (res.data) {
    notes.value = res.data.items
    if (res.data.moved_from && res.data.page.path !== requestedPath) {
      movedNotice.value = formatMovedNotice(res.data.moved_from, res.data.page.path)
      skipNextLoad = true
      router.replace(`/pages/${res.data.page.path}`)
    }
    return
  }
  notes.value = null
  if (res.response.status === 404) {
    notFound.value = true
    return
  }
  errorMessage.value = res.error?.message ?? 'This page could not be read.'
}

watch(
  path,
  (p) => {
    if (skipNextLoad) {
      skipNextLoad = false
      return
    }
    notes.value = null
    if (p) loadNotes(p)
  },
  { immediate: true },
)

const breadcrumbSegments = computed(() => (path.value ? path.value.split('/') : []))
</script>

<template>
  <div class="ch-page-route">
    <template v-if="!path">
      <div class="ch-page-breadcrumb">pages</div>
      <h1 class="ch-page-heading">Pages</h1>
      <p v-if="pagePathsError" class="ch-page-empty">The corpus could not be read.</p>
      <p v-else-if="rootPages.length === 0" class="ch-page-empty">No pages yet.</p>
      <ul v-else class="ch-page-root-list">
        <li v-for="node in rootPages" :key="node.path">
          <RouterLink :to="`/pages/${node.path}`" class="ch-page-root-link">{{
            node.label
          }}</RouterLink>
        </li>
      </ul>
    </template>

    <template v-else>
      <div class="ch-page-breadcrumb">
        <template v-for="(segment, i) in breadcrumbSegments" :key="i">
          <span v-if="i > 0" class="ch-page-breadcrumb-sep">/</span>
          <span :class="{ 'ch-page-breadcrumb-current': i === breadcrumbSegments.length - 1 }">{{
            segment
          }}</span>
        </template>
      </div>

      <p v-if="movedNotice" class="ch-page-moved-notice">{{ movedNotice }}</p>

      <p v-if="loading" class="ch-page-status">Loading…</p>
      <p v-else-if="notFound" class="ch-page-status">No page at this path.</p>
      <p v-else-if="errorMessage" class="ch-page-status">{{ errorMessage }}</p>
      <template v-else-if="notes">
        <p v-if="notes.length === 0" class="ch-page-empty">No notes on this page yet.</p>
        <ul v-else class="ch-page-notes">
          <li v-for="note in notes" :key="note.ref" class="ch-page-note-row">
            <RouterLink :to="`/notes/${note.ref}`" class="ch-page-note-link">
              <span class="ch-page-note-ref">{{ note.ref }}</span>
              <span class="ch-page-note-title">{{ note.title }}</span>
              <span class="ch-page-note-updated">{{ formatTimestamp(note.updated_at) }}</span>
            </RouterLink>
          </li>
        </ul>
      </template>
    </template>
  </div>
</template>

<style scoped>
.ch-page-route {
  padding: 34px 46px;
  max-width: 900px;
}

.ch-page-breadcrumb {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  letter-spacing: var(--ch-track-mid);
  color: var(--ch-text-meta);
}

.ch-page-breadcrumb-sep {
  margin: 0 6px;
}

.ch-page-breadcrumb-current {
  color: var(--ch-signal);
}

.ch-page-heading {
  margin: 16px 0 0;
  font-family: var(--ch-font-serif);
  font-size: 32px;
  font-weight: 400;
  color: var(--ch-text);
}

.ch-page-moved-notice {
  margin-top: 16px;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  color: var(--ch-text-meta);
}

.ch-page-status,
.ch-page-empty {
  margin-top: 24px;
  font-size: var(--ch-size-body);
  color: var(--ch-text-meta);
}

.ch-page-root-list {
  margin: 20px 0 0;
  padding: 0;
  list-style: none;
}

.ch-page-root-link {
  display: block;
  padding: var(--ch-space-1) 0;
  font-size: var(--ch-size-md);
  color: var(--ch-text-2);
  text-decoration: none;
}

.ch-page-root-link:hover {
  color: var(--ch-text);
}

.ch-page-notes {
  margin: 20px 0 0;
  padding: 0;
  list-style: none;
  border-top: 1px solid var(--ch-line);
}

.ch-page-note-row {
  border-bottom: 1px solid var(--ch-line);
}

.ch-page-note-link {
  display: flex;
  align-items: baseline;
  gap: 14px;
  padding: 13px 2px;
  text-decoration: none;
}

.ch-page-note-ref {
  flex: none;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-sm);
  color: var(--ch-signal);
}

.ch-page-note-title {
  flex: 1;
  min-width: 0;
  font-size: var(--ch-size-md);
  color: var(--ch-text);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ch-page-note-updated {
  flex: none;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  color: var(--ch-text-meta);
}
</style>
