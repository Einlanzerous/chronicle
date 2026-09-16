/// <reference types="vite/client" />

// Without this, vue-tsc's own .vue-module resolution silently does not
// activate when `vue-tsc` is invoked via oven/bun's node-fallback shim
// rather than a real Node.js binary -- reproduced against
// deploy/Dockerfile's web stage, where `bun run build` failed with
// "Cannot find module './App.vue'" on every .vue import despite an
// identical `bun run build` succeeding on a host with real Node.js on
// PATH. Mirrors Lyceum's web/env.d.ts, which carries the same shim.
declare module '*.vue' {
  import type { DefineComponent } from 'vue'
  const component: DefineComponent<Record<string, never>, Record<string, never>, unknown>
  export default component
}
