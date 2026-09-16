<script setup lang="ts">
// `/app/discussions/:ref` -- placeholder. CHRN-57 builds the discussion
// thread view (turns, unread, resolve). This exists so a link to it has
// somewhere to land inside the shell.
import { useRoute } from 'vue-router'

const route = useRoute()
</script>

<template>
  <div class="ch-placeholder">
    <p>A discussion thread for <strong>{{ route.params.ref }}</strong> is CHRN-57's.</p>
  </div>
</template>

<style scoped>
.ch-placeholder {
  padding: 34px 46px;
  font-family: var(--ch-font-sans);
  color: var(--ch-text-2);
}
</style>
