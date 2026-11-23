import { createRouter, createWebHistory } from 'vue-router'

declare module 'vue-router' {
  interface RouteMeta {
    /** Page name, announced to screen readers on navigation (App.vue). */
    title?: string
  }
}

// Every screen is a lazy chunk (CHRN-171): the shell and the active route load
// first, and the rest -- TriageView alone is 1300+ lines, and AccountView
// pulls in the QR encoder -- load on first visit rather than in the entry.
const AppShell = () => import('./layouts/AppShell.vue')
const PageTreeView = () => import('./views/PageTreeView.vue')
const Tier1PageView = () => import('./views/Tier1PageView.vue')
const SearchView = () => import('./views/SearchView.vue')
const NoteView = () => import('./views/NoteView.vue')
const DiscussionsListView = () => import('./views/DiscussionsListView.vue')
const DiscussionView = () => import('./views/DiscussionView.vue')
const TriageView = () => import('./views/TriageView.vue')
const AccountView = () => import('./views/AccountView.vue')
const SignIn = () => import('./views/SignIn.vue')

// A tab left open across a release asks for `assets/<View>-<oldhash>.js`,
// which no longer exists; the server answers that path with index.html, so the
// module import fails and the navigation is dropped silently. Recover with a
// full load of the target, which fetches the new index and its new chunks.
// Guarded to once per target so a genuinely broken chunk cannot loop.
const CHUNK_ERROR = /dynamically imported module|importing a module script failed|Unable to preload/i
const RELOAD_KEY = 'ch-chunk-reload'

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
        { path: 'pages/:pathMatch(.*)*', name: 'pages', meta: { title: 'Pages' }, component: PageTreeView },
        { path: 'tier1/:pathMatch(.*)*', name: 'tier1', meta: { title: 'Estate page' }, component: Tier1PageView },
        { path: 'search', name: 'search', meta: { title: 'Search' }, component: SearchView },
        // CHRN-56/57/55/106 build these; each is a minimal placeholder
        // inside the shell for now, named after the ticket that owns it.
        { path: 'notes/:ref', name: 'note', meta: { title: 'Note' }, component: NoteView },
        // CHRN-57: the DISCUSSIONS sidebar row links to the bare 'discussions'
        // list; 'discussions/:ref' is a thread. Order matters only in that
        // the exact path must be registered -- vue-router does not treat
        // 'discussions' as matching ':ref' of the row below it.
        { path: 'discussions', name: 'discussions', meta: { title: 'Discussions' }, component: DiscussionsListView },
        { path: 'discussions/:ref', name: 'discussion', meta: { title: 'Discussion' }, component: DiscussionView },
        { path: 'triage', name: 'triage', meta: { title: 'Evening triage' }, component: TriageView },
        { path: 'account', name: 'account', meta: { title: 'Account' }, component: AccountView },
      ],
    },
    // CHRN-106 builds the real sign-in screen; this is only the fork
    // src/auth/index.ts routes to when nobody is signed in and Cloudflare
    // Access has nothing to offer either. Outside the shell: there is no
    // sidebar to draw for someone who is not signed in.
    { path: '/sign-in', name: 'sign-in', meta: { title: 'Sign in' }, component: SignIn },
  ],
})

router.onError((err, to) => {
  if (!CHUNK_ERROR.test(String((err as Error)?.message ?? err))) return
  if (sessionStorage.getItem(RELOAD_KEY) === to.fullPath) return
  sessionStorage.setItem(RELOAD_KEY, to.fullPath)
  window.location.assign(router.resolve(to.fullPath).href)
})
