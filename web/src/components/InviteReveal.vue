<script setup lang="ts">
// One invite, shown once (CHRN-106). Used by ADD DEVICE (block 3, board 1d)
// and reused as-is for the owner's PEOPLE invites (block 4's per-member "New
// invite" and "New person"), per the ticket: "showing the resulting
// token/QR the same way as block 3."
//
// Holds no state that survives its own unmount: no localStorage, no store,
// nothing written back through `currentUser` or any shared ref. Leaving this
// screen -- or a re-mint replacing `inviteToken`/`expiresIn` -- loses the
// value for good, which is the point of a single-use credential.
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import InviteQr from './InviteQr.vue'
import { countdownLabel, durationLabel, parseGoDuration } from '@/lib/account'

const props = defineProps<{
  inviteToken: string
  signInUrl?: string
  expiresIn: string
}>()

const expiresAtMs = ref(0)
const remainingSeconds = ref(0)
const copied = ref(false)

function reset(): void {
  expiresAtMs.value = Date.now() + parseGoDuration(props.expiresIn) * 1000
  remainingSeconds.value = Math.max(0, Math.round((expiresAtMs.value - Date.now()) / 1000))
  copied.value = false
}

function tick(): void {
  remainingSeconds.value = Math.max(0, Math.round((expiresAtMs.value - Date.now()) / 1000))
}

// A re-mint (NEW INVITE) passes a new token/expiry into the same mounted
// component -- watch rather than onMounted alone, so the countdown restarts
// rather than continuing to count down the PREVIOUS invite's clock.
watch(() => [props.inviteToken, props.expiresIn], reset, { immediate: true })

const timer = setInterval(tick, 1000)
onBeforeUnmount(() => clearInterval(timer))

const countdown = computed(() => countdownLabel(remainingSeconds.value))
const ttl = computed(() => durationLabel(parseGoDuration(props.expiresIn)))

async function copyToken(): Promise<void> {
  try {
    await navigator.clipboard.writeText(props.inviteToken)
    copied.value = true
    setTimeout(() => {
      copied.value = false
    }, 1500)
  } catch {
    // Clipboard access can be refused (permissions, an insecure context) --
    // the token is still plain selectable text either way, so this fails
    // quietly rather than surfacing an error over a cosmetic convenience.
  }
}
</script>

<template>
  <div class="ch-invite-reveal">
    <div class="ch-invite-reveal-sub">INVITE VIA QR CODE · SINGLE USE · EXPIRES IN {{ ttl }}</div>

    <div v-if="signInUrl" class="ch-invite-reveal-with-qr">
      <InviteQr :url="signInUrl" />
      <div class="ch-invite-reveal-info">
        <div class="ch-invite-reveal-label">INVITE TOKEN</div>
        <div class="ch-invite-reveal-token-row">
          <span class="ch-invite-reveal-token">{{ inviteToken }}</span>
          <button type="button" class="ch-invite-reveal-copy" @click="copyToken">
            {{ copied ? 'COPIED' : 'COPY' }}
          </button>
        </div>
        <div class="ch-invite-reveal-countdown">EXPIRES {{ countdown }}</div>
        <p class="ch-invite-reveal-copy-text">
          Scan from the Chronicle app on a phone or tablet, or paste the token into a sign-in form
          on a laptop. A token is shown once and works once.
        </p>
        <p class="ch-invite-reveal-lost">Shown once — leave this page and it is gone.</p>
      </div>
    </div>

    <div v-else class="ch-invite-reveal-no-qr">
      <div class="ch-invite-reveal-no-qr-label">NO QR · THIS HOST IS BEHIND ACCESS · PASTE THE TOKEN</div>
      <div class="ch-invite-reveal-no-qr-row">
        <span class="ch-invite-reveal-token ch-invite-reveal-token--small">{{ inviteToken }}</span>
        <button type="button" class="ch-invite-reveal-copy" @click="copyToken">
          {{ copied ? 'COPIED' : 'COPY' }}
        </button>
        <span class="ch-invite-reveal-countdown ch-invite-reveal-countdown--inline">EXPIRES {{ countdown }}</span>
      </div>
      <p class="ch-invite-reveal-lost">Shown once — leave this page and it is gone.</p>
    </div>
  </div>
</template>

<style scoped>
.ch-invite-reveal-sub {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-2xs);
  letter-spacing: var(--ch-track-wide);
  color: var(--ch-text-meta);
}

.ch-invite-reveal-with-qr {
  margin-top: 20px;
  display: flex;
  gap: 28px;
  align-items: flex-start;
}

.ch-invite-reveal-info {
  min-width: 0;
  flex: 1;
  max-width: 400px;
}

.ch-invite-reveal-label {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-2xs);
  letter-spacing: 0.12em;
  color: var(--ch-text-meta);
}

.ch-invite-reveal-token-row {
  margin-top: 8px;
  display: flex;
  align-items: center;
  gap: 12px;
}

.ch-invite-reveal-token {
  font-family: var(--ch-font-mono);
  font-size: 19px;
  letter-spacing: 0.06em;
  color: var(--ch-text);
  word-break: break-all;
}

.ch-invite-reveal-token--small {
  font-size: 17px;
}

.ch-invite-reveal-copy {
  flex: none;
  border: 1px solid var(--ch-line);
  background: transparent;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  letter-spacing: var(--ch-track-mid);
  color: var(--ch-text-2);
  padding: 4px 9px;
  cursor: pointer;
}

.ch-invite-reveal-copy:hover {
  color: var(--ch-signal);
}

.ch-invite-reveal-countdown {
  margin-top: 14px;
  font-family: var(--ch-font-mono);
  font-size: 11px;
  color: var(--ch-text-meta);
  font-variant-numeric: tabular-nums;
}

.ch-invite-reveal-copy-text {
  margin: 18px 0 0;
  font-size: 13.5px;
  line-height: 1.62;
  color: var(--ch-text-2);
}

.ch-invite-reveal-lost {
  margin: 10px 0 0;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-2xs);
  letter-spacing: var(--ch-track-mid);
  color: var(--ch-text-meta);
}

.ch-invite-reveal-no-qr {
  margin-top: 24px;
  max-width: 460px;
  border: 1px solid var(--ch-line);
  background: var(--ch-raised);
  padding: 16px;
}

.ch-invite-reveal-no-qr-label {
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-2xs);
  letter-spacing: 0.14em;
  color: var(--ch-text-meta);
}

.ch-invite-reveal-no-qr-row {
  margin-top: 12px;
  display: flex;
  align-items: center;
  gap: 12px;
}

.ch-invite-reveal-countdown--inline {
  margin-top: 0;
  margin-left: auto;
}
</style>
