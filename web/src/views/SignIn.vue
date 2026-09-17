<script setup lang="ts">
// `/app/sign-in` (CHRN-106, board 1d's "SIGN IN · DIRECT HOST, NO SESSION"
// inset, error state "SIGN IN · ERROR"). src/auth/index.ts routes here when
// GET /auth/me and POST /auth/sso/cloudflare have both answered something
// other than 200 -- nobody is signed in, and there is no Access assertion to
// adopt. The tunneled host never renders this: createSessionFromAccess
// succeeds there on load and auth/index.ts never reaches the push to
// /sign-in.
//
// Outside AppShell (router.ts) -- there is no sidebar, no account row, no
// page tree to draw for someone who has not signed in yet.
import { ref } from 'vue'
import { useRouter } from 'vue-router'
import Mark from '@/components/Mark.vue'
import { api } from '@/api/client'
import { resolveSession } from '@/auth'

const router = useRouter()

const token = ref('')
const deviceLabel = ref('')
const submitting = ref(false)
// A single boolean, not an error string: openapi.yaml's createSession is
// explicit that a spent, expired and unknown invite answer with ONE
// indistinguishable body on purpose -- "probing cannot tell a used invite
// from one that never existed" -- so the client has no distinction to make
// either, whatever status or body comes back, network failure included.
const failed = ref(false)

async function submit(): Promise<void> {
  const t = token.value.trim()
  if (!t || submitting.value) return
  submitting.value = true
  failed.value = false
  try {
    const label = deviceLabel.value.trim()
    const res = await api.POST('/auth/session', {
      body: label ? { token: t, device_label: label } : { token: t },
    })
    if (res.data) {
      // The server set the cookie (Set-Cookie on this 200); nothing to store
      // here (`session_token` on the response body is for the non-browser
      // clients openapi.yaml names, not this one). Re-run the same bootstrap
      // main.ts ran before the first paint, then land in the shell.
      const me = await resolveSession(router)
      if (me) await router.push('/')
      return
    }
    failed.value = true
  } catch {
    failed.value = true
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <main class="ch-sign-in">
    <!-- The canvas draws "no session" and "error" as two separate insets;
         this is one continuous form that grows the error message in place
         rather than dropping the device-label field and meta sentence when
         a submission fails -- losing what you typed into a field the error
         has nothing to do with would be inventing a worse interaction than
         the board specifies, not a more faithful one. Named as a deviation
         in the PR. -->
    <form class="ch-sign-in-card" @submit.prevent="submit">
      <div class="ch-sign-in-wordmark">
        <Mark :size="22" />
        <span class="ch-sign-in-wordmark-text">CHRONICLE</span>
      </div>

      <div class="ch-sign-in-field">
        <label class="ch-sign-in-label" for="ch-sign-in-token">INVITE TOKEN</label>
        <input
          id="ch-sign-in-token"
          v-model="token"
          class="ch-sign-in-input"
          :class="{ 'is-error': failed }"
          type="text"
          autocomplete="off"
          autocapitalize="off"
          spellcheck="false"
          :disabled="submitting"
          required
        />
        <p v-if="failed" class="ch-sign-in-error">That token did not work.</p>
      </div>

      <div class="ch-sign-in-field">
        <label class="ch-sign-in-label" for="ch-sign-in-device-label">DEVICE LABEL · OPTIONAL</label>
        <input
          id="ch-sign-in-device-label"
          v-model="deviceLabel"
          class="ch-sign-in-input"
          type="text"
          placeholder="this device"
          autocomplete="off"
          :disabled="submitting"
        />
      </div>

      <button type="submit" class="ch-sign-in-submit" :disabled="submitting || !token.trim()">
        SIGN IN
      </button>

      <p class="ch-sign-in-meta">
        The tunneled host signs you in through Access. This one takes a token.
      </p>
    </form>
  </main>
</template>

<style scoped>
.ch-sign-in {
  min-height: 100vh;
  display: flex;
  align-items: center;
  justify-content: center;
  background: var(--ch-base);
  padding: 24px;
}

.ch-sign-in-card {
  width: 480px;
  max-width: 100%;
  border: 1px solid var(--ch-line);
  background: var(--ch-base);
  padding: 56px 48px 48px;
}

.ch-sign-in-wordmark {
  display: flex;
  align-items: center;
  gap: 14px;
}

.ch-sign-in-wordmark-text {
  font-family: var(--ch-font-mono);
  font-size: 15px;
  letter-spacing: 0.22em;
  color: var(--ch-text);
}

.ch-sign-in-field {
  margin-top: 42px;
}

.ch-sign-in-field + .ch-sign-in-field {
  margin-top: 20px;
}

.ch-sign-in-label {
  display: block;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-2xs);
  letter-spacing: 0.14em;
  color: var(--ch-text-meta);
}

.ch-sign-in-input {
  margin-top: 8px;
  width: 100%;
  height: 42px;
  border: 1px solid var(--ch-line);
  background: var(--ch-raised);
  color: var(--ch-text);
  font-family: var(--ch-font-mono);
  font-size: 15px;
  letter-spacing: 0.06em;
  padding: 0 13px;
}

.ch-sign-in-input:focus {
  outline: none;
  border-color: var(--ch-signal);
}

.ch-sign-in-input.is-error {
  border-bottom: 2px solid var(--ch-signal);
  color: var(--ch-text-2);
}

.ch-sign-in-error {
  margin: 10px 0 0;
  font-size: 13.5px;
  color: var(--ch-text);
}

.ch-sign-in-submit {
  margin-top: 26px;
  width: 100%;
  height: 46px;
  border: none;
  background: var(--ch-signal);
  font-family: var(--ch-font-mono);
  font-size: 11.5px;
  font-weight: 600;
  letter-spacing: 0.13em;
  color: var(--ch-base);
  cursor: pointer;
}

.ch-sign-in-submit:disabled {
  opacity: 0.6;
  cursor: default;
}

.ch-sign-in-meta {
  margin: 20px 0 0;
  font-size: 12.5px;
  line-height: 1.6;
  color: var(--ch-text-meta);
}
</style>
