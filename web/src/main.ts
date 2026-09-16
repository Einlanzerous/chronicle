import { createApp } from 'vue'
import App from './App.vue'
import { router } from './router'
import { resolveSession } from './auth'
import './styles/tokens.css'
import './styles/base.css'

const app = createApp(App)
app.use(router)

// Resolve who is signed in (or send them to /app/sign-in) before the first
// paint, so Home never flashes a signed-out shell for the instant it takes
// GET /auth/me to answer.
void resolveSession(router).finally(() => {
  app.mount('#app')
})
