<script setup lang="ts">
// The shell every signed-in route renders inside (CHRN-58, board 1c/1d/1e):
// a fixed-width sidebar -- wordmark, search, TRIAGE, the tier-2 page tree,
// the tier-1 generated list, the account row -- and a main column that is
// whatever route matched. The two tiers are drawn one above the other in
// the SAME sidebar deliberately (CLAUDE.md invariant 1): the separation is
// a visual fact -- sans/text for tier 2, mono/steel for tier 1 -- not a
// paragraph explaining it.
import { computed, onMounted, provide, ref, watch } from 'vue'
import { RouterLink, RouterView, useRoute, useRouter } from 'vue-router'
import Mark from '@/components/Mark.vue'
import { api } from '@/api/client'
import { currentUser } from '@/auth'
import { buildPageTree, flattenPageTree } from '@/lib/pageTree'
import { PAGE_PATHS_KEY } from './shellData'
import type { components } from '@/api/schema.d.ts'

type Tier1PageSummary = components['schemas']['Tier1PageSummary']

const route = useRoute()
const router = useRouter()

// Fetched once per shell mount (the shell persists across every child
// route -- see router.ts -- so this is once per signed-in session, not
// once per page view).
const pagePaths = ref<string[] | null>(null)
const tier1Pages = ref<Tier1PageSummary[] | null>(null)

// Provided so `/app/pages/` (no path -- "the root page list") can render
// the tree's own roots without a second GET /pages: the shell already holds
// it for the session, and listPages is the whole corpus in one unpaginated
// call (openapi.yaml: "a few hundred pages at most").
provide(PAGE_PATHS_KEY, pagePaths)

onMounted(async () => {
  const [pages, tier1] = await Promise.all([api.GET('/pages'), api.GET('/tier1/pages')])
  if (pages.data) pagePaths.value = pages.data.paths
  if (tier1.data) tier1Pages.value = tier1.data.items
})

const pageTreeRows = computed(() =>
  pagePaths.value ? flattenPageTree(buildPageTree(pagePaths.value)) : [],
)

function currentPagesPath(): string {
  if (route.name !== 'pages') return ''
  const raw = route.params.pathMatch
  return Array.isArray(raw) ? raw.filter(Boolean).join('/') : (raw ?? '')
}

function currentTier1Path(): string {
  if (route.name !== 'tier1') return ''
  const raw = route.params.pathMatch
  return Array.isArray(raw) ? raw.filter(Boolean).join('/') : (raw ?? '')
}

// NOTE (decision, recorded in the CHRN-58 completion comment): the TRIAGE
// row draws no count badge. `getTriageBatch` is not a cheap "how many are
// pending" call -- it has no total/count field, only `items` (each one
// carrying a full excerpt and proposal) capped by `limit`, and `limit`'s
// minimum is 1, so there is no way to ask it for a count without paying for
// a batch of proposal payloads on every sidebar mount, on every route. The
// ticket names this exact fallback: render the row without a badge.

const searchInput = ref(typeof route.query.q === 'string' ? route.query.q : '')
watch(
  () => route.query.q,
  (q) => {
    if (typeof q === 'string') searchInput.value = q
  },
)

function submitSearch(): void {
  const q = searchInput.value.trim()
  if (!q) return
  router.push({ path: '/search', query: { q } })
}
</script>

<template>
  <div class="ch-shell">
    <aside class="ch-shell-sidebar">
      <div class="ch-shell-wordmark">
        <Mark :size="16" />
        <span class="ch-shell-wordmark-text">CHRONICLE</span>
      </div>

      <form class="ch-shell-search" @submit.prevent="submitSearch">
        <span class="ch-shell-search-icon" aria-hidden="true">⌕</span>
        <input
          v-model="searchInput"
          class="ch-shell-search-input"
          type="search"
          placeholder="Search notes"
          aria-label="Search notes"
        />
      </form>

      <RouterLink to="/triage" class="ch-shell-triage" active-class="is-active">
        <span class="ch-shell-triage-label">TRIAGE</span>
      </RouterLink>

      <nav class="ch-shell-section" aria-label="Tier 2, authored">
        <div class="ch-shell-section-label ch-shell-section-label--tier2">TIER 2 · AUTHORED</div>
        <RouterLink
          v-for="node in pageTreeRows"
          :key="node.path"
          :to="`/pages/${node.path}`"
          class="ch-shell-tree-row"
          :class="{ 'is-active': currentPagesPath() === node.path }"
          :style="{ paddingLeft: `${16 + node.depth * 12}px` }"
        >
          {{ node.label }}
        </RouterLink>
      </nav>

      <nav class="ch-shell-section ch-shell-section--tier1" aria-label="Tier 1, generated">
        <div class="ch-shell-section-label ch-shell-section-label--tier1">
          <span class="ch-shell-tier1-dot" aria-hidden="true"></span>
          TIER 1 · GENERATED
        </div>
        <RouterLink
          v-for="page in tier1Pages ?? []"
          :key="page.path"
          :to="`/tier1/${page.path}`"
          class="ch-shell-tree-row ch-shell-tree-row--tier1"
          :class="{ 'is-active': currentTier1Path() === page.path }"
        >
          {{ page.path }}
        </RouterLink>
        <div class="ch-shell-readonly">
          <div class="ch-shell-readonly-stamp">READ ONLY</div>
          <p class="ch-shell-readonly-body">
            Regenerated from SERV. Separate store — nothing here can overwrite tier 2.
          </p>
        </div>
      </nav>

      <RouterLink to="/account" class="ch-shell-account" active-class="is-active">
        <span class="ch-shell-account-accent" aria-hidden="true"></span>
        <span class="ch-shell-account-info">
          <span class="ch-shell-account-name">{{ currentUser?.display_name ?? '…' }}</span>
          <span class="ch-shell-account-email">{{ currentUser?.email ?? '' }}</span>
        </span>
      </RouterLink>
    </aside>

    <main class="ch-shell-main">
      <RouterView />
    </main>
  </div>
</template>

<style scoped>
.ch-shell {
  min-height: 100vh;
  display: grid;
  grid-template-columns: 236px 1fr;
  align-items: stretch;
}

.ch-shell-sidebar {
  border-right: 1px solid var(--ch-line);
  background: var(--ch-base);
  display: flex;
  flex-direction: column;
  min-height: 100vh;
}

.ch-shell-wordmark {
  display: flex;
  align-items: center;
  gap: var(--ch-space-2);
  padding: var(--ch-space-3) var(--ch-space-3) 18px;
}

.ch-shell-wordmark-text {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-sm);
  letter-spacing: 0.2em;
  color: var(--ch-text);
}

.ch-shell-search {
  margin: 0 var(--ch-space-3) 14px;
  height: 32px;
  border: 1px solid var(--ch-line);
  background: var(--ch-raised);
  display: flex;
  align-items: center;
  gap: var(--ch-space-2);
  padding: 0 10px;
}

.ch-shell-search-icon {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-sm);
  color: var(--ch-text-meta);
}

.ch-shell-search-input {
  flex: 1;
  min-width: 0;
  border: none;
  background: transparent;
  font-size: 12.5px;
  color: var(--ch-text);
}

.ch-shell-search-input::placeholder {
  color: var(--ch-text-meta);
}

.ch-shell-search-input:focus {
  outline: none;
}

.ch-shell-triage {
  display: flex;
  align-items: center;
  margin: 0 var(--ch-space-3) var(--ch-space-4);
  padding: var(--ch-space-1) 10px;
  border-left: 2px solid transparent;
  text-decoration: none;
}

.ch-shell-triage.is-active,
.ch-shell-triage:hover {
  background: var(--ch-raised);
  border-left-color: var(--ch-signal);
}

.ch-shell-triage-label {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-sm);
  letter-spacing: var(--ch-track-wide);
  color: var(--ch-text);
}

.ch-shell-section {
  display: flex;
  flex-direction: column;
}

.ch-shell-section--tier1 {
  margin-top: var(--ch-space-4);
  padding-top: var(--ch-space-4);
  border-top: 1px solid var(--ch-line);
}

.ch-shell-section-label {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-2xs);
  letter-spacing: var(--ch-track-wide);
  padding: 0 var(--ch-space-3) 6px;
}

.ch-shell-section-label--tier2 {
  color: var(--ch-signal);
}

.ch-shell-section-label--tier1 {
  display: flex;
  align-items: center;
  gap: 8px;
  color: var(--ch-generated);
}

.ch-shell-tier1-dot {
  width: 10px;
  height: 10px;
  flex: none;
  background: color-mix(in srgb, var(--ch-generated) 20%, transparent);
  border: 1px solid color-mix(in srgb, var(--ch-generated) 45%, transparent);
}

.ch-shell-tree-row {
  padding: var(--ch-space-1) var(--ch-space-3);
  font-size: var(--ch-size-body);
  color: var(--ch-text-2);
  text-decoration: none;
  border-left: 2px solid transparent;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.ch-shell-tree-row:hover {
  color: var(--ch-text);
}

.ch-shell-tree-row.is-active {
  color: var(--ch-text);
  background: var(--ch-raised);
  border-left-color: var(--ch-signal);
}

/* Tier 1 rows are visually demoted, on purpose (CLAUDE.md invariant 1):
 * mono and steel, never the tier-2 tree's sans/text treatment, so the two
 * halves of the sidebar are distinguishable at a glance and not just by
 * their section label. */
.ch-shell-tree-row--tier1 {
  padding-left: var(--ch-space-3);
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-sm);
  color: var(--ch-text-meta);
}

.ch-shell-tree-row--tier1:hover {
  color: var(--ch-generated);
}

.ch-shell-tree-row--tier1.is-active {
  color: var(--ch-generated);
  border-left-color: var(--ch-generated);
}

.ch-shell-readonly {
  margin: var(--ch-space-3);
  padding: 11px 12px;
  border: 1px solid var(--ch-line);
  background: var(--ch-raised);
}

.ch-shell-readonly-stamp {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-2xs);
  letter-spacing: 0.12em;
  color: var(--ch-generated);
}

.ch-shell-readonly-body {
  margin: 6px 0 0;
  font-size: 12px;
  line-height: 1.5;
  color: var(--ch-text-meta);
}

.ch-shell-account {
  margin-top: auto;
  display: flex;
  align-items: stretch;
  border-top: 1px solid var(--ch-line);
  text-decoration: none;
}

.ch-shell-account-accent {
  width: 2px;
  flex: none;
  background: var(--ch-signal);
}

.ch-shell-account.is-active,
.ch-shell-account:hover {
  background: var(--ch-raised);
}

.ch-shell-account-info {
  flex: 1;
  min-width: 0;
  padding: 12px 15px;
}

.ch-shell-account-name,
.ch-shell-account-email {
  display: block;
}

.ch-shell-account-name {
  font-size: var(--ch-size-md);
  color: var(--ch-text);
}

.ch-shell-account-email {
  margin-top: 4px;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  color: var(--ch-text-meta);
}

.ch-shell-main {
  min-width: 0;
  background: var(--ch-base);
}
</style>
