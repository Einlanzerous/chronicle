import { createRouter, createWebHistory } from 'vue-router'
import Home from './views/Home.vue'
import SignIn from './views/SignIn.vue'

// History base matches vite.config.ts's `base` and web/embed.go's mount
// point (CHRN-53's decision comment): the API owns the root -- GET
// /notes/{ref} is JSON, so a client route like /app/notes/CHR-0311 cannot
// also be a page there -- and every route this app owns lives under /app/.
export const router = createRouter({
  history: createWebHistory('/app/'),
  routes: [
    { path: '/', name: 'home', component: Home },
    // CHRN-106 builds the real sign-in screen; this is only the fork
    // src/auth/index.ts routes to when nobody is signed in and Cloudflare
    // Access has nothing to offer either.
    { path: '/sign-in', name: 'sign-in', component: SignIn },
  ],
})
