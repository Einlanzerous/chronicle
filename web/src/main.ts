import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router'
import { currentUser, resolveSession } from './auth'
import './styles/tokens.css'
import './styles/base.css'

const app = createApp(App)
app.use(router)

// Resolve who is signed in (or send them to /app/sign-in) before the first
// paint, so Home never flashes a signed-out shell for the instant it takes
// GET /auth/me to answer.
//
// openapi-fetch answers an HTTP error as {error}, not a throw -- but a
// request that never reaches the server (backend unreachable, offline) DOES
// throw, and resolveSession has no try/catch of its own. Without this
// .catch, that surfaces as an unhandled promise rejection in the console on
// exactly the load where someone is already debugging a dead backend. The
// app still mounts either way: a session that failed to resolve is the same
// as no session.
resolveSession(router)
  .catch(() => {
    currentUser.value = null
  })
  .finally(() => {
    app.mount('#app')
  })
