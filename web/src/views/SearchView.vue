<script setup lang="ts">
// `/app/search?q=` (CHRN-58). The canvas draws no search-results screen
// (board 1c has only the sidebar's `⌕ Search notes` box) -- this view is
// designed in code, against the two constraints the CHRN-58 ticket comment
// names: `search` is owner-only in the contract, so a member's query
// answers 403 and the box stays put with an explanation rather than
// hiding; and a transcript hit is tier 2 but not a note, so its row needs
// a source label as clear as a tier-1 row's steel (src/lib/searchResult.ts
// is that labelling, unit tested there).
import { computed, ref, watch } from 'vue'
import { RouterLink, useRoute } from 'vue-router'
import { api } from '@/api/client'
import { describeSearchHit } from '@/lib/searchResult'
import type { components } from '@/api/schema.d.ts'

type SearchHit = components['schemas']['SearchHit']

const route = useRoute()

const query = computed(() => (typeof route.query.q === 'string' ? route.query.q : ''))
const hits = ref<SearchHit[] | null>(null)
const loading = ref(false)
const forbidden = ref(false)
const errorMessage = ref<string | null>(null)

// Bumped on every search and captured per-request, so a slower answer to an
// earlier query cannot land after a faster answer to a later one and show
// the wrong results under the current query string (typing "pruner", then
// "amber" before the first answers).
let searchSeq = 0

async function runSearch(q: string): Promise<void> {
  const seq = ++searchSeq
  hits.value = null
  forbidden.value = false
  errorMessage.value = null
  if (!q) return
  loading.value = true
  const res = await api.GET('/search', { params: { query: { q } } })
  if (seq !== searchSeq) return // superseded by a later search
  loading.value = false
  if (res.data) {
    hits.value = res.data.items
    return
  }
  if (res.response.status === 403) {
    forbidden.value = true
    return
  }
  errorMessage.value = res.error?.message ?? 'Search failed.'
}

watch(query, runSearch, { immediate: true })

// A note hit's ref and a transcript hit's memo_id are each unique on their
// own kind (SearchHit's own doc: the two field sets are mutually
// exclusive), so kind plus whichever identity the hit carries is unique
// across the whole result list -- unlike the label, which two transcripts
// landing in the same minute can share (formatTimestamp truncates to it).
const rows = computed(() =>
  (hits.value ?? []).map((hit) => ({
    hit,
    source: describeSearchHit(hit),
    key: `${hit.kind}-${hit.ref ?? hit.memo_id}`,
  })),
)
</script>

<template>
  <div class="ch-search-route">
    <h1 class="ch-search-heading">Search</h1>
    <p class="ch-search-query" v-if="query">for “{{ query }}”</p>

    <!-- search is owner-only in the contract (x-chronicle-policy: owner) --
         the ticket comment names the 403 as a constraint and leaves the
         choice open ("decide whether the box hides or explains"); this PR
         explains rather than hides, and the box stays visible either way. -->
    <p v-if="forbidden" class="ch-search-forbidden">
      Search spans every author's transcripts, so it is owner-only.
    </p>
    <p v-else-if="loading" class="ch-search-status">Searching…</p>
    <p v-else-if="errorMessage" class="ch-search-status">{{ errorMessage }}</p>
    <p v-else-if="query && rows.length === 0" class="ch-search-status">No results.</p>

    <ul v-else-if="rows.length > 0" class="ch-search-results">
      <li v-for="{ hit, source, key } in rows" :key="key" class="ch-search-row">
        <RouterLink
          v-if="source.to"
          :to="source.to"
          class="ch-search-row-link"
          :class="`ch-search-row-link--${source.kind}`"
        >
          <span class="ch-search-source" :class="`ch-search-source--${source.kind}`">{{
            source.label
          }}</span>
          <!-- eslint-disable-next-line vue/no-v-html -->
          <span class="ch-search-snippet" v-html="hit.snippet"></span>
        </RouterLink>
        <div v-else class="ch-search-row-static ch-search-row-static--transcript" :title="source.title">
          <span class="ch-search-source ch-search-source--transcript">{{ source.label }}</span>
          <!-- eslint-disable-next-line vue/no-v-html -->
          <span class="ch-search-snippet" v-html="hit.snippet"></span>
        </div>
      </li>
    </ul>
  </div>
</template>

<style scoped>
.ch-search-route {
  padding: 34px 46px;
  max-width: 900px;
}

.ch-search-heading {
  margin: 0;
  font-family: var(--ch-font-serif);
  font-size: 32px;
  font-weight: 400;
  color: var(--ch-text);
}

.ch-search-query {
  margin: 8px 0 0;
  font-size: var(--ch-size-body);
  color: var(--ch-text-meta);
}

.ch-search-forbidden,
.ch-search-status {
  margin-top: 24px;
  font-size: var(--ch-size-body);
  color: var(--ch-text-meta);
}

.ch-search-results {
  margin: 24px 0 0;
  padding: 0;
  list-style: none;
  border-top: 1px solid var(--ch-line);
}

.ch-search-row {
  border-bottom: 1px solid var(--ch-line);
}

.ch-search-row-link,
.ch-search-row-static {
  display: flex;
  align-items: baseline;
  gap: 16px;
  padding: 14px 2px;
  text-decoration: none;
}

.ch-search-row-static--transcript {
  cursor: help;
}

.ch-search-source {
  flex: none;
  width: 220px;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-sm);
}

/* A note hit is tier 2, and reads like the sidebar's own tier-2 accent.
 * A transcript hit is also tier 2 -- never steel, that colour is reserved
 * for generated content (CLAUDE.md invariant 1) -- but visually demoted to
 * the meta colour so the two are never mistaken for each other, matching
 * the ticket comment's "a source label as clear as steel". */
.ch-search-source--note {
  color: var(--ch-signal);
}

.ch-search-source--transcript {
  color: var(--ch-text-meta);
}

.ch-search-snippet {
  flex: 1;
  min-width: 0;
  font-size: var(--ch-size-body);
  color: var(--ch-text-2);
}

.ch-search-snippet :deep(b) {
  color: var(--ch-text);
  font-weight: 600;
}
</style>
