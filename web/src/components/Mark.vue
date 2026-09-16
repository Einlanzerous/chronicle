<script setup lang="ts">
// The wordmark's mark: "2a, the interrupted rule" (CHRN-54's design brief,
// Chronicle.dc.html pass 02 · identity) -- two ruled lines, a waveform
// cutting across the middle, two more ruled lines below. It is the pick
// because it is literally the product: writing, with speech landing inside
// it, and it is the only one of the four candidates in the canvas built to
// survive at an 11px square next to the wordmark -- which `size` proves.
//
// `fill="currentColor"` and no colour literal here: a consumer sets colour
// through CSS `color`, and the default below reaches for the token rather
// than a hex value, so scripts/check-tokens.sh has nothing to catch.
withDefaults(defineProps<{ size?: number }>(), { size: 22 })
</script>

<template>
  <svg
    :width="size"
    :height="size"
    viewBox="0 0 24 24"
    fill="currentColor"
    aria-hidden="true"
    class="ch-mark"
  >
    <rect x="0" y="2" width="24" height="1.5" />
    <rect x="0" y="5.5" width="21.12" height="1.5" />
    <rect x="5.25" y="10.5" width="1.5" height="3" />
    <rect x="8.25" y="9" width="1.5" height="6" />
    <rect x="11.25" y="10" width="1.5" height="4" />
    <rect x="14.25" y="9" width="1.5" height="6" />
    <rect x="17.25" y="10.5" width="1.5" height="3" />
    <rect x="0" y="17" width="17.28" height="1.5" />
    <rect x="0" y="20.5" width="22.56" height="1.5" />
  </svg>
</template>

<style scoped>
.ch-mark {
  color: var(--ch-signal);
  display: block;
  flex-shrink: 0;
}
</style>
