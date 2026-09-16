// Session resolution (CHRN-53). The session itself lives in the
// `chronicle_session` cookie -- HttpOnly, so this module never reads or sets
// it directly; api/client.ts's `credentials: 'same-origin'` is the whole of
// how the browser carries it.
import { ref } from 'vue'
import type { Router } from 'vue-router'
import type { components } from '@/api/schema.d.ts'
import { api } from '@/api/client'

export type User = components['schemas']['User']

// The signed-in account, or null before the first check resolves and after a
// session dies. A ref rather than Pinia: this is the one piece of state the
// skeleton shares across components, and CHRN-53 does not pull in a store
// library for it -- a later ticket can, if a second piece of shared state
// shows up wanting one.
export const currentUser = ref<User | null>(null)

// Resolved once, from main.ts, before the app mounts:
//
//   1. GET /auth/me. A 200 means the cookie is still good.
//   2. ANYTHING ELSE -- 401 (no session, or an expired one), 403 (the
//      Access assertion verified but named an email no account holds,
//      openapi.yaml's `createSessionFromAccess`), 429, 500 -- try the
//      Cloudflare Access exchange (POST /auth/sso/cloudflare): the tunnel
//      may have injected a verified identity Chronicle has not yet turned
//      into a session, and this is the same call the browser would make on
//      any cold load. Every one of those statuses means "not signed in
//      right now", and none of them is actionable here -- there is no
//      retry or backoff to attempt before falling through to step 3.
//   3. Still no session after that means there is truly nobody signed in
//      and no way to become somebody without a person present, so this
//      routes to a placeholder sign-in view rather than leaving the app to
//      fail every later request one at a time. CHRN-106 owns what that
//      screen actually looks like; this is only the fork in the road.
export async function resolveSession(router: Router): Promise<User | null> {
  const me = await api.GET('/auth/me')
  if (me.data) {
    currentUser.value = me.data
    return me.data
  }

  const sso = await api.POST('/auth/sso/cloudflare')
  if (sso.data) {
    currentUser.value = sso.data.user
    return sso.data.user
  }

  currentUser.value = null
  await router.push('/sign-in')
  return null
}
