import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vitest/config'
import vue from '@vitejs/plugin-vue'

// CHRN-58's minimal vitest setup, the shape Lyceum's web/vitest.config.ts
// already uses. jsdom is for the two things worth a regression test here:
// the page-tree builder (pure data → tree, no DOM) and the search
// source-labelling (pure data → row descriptor, no DOM either) -- jsdom is
// only needed because vitest's config loader shares this file with a
// potential future component test, not because either current suite
// touches the DOM.
export default defineConfig({
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  test: {
    environment: 'jsdom',
    globals: true,
    include: ['src/**/*.{test,spec}.ts'],
  },
})
