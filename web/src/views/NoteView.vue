<script setup lang="ts">
// `/app/notes/:ref` -- placeholder. CHRN-56 builds the note reader (tier 2
// beside its tier-1 references, resolved as live cards). This exists so a
// link to it -- from the page view's note rows, and from a search hit --
// has somewhere to land inside the shell.
import { useRoute } from 'vue-router'

const route = useRoute()
</script>

<template>
  <div class="ch-placeholder">
    <p>A note reader for <strong>{{ route.params.ref }}</strong> is CHRN-56's.</p>
  </div>
</template>

<style scoped>
.ch-placeholder {
  padding: 34px 46px;
  font-family: var(--ch-font-sans);
  color: var(--ch-text-2);
}
</style>
