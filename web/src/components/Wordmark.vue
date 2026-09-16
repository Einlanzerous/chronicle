<script setup lang="ts">
// The mono wordmark (CHRN-54's design brief, "2a IN USE"): mark 2a at 22px
// beside CHRONICLE set in JetBrains Mono -- the family choice that matches
// SERV's and Switchyard's own wordmarks rather than inventing a fourth.
import Mark from './Mark.vue'

withDefaults(defineProps<{ tagline?: string }>(), { tagline: 'TIER 2 · AUTHORED' })
</script>

<template>
  <div class="ch-wordmark">
    <div class="ch-wordmark-row">
      <Mark :size="22" />
      <span class="ch-wordmark-text">CHRONICLE</span>
    </div>
    <div v-if="tagline" class="ch-wordmark-tagline">{{ tagline }}</div>
  </div>
</template>

<style scoped>
.ch-wordmark-row {
  display: flex;
  align-items: center;
  gap: 20px;
}

.ch-wordmark-text {
  font-family: var(--ch-font-mono);
  font-size: 25px;
  letter-spacing: 0.24em;
  color: var(--ch-text);
}

.ch-wordmark-tagline {
  margin-top: 9px;
  font-family: var(--ch-font-mono);
  font-size: 9.5px;
  letter-spacing: 0.2em;
  color: var(--ch-text-meta);
}
</style>
