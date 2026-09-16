import { describe, expect, it } from 'vitest'
import {
  canSeePeople,
  countdownLabel,
  durationLabel,
  isInvitedNeverSignedIn,
  parseGoDuration,
  seenLabel,
  sortSessions,
  type DeviceSession,
} from './account'

function session(overrides: Partial<DeviceSession> = {}): DeviceSession {
  return {
    id: '11111111-1111-4111-8111-111111111111',
    device_label: 'Pixel 9',
    created_at: '2026-08-25T00:00:00Z',
    last_seen_at: null,
    current: false,
    ...overrides,
  }
}

describe('sortSessions', () => {
  it('matches board 1d exactly: current first, then last-seen desc, never-seen last', () => {
    const current = session({ id: 'a', current: true, last_seen_at: '2026-09-16T19:00:00Z' })
    const recent = session({ id: 'b', last_seen_at: '2026-09-16T19:04:00Z' })
    const older = session({ id: 'c', last_seen_at: '2026-04-30T00:00:00Z' })
    const never = session({ id: 'd', last_seen_at: null })
    expect(sortSessions([never, older, current, recent]).map((s) => s.id)).toEqual([
      'a',
      'b',
      'c',
      'd',
    ])
  })

  it('puts the current row first even when it has never been seen', () => {
    const current = session({ id: 'a', current: true, last_seen_at: null })
    const seen = session({ id: 'b', last_seen_at: '2026-09-16T19:00:00Z' })
    expect(sortSessions([seen, current]).map((s) => s.id)).toEqual(['a', 'b'])
  })

  it('does not mutate its input', () => {
    const rows = [session({ id: 'a' }), session({ id: 'b' })]
    const copy = [...rows]
    sortSessions(rows)
    expect(rows).toEqual(copy)
  })
})

describe('seenLabel', () => {
  const now = new Date('2026-09-16T19:08:00Z')

  it('is NEVER for a null last_seen_at (board: "SEEN NEVER")', () => {
    expect(seenLabel(null, now)).toBe('NEVER')
  })

  it('is NOW inside the first minute (board: the current row, "SEEN NOW")', () => {
    expect(seenLabel('2026-09-16T19:07:45Z', now)).toBe('NOW')
  })

  it('reads "N MIN AGO" under an hour (board: "SEEN 4 MIN AGO", "SEEN 12 MIN AGO")', () => {
    expect(seenLabel('2026-09-16T19:04:00Z', now)).toBe('4 MIN AGO')
    expect(seenLabel('2026-08-25T00:00:00Z', new Date('2026-08-25T00:12:30Z'))).toBe('12 MIN AGO')
  })

  it('reads "N HR AGO" between one hour and a day', () => {
    expect(seenLabel('2026-09-16T12:00:00Z', now)).toBe('7 HR AGO')
  })

  it('falls back to a bare date past a day (board: "SEEN 2026-04-30")', () => {
    expect(seenLabel('2026-04-30T09:15:00Z', now)).toBe('2026-04-30')
  })
})

describe('parseGoDuration', () => {
  it('parses the board\'s own mock TTL', () => {
    expect(parseGoDuration('15m0s')).toBe(900)
  })

  it('parses the real store.InviteTTL shape (a week)', () => {
    expect(parseGoDuration('168h0m0s')).toBe(604800)
  })

  it('parses the schema example', () => {
    expect(parseGoDuration('24h0m0s')).toBe(86400)
  })

  it('handles a single unit with no trailing zero components', () => {
    expect(parseGoDuration('45s')).toBe(45)
  })

  it('is 0 for an empty or unparseable string rather than throwing', () => {
    expect(parseGoDuration('')).toBe(0)
    expect(parseGoDuration('not a duration')).toBe(0)
  })
})

describe('durationLabel', () => {
  it('matches the board\'s own "EXPIRES IN 15 MIN" for a 15-minute TTL', () => {
    expect(durationLabel(900)).toBe('15 MIN')
  })

  it('reduces the real 7-day TTL to whole days, not minutes', () => {
    expect(durationLabel(604800)).toBe('7 DAYS')
  })

  it('singularises a one-unit duration', () => {
    expect(durationLabel(3600)).toBe('1 HOUR')
    expect(durationLabel(86400)).toBe('1 DAY')
  })

  it('falls back to seconds for anything not a whole minute', () => {
    expect(durationLabel(90)).toBe('90 SEC')
  })
})

describe('countdownLabel', () => {
  it('matches the board\'s own "EXPIRES 14:32" MM:SS shape under an hour', () => {
    expect(countdownLabel(14 * 60 + 32)).toBe('14:32')
  })

  it('pads seconds under ten', () => {
    expect(countdownLabel(65)).toBe('01:05')
  })

  it('falls back to hours once past sixty minutes', () => {
    expect(countdownLabel(2 * 3600 + 5 * 60)).toBe('2H 05M')
  })

  it('falls back to days for the real week-long TTL', () => {
    expect(countdownLabel(6 * 86400 + 23 * 3600)).toBe('6D 23H')
  })

  it('reads EXPIRED at or past zero rather than a negative countdown', () => {
    expect(countdownLabel(0)).toBe('EXPIRED')
    expect(countdownLabel(-30)).toBe('EXPIRED')
  })
})

describe('isInvitedNeverSignedIn', () => {
  it('is true exactly for Member\'s own documented shape: last_seen_at null, invite_expires_at set', () => {
    expect(
      isInvitedNeverSignedIn({ last_seen_at: null, invite_expires_at: '2026-09-16T00:00:00Z' }),
    ).toBe(true)
  })

  it('is false once the account has signed in at least once', () => {
    expect(
      isInvitedNeverSignedIn({ last_seen_at: '2026-09-16T00:00:00Z', invite_expires_at: null }),
    ).toBe(false)
  })

  it('is false for an account with neither field set', () => {
    expect(isInvitedNeverSignedIn({ last_seen_at: null, invite_expires_at: null })).toBe(false)
  })
})

describe('canSeePeople', () => {
  it('is true for the owner', () => {
    expect(canSeePeople({ is_owner: true })).toBe(true)
  })

  it('is false for a member, so PEOPLE never mounts for them', () => {
    expect(canSeePeople({ is_owner: false })).toBe(false)
  })
})
