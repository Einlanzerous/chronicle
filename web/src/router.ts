import { createRouter, createWebHistory } from 'vue-router'
import AppShell from './layouts/AppShell.vue'
import PageTreeView from './views/PageTreeView.vue'
import Tier1PageView from './views/Tier1PageView.vue'
import SearchView from './views/SearchView.vue'
import NoteView from './views/NoteView.vue'
import DiscussionView from './views/DiscussionView.vue'
import TriageView from './views/TriageView.vue'
import AccountView from './views/AccountView.vue'
import SignIn from './views/SignIn.vue'

// History base matches vite.config.ts's `base` and web/embed.go's mount
// point (CHRN-53's decision comment): the API owns the root -- GET
// /notes/{ref} is JSON, so a client route like /app/notes/CHR-0311 cannot
// also be a page there -- and every route this app owns lives under /app/.
//
// Every signed-in route nests under AppShell (CHRN-58): one sidebar
// instance for the whole session, rather than one per route, so its
// listPages/listTier1Pages calls happen once, not on every navigation.
// /sign-in is the one route outside it -- there is nobody signed in yet to
// show a page tree or an account row for.
export const router = createRouter({
  history: createWebHistory('/app/'),
  routes: [
    {
      path: '/',
      component: AppShell,
      children: [
        // `/app/` -- redirect to `/app/pages/` (the root page list) once
        // signed in (CHRN-53's decision comment; src/auth/index.ts is what
        // gates "signed in" before this router ever renders).
        { path: '', redirect: '/pages' },
        { path: 'pages/:pathMatch(.*)*', name: 'pages', component: PageTreeView },
        { path: 'tier1/:pathMatch(.*)*', name: 'tier1', component: Tier1PageView },
        { path: 'search', name: 'search', component: SearchView },
        // CHRN-56/57/55/106 build these; each is a minimal placeholder
        // inside the shell for now, named after the ticket that owns it.
        { path: 'notes/:ref', name: 'note', component: NoteView },
        { path: 'discussions/:ref', name: 'discussion', component: DiscussionView },
        { path: 'triage', name: 'triage', component: TriageView },
        { path: 'account', name: 'account', component: AccountView },
      ],
    },
    // CHRN-106 builds the real sign-in screen; this is only the fork
    // src/auth/index.ts routes to when nobody is signed in and Cloudflare
    // Access has nothing to offer either. Outside the shell: there is no
    // sidebar to draw for someone who is not signed in.
    { path: '/sign-in', name: 'sign-in', component: SignIn },
  ],
})
