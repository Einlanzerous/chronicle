import { fileURLToPath, URL } from 'node:url'
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'

// Served by the Go binary under /app/ (CHRN-53's decision comment): the API
// owns the root -- GET /notes/{ref} is JSON -- so the app's own client routes
// live under /app/, and `base` here is what bakes that prefix into every
// asset URL the build emits. web/embed.go mounts the built bundle at the same
// prefix, so the two must agree.
const BASE = '/app/'

// Dev-server proxy target: Chronicle's own default port (CHRONICLE_PORT).
// Chronicle's HTTP contract has no /v1 prefix (openapi.yaml ruling 6) and no
// CORS surface, so in dev every request that is NOT under /app/ is, by
// construction, an API request -- /healthz, /auth/me, /notes/{ref}, and
// whatever later epics add -- and gets proxied here rather than enumerated
// route by route, which would silently miss whatever the next epic adds.
const BACKEND = process.env.CHRONICLE_BACKEND ?? 'http://localhost:4009'

export default defineConfig({
  base: BASE,
  plugins: [vue()],
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  server: {
    proxy: {
      // Regex key (leading ^): everything except the app's own base path.
      '^/(?!app/).*': {
        target: BACKEND,
        changeOrigin: true,
      },
    },
  },
})
