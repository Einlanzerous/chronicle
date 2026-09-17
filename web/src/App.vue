<script setup lang="ts">
// Root shell. Deliberately thin: routing owns the screen, and the screens
// themselves are later tickets. See src/router.ts and src/auth/index.ts for
// the two things this skeleton actually does -- route under /app/, and
// resolve who is signed in before the first paint.
import { RouterView } from 'vue-router'
</script>

<template>
  <RouterView />
</template>
