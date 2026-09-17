<script setup lang="ts">
// Placeholder view (CHRN-53): proof that the skeleton actually talks to the
// API, nothing more. It renders the wordmark, the signed-in user's display
// name (src/auth/index.ts resolves this before the app mounts), and the
// /healthz version so a glance at /app/ confirms the whole path -- Vite
// build, Go embed, session cookie, JSON response -- is wired end to end. The
// real screens are other tickets.
import { onMounted, ref } from 'vue'
import Wordmark from '@/components/Wordmark.vue'
import { api } from '@/api/client'
import { currentUser } from '@/auth'

const version = ref<string | null>(null)

onMounted(async () => {
  const res = await api.GET('/healthz')
  if (res.data) version.value = res.data.version
})
</script>

<template>
  <main class="home">
    <Wordmark />
    <p class="home-meta">
      <span v-if="currentUser">Signed in as {{ currentUser.display_name }}</span>
      <span v-else>Not signed in</span>
      <span v-if="version"> · chronicle {{ version }}</span>
    </p>
  </main>
</template>

<style scoped>
.home {
  min-height: 100vh;
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 16px;
}

.home-meta {
  font-family: var(--ch-font-mono);
  font-size: 11px;
  letter-spacing: 0.08em;
  color: var(--ch-text-meta);
  text-transform: uppercase;
}
</style>
