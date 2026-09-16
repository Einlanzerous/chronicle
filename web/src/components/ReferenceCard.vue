<script setup lang="ts">
// One resolved estate reference, rendered as a live card (CHRN-56), or --
// for a local `chronicle` reference -- a plain in-app link. The view model
// (referenceCard.ts's `buildReferenceCard`) is pure and unit tested; this
// component only draws it.
//
// CLAUDE.md invariant 2, held to literally here: "coral is Switchyard, gold
// is Amber, anywhere either resolves" (`card.colorVar` is one of the two
// RESERVED tokens.css custom properties, or null), "LINKED · NOT COPIED"
// labels every estate card regardless of its resolution state -- it is a
// structural fact about how Chronicle relates to the upstream row, not a
// claim about whether the row was reachable just now -- and "a cache with no
// visible staleness is a copy that lies": the age line renders whenever the
// server attempted a fetch, and the unreachable card says plainly that it
// could not reach upstream rather than showing the last good answer as if it
// were current.
import { RouterLink } from 'vue-router'
import type { ReferenceCardView } from '@/lib/referenceCard'

defineProps<{ card: ReferenceCardView }>()

const STATE_LABEL: Record<ReferenceCardView['state'], string> = {
  resolved: '',
  broken: 'BROKEN',
  unreachable: 'UNREACHABLE',
  unconfigured: 'NOT CONFIGURED',
  // Never "checking…" -- CHRN-51/openapi.yaml is explicit that unchecked is
  // never answered, so nothing here should imply an answer is coming.
  unchecked: 'NOT CHECKED',
}
</script>

<template>
  <RouterLink
    v-if="!card.isEstate && card.href"
    :to="card.href"
    class="ch-ref-card ch-ref-card--local"
  >
    <span class="ch-ref-card-key">{{ card.key ?? card.token }}</span>
    <span v-if="card.title" class="ch-ref-card-title">{{ card.title }}</span>
    <span v-if="card.state === 'broken'" class="ch-ref-card-local-state">DELETED</span>
  </RouterLink>

  <!-- A local reference this client never got a resolution for at all (over
       this render's own batch cap) has nowhere in-app to point yet -- shown
       as an unchecked line rather than a guess at a link. -->
  <div v-else-if="!card.isEstate" class="ch-ref-card ch-ref-card--local ch-ref-card--local-unchecked">
    <span class="ch-ref-card-key">{{ card.key ?? card.token }}</span>
    <span class="ch-ref-card-local-state">NOT CHECKED</span>
  </div>

  <div
    v-else
    class="ch-ref-card"
    :style="{ '--ch-ref-card-color': card.colorVar ? `var(${card.colorVar})` : 'var(--ch-text-meta)' }"
  >
    <div class="ch-ref-card-row">
      <span class="ch-ref-card-key">{{ card.key ?? card.token }}</span>
      <span v-if="card.title" class="ch-ref-card-title">{{ card.title }}</span>

      <span v-if="card.state === 'resolved' && card.stateWord" class="ch-ref-card-pill">{{ card.stateWord }}</span>
      <span v-else class="ch-ref-card-pill ch-ref-card-pill--muted">{{ STATE_LABEL[card.state] }}</span>

      <span class="ch-ref-card-linked">LINKED · NOT COPIED</span>
      <a v-if="card.url" :href="card.url" target="_blank" rel="noopener noreferrer" class="ch-ref-card-arrow"
        >↗ {{ card.system.toUpperCase() }}</a
      >
      <span v-else class="ch-ref-card-arrow ch-ref-card-arrow--dead">↗ {{ card.system.toUpperCase() }}</span>
    </div>

    <div v-if="card.ageLabel || card.lastResolvedLabel || card.explain" class="ch-ref-card-meta">
      <span v-if="card.ageLabel">{{ card.ageLabel }}</span>
      <span v-if="card.lastResolvedLabel">{{ card.lastResolvedLabel }}</span>
      <span v-if="card.explain">{{ card.explain }}</span>
    </div>
  </div>
</template>

<style scoped>
.ch-ref-card {
  display: flex;
  flex-direction: column;
  gap: 6px;
  padding: 13px 15px;
  background: var(--ch-raised);
  border: 1px solid var(--ch-line);
  border-left: 2px solid var(--ch-ref-card-color, var(--ch-text-meta));
}

.ch-ref-card-row {
  display: flex;
  align-items: center;
  gap: 14px;
  flex-wrap: wrap;
}

.ch-ref-card-key {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-sm);
  color: var(--ch-ref-card-color, var(--ch-text));
}

.ch-ref-card-title {
  font-size: var(--ch-size-md);
  color: var(--ch-text);
}

.ch-ref-card-pill {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-2xs);
  font-weight: 600;
  letter-spacing: var(--ch-track-mid);
  color: var(--ch-ref-card-color, var(--ch-text-meta));
  background: color-mix(in srgb, var(--ch-ref-card-color, var(--ch-text-meta)) 16%, transparent);
  padding: 2px 7px;
}

.ch-ref-card-pill--muted {
  color: var(--ch-text-meta);
  background: transparent;
  border: 1px solid var(--ch-line);
}

.ch-ref-card-linked {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-2xs);
  letter-spacing: var(--ch-track-mid);
  color: var(--ch-text-meta);
}

.ch-ref-card-arrow {
  margin-left: auto;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  color: var(--ch-text-meta);
  text-decoration: none;
}

.ch-ref-card-arrow:hover {
  color: var(--ch-text);
}

.ch-ref-card-arrow--dead {
  opacity: 0.55;
}

.ch-ref-card-meta {
  display: flex;
  flex-wrap: wrap;
  gap: 10px;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  color: var(--ch-text-meta);
}

.ch-ref-card--local {
  flex-direction: row;
  align-items: center;
  gap: 10px;
  padding: 9px 13px;
  border-left: 2px solid var(--ch-signal);
  text-decoration: none;
}

.ch-ref-card--local .ch-ref-card-key {
  color: var(--ch-signal);
}

.ch-ref-card--local .ch-ref-card-title {
  color: var(--ch-text-2);
  font-size: var(--ch-size-body);
}

.ch-ref-card--local-unchecked {
  border-left-color: var(--ch-line);
}

.ch-ref-card-local-state {
  margin-left: auto;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-2xs);
  letter-spacing: var(--ch-track-mid);
  color: var(--ch-text-meta);
}
</style>
