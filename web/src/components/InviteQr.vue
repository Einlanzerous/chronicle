<script setup lang="ts">
// The invite as a QR (CHRN-106), for ADD DEVICE and the owner's PEOPLE
// invites alike. Board 1d's brief: draw it in `--ch-signal` on `--ch-base`,
// about 200px -- nothing here is a reference (no coral/gold) and nothing is
// generated (no steel), so the QR itself carries the one colour that is
// Chronicle's own.
//
// `qrcode`'s renderer wants literal colour strings, not CSS custom
// properties -- a canvas has no cascade to resolve `var(--ch-signal)`
// against. Reading the values off `getComputedStyle` at draw time (rather
// than hardcoding either colour's hex here) keeps this file free of the hex
// literals scripts/check-tokens.sh greps for -- there is deliberately no
// hardcoded fallback either -- and means a token swap in tokens.css still
// repaints every QR drawn after it, same as everything else on the page.
import { ref, watchEffect } from 'vue'
import QRCode, { type QRCodeToDataURLOptions } from 'qrcode'

const props = defineProps<{ url: string }>()

const src = ref('')
const failed = ref(false)

function tokenColor(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim()
}

// Guards a slow render for a spent invite landing after a fast one for the
// invite that replaced it (NEW INVITE re-mints in place) -- otherwise two
// renders in flight could land out of order, showing a QR that scans clean
// beside a key it no longer matches.
let renderSeq = 0

watchEffect(async () => {
  const seq = ++renderSeq
  const url = props.url
  failed.value = false
  try {
    const options: QRCodeToDataURLOptions = { errorCorrectionLevel: 'M', margin: 1, width: 200 }
    const dark = tokenColor('--ch-signal')
    const light = tokenColor('--ch-base')
    // Both tokens are always defined in tokens.css; this guard only matters
    // for a theoretical render before the stylesheet has applied, where
    // falling through to the library's own built-in default is safer than a
    // hardcoded hex literal here would be.
    if (dark && light) options.color = { dark, light }
    const dataUrl = await QRCode.toDataURL(url, options)
    if (seq !== renderSeq) return
    src.value = dataUrl
  } catch {
    // A QR that will not render must not take the reveal down with it -- the
    // copyable token beside it is still the whole of what a sign-in needs.
    if (seq !== renderSeq) return
    src.value = ''
    failed.value = true
  }
})
</script>

<template>
  <img v-if="src && !failed" class="ch-invite-qr" :src="src" width="200" height="200" alt="Invite QR code" />
  <p v-else-if="failed" class="ch-invite-qr-error">QR could not be drawn — the token beside it still works.</p>
</template>

<style scoped>
.ch-invite-qr {
  display: block;
  width: 200px;
  height: 200px;
  flex: none;
  border: 1px solid var(--ch-line);
}

.ch-invite-qr-error {
  margin: 0;
  width: 200px;
  font-family: var(--ch-font-mono);
  font-size: var(--ch-size-xs);
  color: var(--ch-text-meta);
}
</style>
