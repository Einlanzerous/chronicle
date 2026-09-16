<script setup lang="ts">
// Placeholder. CHRN-106 builds the real sign-in screen; src/auth/index.ts
// routes here when GET /auth/me and POST /auth/sso/cloudflare have both
// answered 401, i.e. nobody is signed in and there is no SSO session to
// adopt.
</script>

<template>
  <main class="sign-in">
    <p>A sign-in screen is CHRN-106's.</p>
  </main>
</template>

<style scoped>
.sign-in {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  font-family: var(--ch-font-sans);
  color: var(--ch-text-2);
}
</style>
