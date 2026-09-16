// Pure account/devices logic, kept out of the components so it is unit
// testable without mounting anything (CHRN-106, following CHRN-57/58's
// discussionThread.ts / pageTree.ts / searchResult.ts precedent).
import type { components } from '@/api/schema.d.ts'

export type DeviceSession = components['schemas']['DeviceSession']
export type Member = components['schemas']['Member']
export type User = components['schemas']['User']

// ── Devices: sort order ──────────────────────────────────────────────────
//
// Board 1d, ticket's own words: "current first, then by last seen desc,
// never-seen last." `current` always wins the comparison outright -- a
// current device that has somehow never phoned in still leads the list,
// because "this is the one you're looking at right now" outranks recency.
export function sortSessions(sessions: readonly DeviceSession[]): DeviceSession[] {
  return [...sessions].sort((a, b) => {
    if (a.current !== b.current) return a.current ? -1 : 1
    if (a.last_seen_at === null && b.last_seen_at === null) return 0
    if (a.last_seen_at === null) return 1
    if (b.last_seen_at === null) return -1
    if (a.last_seen_at === b.last_seen_at) return 0
    return a.last_seen_at > b.last_seen_at ? -1 : 1
  })
}

// ── Devices: the "SEEN …" label ──────────────────────────────────────────
//
// Read off the board: `SEEN NOW`, `SEEN 4 MIN AGO`, `SEEN 12 MIN AGO` for
// recent sessions, `SEEN 2026-04-30` (a bare date, no time -- formatDate's
// job, not this function's) for one four months stale, and `SEEN NEVER` for
// `last_seen_at: null`. This returns the word after `SEEN `, not the whole
// label, so a caller composing `SEEN {{ seenLabel(...) }}` matches the board
// exactly without this function hardcoding the prefix.
export function seenLabel(lastSeenAt: string | null, now: Date = new Date()): string {
  if (lastSeenAt === null) return 'NEVER'
  const diffMs = now.getTime() - Date.parse(lastSeenAt)
  if (diffMs < 60_000) return 'NOW'
  const minutes = Math.floor(diffMs / 60_000)
  if (minutes < 60) return `${minutes} MIN AGO`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours} HR AGO`
  return lastSeenAt.slice(0, 10)
}

// ── Go duration strings ──────────────────────────────────────────────────
//
// `Invite.expires_in` (openapi.yaml) is a Go `time.Duration.String()` value
// -- `24h0m0s`, `900000000ns`, `15m0s` -- a run of (number)(unit) pairs with
// no separators. This parses every unit Go's formatter can emit into
// seconds; a value this app never actually sees (store.InviteTTL is a flat
// week) still parses correctly rather than assuming the shape the design
// canvas's mock value ("15 MIN") happened to use.
const GO_DURATION_UNIT_SECONDS: Record<string, number> = {
  ns: 1e-9,
  'µs': 1e-6,
  us: 1e-6,
  ms: 1e-3,
  s: 1,
  m: 60,
  h: 3600,
}

const GO_DURATION_TOKEN_RE = /([0-9]*\.?[0-9]+)(ns|µs|us|ms|s|m|h)/g

export function parseGoDuration(input: string): number {
  const s = input.trim()
  if (s === '') return 0
  const negative = s.startsWith('-')
  const body = negative || s.startsWith('+') ? s.slice(1) : s
  GO_DURATION_TOKEN_RE.lastIndex = 0
  let total = 0
  let matched = false
  let match: RegExpExecArray | null
  while ((match = GO_DURATION_TOKEN_RE.exec(body)) !== null) {
    matched = true
    total += parseFloat(match[1]) * GO_DURATION_UNIT_SECONDS[match[2]]
  }
  if (!matched) return 0
  return negative ? -total : total
}

// ── "EXPIRES IN …" (the static TTL, at mint time) ────────────────────────
//
// The board's ADD DEVICE sub-label reads `EXPIRES IN 15 MIN` for its mock
// 15-minute TTL; this reduces any whole-unit duration to the same
// "N UNIT" shape (`7 DAYS`, `1 HOUR`, `15 MIN`) rather than hardcoding a
// number the real TTL (store.InviteTTL, a week) does not have.
export function durationLabel(totalSeconds: number): string {
  const s = Math.round(totalSeconds)
  if (s >= 86400 && s % 86400 === 0) {
    const days = s / 86400
    return `${days} DAY${days === 1 ? '' : 'S'}`
  }
  if (s >= 3600 && s % 3600 === 0) {
    const hours = s / 3600
    return `${hours} HOUR${hours === 1 ? '' : 'S'}`
  }
  if (s >= 60 && s % 60 === 0) {
    return `${s / 60} MIN`
  }
  return `${s} SEC`
}

// ── "EXPIRES 14:32" (the live, ticking countdown) ────────────────────────
//
// Matches the board's MM:SS shape for anything under an hour left (its own
// 15-minute invite reads `14:32`); an invite with days left (the real
// week-long TTL) falls back to a coarser `NDh` / `NDD NNh` shape rather than
// counting down seconds for a week.
export function countdownLabel(remainingSeconds: number): string {
  const total = Math.max(0, Math.round(remainingSeconds))
  if (total <= 0) return 'EXPIRED'
  const days = Math.floor(total / 86400)
  const hours = Math.floor((total % 86400) / 3600)
  const minutes = Math.floor((total % 3600) / 60)
  const seconds = total % 60
  if (days > 0) return `${days}D ${String(hours).padStart(2, '0')}H`
  if (hours > 0) return `${hours}H ${String(minutes).padStart(2, '0')}M`
  return `${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`
}

// ── People: the invited-never-signed-in predicate ────────────────────────
//
// `Member`'s own doc comment (openapi.yaml): "`last_seen_at` null and
// `invite_expires_at` set is an invite never redeemed."
export function isInvitedNeverSignedIn(member: Pick<Member, 'last_seen_at' | 'invite_expires_at'>): boolean {
  return member.last_seen_at === null && member.invite_expires_at !== null
}

// ── People: the owner-only gate ──────────────────────────────────────────
//
// Trivial, but named and tested on its own so AccountView's `v-if` reads as
// "the thing the ticket's Done-when actually asks for" rather than an
// inline `me.is_owner` a later edit could quietly invert.
export function canSeePeople(me: Pick<User, 'is_owner'>): boolean {
  return me.is_owner
}
