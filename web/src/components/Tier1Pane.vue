<script setup lang="ts">
// The tier-1 pane (CHRN-58): mono and steel throughout, `READ ONLY` stamped,
// no edit affordance of any kind -- CLAUDE.md invariant 1, "the tier-1 pane
// is stamped READ ONLY because editing it would fabricate existence."
// Reusable rather than folded into Tier1PageView so CHRN-56 can place it
// beside a note; this component makes no assumption about its own width,
// the route that renders it full-width is what decides that.
import type { components } from '@/api/schema.d.ts'

type Tier1Page = components['schemas']['Tier1Page']

defineProps<{
  page: Tier1Page | null
  loading: boolean
  /** A one-sentence, person-facing reason there is no page to show. */
  error: string | null
}>()
</script>

<template>
  <div class="ch-tier1-pane">
    <div class="ch-tier1-pane-stamp">READ ONLY</div>

    <p v-if="loading" class="ch-tier1-pane-status">Loading…</p>
    <p v-else-if="error" class="ch-tier1-pane-status">{{ error }}</p>

    <template v-else-if="page">
      <!-- No separate title heading: the generator's own rendered body
           already carries one (confirmed against a real render -- SERV's
           corpus keeps the H1 in the body, front matter or not), and a
           second one here duplicated it. `page.title` still does the
           sidebar's and the browser tab's job; it does not need a second
           on-page appearance. -->
      <!-- eslint-disable-next-line vue/no-v-html -->
      <div class="ch-tier1-pane-body" v-html="page.html"></div>
      <!-- The Generated marking's own line, rendered verbatim -- never
           composed here (CHRN-58's ticket comment: "render that marking's
           line verbatim as the fact sheet's footer rather than composing
           one"). -->
      <div class="ch-tier1-pane-footer">{{ page.generated.notice }}</div>
    </template>
  </div>
</template>

<style scoped>
.ch-tier1-pane {
  font-family: var(--ch-font-mono);
  color: var(--ch-generated);
}

.ch-tier1-pane-stamp {
  font-size: var(--ch-size-2xs);
  letter-spacing: 0.12em;
  color: var(--ch-generated);
  border: 1px solid color-mix(in srgb, var(--ch-generated) 45%, transparent);
  padding: 3px 8px;
  display: inline-block;
}

.ch-tier1-pane-status {
  margin-top: 18px;
  font-size: var(--ch-size-xs);
  color: var(--ch-text-meta);
}

.ch-tier1-pane-body {
  margin-top: 20px;
  font-size: var(--ch-size-body);
  line-height: 1.7;
}

.ch-tier1-pane-body :deep(*) {
  font-family: var(--ch-font-mono);
  color: var(--ch-generated);
}

.ch-tier1-pane-footer {
  margin-top: 24px;
  padding-top: 14px;
  border-top: 1px solid var(--ch-line);
  font-size: var(--ch-size-2xs);
  letter-spacing: 0.1em;
  color: var(--ch-text-meta);
}
</style>
