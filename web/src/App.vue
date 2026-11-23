<script setup lang="ts">
// Root shell. Deliberately thin: routing owns the screen, and the screens
// themselves are later tickets. See src/router.ts and src/auth/index.ts for
// the two things this skeleton actually does -- route under /app/, and
// resolve who is signed in before the first paint.
import { ref, watch } from 'vue'
import { RouterView, useRoute } from 'vue-router'

// A client-side navigation is silent to a screen reader: nothing reloads, so
// nothing is announced. Say the new page's name in a polite live region
// (CHRN-171), and keep document.title in step for the tab and history.
const route = useRoute()
const announcement = ref('')
watch(
  () => route.fullPath,
  () => {
    const title = route.meta.title
    if (!title) return
    announcement.value = title
    document.title = `${title} — Chronicle`
  },
  { immediate: true },
)
</script>

<template>
  <RouterView />
  <p class="visually-hidden" aria-live="polite" data-test="route-announcer">{{ announcement }}</p>
</template>
