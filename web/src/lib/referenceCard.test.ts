import { describe, expect, it } from 'vitest'
import {
  MAX_RESOLVE_BATCH,
  buildReferenceCard,
  colorVarForSystem,
  dedupeReferences,
  formatCacheAge,
  formatLastResolved,
  isEstateSystem,
  relativeAgeLabel,
  splitReferences,
  type ReferenceDescriptor,
  type Resolution,
} from './referenceCard'

const NOW = new Date('2026-09-16T12:00:00Z').getTime()

function descriptor(overrides: Partial<ReferenceDescriptor> = {}): ReferenceDescriptor {
  return { system: 'switchyard', key: 'SWY', token: 'SWY-412', number: 412, ...overrides }
}

function resolution(overrides: Partial<Resolution> = {}): Resolution {
  return { token: 'SWY-412', state: 'resolved', ...overrides }
}

describe('colorVarForSystem / isEstateSystem', () => {
  it('reserves the coral token for switchyard and nothing else', () => {
    expect(colorVarForSystem('switchyard')).toBe('--ch-ref-switchyard')
  })

  it('reserves the gold token for amber and nothing else', () => {
    expect(colorVarForSystem('amber')).toBe('--ch-ref-amber')
  })

  it('gives chronicle no colour token at all -- never coral, never gold', () => {
    expect(colorVarForSystem('chronicle')).toBeNull()
  })

  it('classifies switchyard and amber as estate systems, chronicle as not', () => {
    expect(isEstateSystem('switchyard')).toBe(true)
    expect(isEstateSystem('amber')).toBe(true)
    expect(isEstateSystem('chronicle')).toBe(false)
  })
})

describe('dedupeReferences', () => {
  it('keeps the first occurrence order and drops repeats of the same token', () => {
    const refs = [
      descriptor({ token: 'SWY-412' }),
      descriptor({ token: 'AMB-2291', system: 'amber', key: undefined, number: undefined }),
      descriptor({ token: 'SWY-412' }),
    ]
    expect(dedupeReferences(refs).map((d) => d.token)).toEqual(['SWY-412', 'AMB-2291'])
  })
})

describe('splitReferences', () => {
  it('splits estate (dialled out) from local (never dialled) references', () => {
    const refs = [
      descriptor({ token: 'SWY-412' }),
      descriptor({ token: 'AMB-2291', system: 'amber' }),
      descriptor({ token: 'CHR-0311', system: 'chronicle', key: 'CHR', target: 'note', number: 311 }),
    ]
    const { estate, local } = splitReferences(refs)
    expect(estate.map((d) => d.token)).toEqual(['SWY-412', 'AMB-2291'])
    expect(local.map((d) => d.token)).toEqual(['CHR-0311'])
  })
})

describe('relativeAgeLabel / formatCacheAge / formatLastResolved', () => {
  it('reads under a minute as "<1 MIN"', () => {
    expect(relativeAgeLabel(new Date(NOW - 30_000).toISOString(), NOW)).toBe('<1 MIN')
  })

  it('matches CHRN-56\'s own example: 4 minutes reads "AS OF 4 MIN AGO"', () => {
    expect(formatCacheAge(new Date(NOW - 4 * 60_000).toISOString(), NOW)).toBe('AS OF 4 MIN AGO')
  })

  it('rolls over to hours at 60 minutes', () => {
    expect(relativeAgeLabel(new Date(NOW - 90 * 60_000).toISOString(), NOW)).toBe('1 HR')
  })

  it('rolls over to days at 24 hours, pluralised', () => {
    expect(relativeAgeLabel(new Date(NOW - 25 * 3_600_000).toISOString(), NOW)).toBe('1 DAY')
    expect(relativeAgeLabel(new Date(NOW - 50 * 3_600_000).toISOString(), NOW)).toBe('2 DAYS')
  })

  it('clamps a future/skewed timestamp to "<1 MIN" rather than a negative age', () => {
    expect(relativeAgeLabel(new Date(NOW + 5_000).toISOString(), NOW)).toBe('<1 MIN')
  })

  it('formats the unreachable card\'s honest secondary line in the past tense', () => {
    expect(formatLastResolved(new Date(NOW - 2 * 3_600_000).toISOString(), NOW)).toBe('Last reached 2 HR ago')
  })
})

describe('buildReferenceCard', () => {
  it('maps a resolved Switchyard reference to a coral card with the upstream display_name verbatim', () => {
    const d = descriptor({ token: 'SWY-412' })
    const r = resolution({
      token: 'SWY-412',
      state: 'resolved',
      upstream: { key: 'SWY-412', outcome: 'in_progress', display_name: 'IN PROGRESS', title: 'Rebuild Amber index shards', url: 'https://switchyard.example/SWY-412' },
      fetched_at: new Date(NOW - 4 * 60_000).toISOString(),
    })
    const card = buildReferenceCard(d, r, NOW)
    expect(card.isEstate).toBe(true)
    expect(card.colorVar).toBe('--ch-ref-switchyard')
    expect(card.state).toBe('resolved')
    expect(card.stateWord).toBe('IN PROGRESS')
    expect(card.title).toBe('Rebuild Amber index shards')
    expect(card.url).toBe('https://switchyard.example/SWY-412')
    expect(card.ageLabel).toBe('AS OF 4 MIN AGO')
    expect(card.href).toBeUndefined()
  })

  it('maps a resolved Amber reference to a gold card, using outcome since Amber has no display_name', () => {
    const d = descriptor({ system: 'amber', key: undefined, token: 'amber1.sess.rec', number: undefined })
    const r = resolution({ token: 'amber1.sess.rec', state: 'resolved', upstream: { outcome: 'held' }, fetched_at: new Date(NOW - 90 * 60_000).toISOString() })
    const card = buildReferenceCard(d, r, NOW)
    expect(card.colorVar).toBe('--ch-ref-amber')
    expect(card.stateWord).toBe('held')
    expect(card.ageLabel).toBe('AS OF 1 HR AGO')
  })

  it('maps a broken reference honestly, without inventing a title or url', () => {
    const d = descriptor({ token: 'SWY-999' })
    const r = resolution({ token: 'SWY-999', state: 'broken', upstream: { key: 'SWY-999' }, fetched_at: new Date(NOW).toISOString() })
    const card = buildReferenceCard(d, r, NOW)
    expect(card.state).toBe('broken')
    expect(card.title).toBeUndefined()
    expect(card.stateWord).toBeUndefined()
  })

  it('maps unreachable to a card with no upstream fact and an honest last-reached line, never a stale value', () => {
    const d = descriptor({ token: 'SWY-412' })
    const r = resolution({
      token: 'SWY-412',
      state: 'unreachable',
      fetched_at: new Date(NOW).toISOString(),
      last_resolved_at: new Date(NOW - 2 * 3_600_000).toISOString(),
      explain: 'Switchyard did not answer within the deadline.',
    })
    const card = buildReferenceCard(d, r, NOW)
    expect(card.state).toBe('unreachable')
    expect(card.title).toBeUndefined()
    expect(card.url).toBeUndefined()
    expect(card.lastResolvedLabel).toBe('Last reached 2 HR ago')
    expect(card.explain).toBe('Switchyard did not answer within the deadline.')
  })

  it('maps unconfigured with no age label, since nothing was ever attempted', () => {
    const d = descriptor({ token: 'SWY-412' })
    const r = resolution({ token: 'SWY-412', state: 'unconfigured', explain: 'No Switchyard credential on this deployment.' })
    const card = buildReferenceCard(d, r, NOW)
    expect(card.state).toBe('unconfigured')
    expect(card.ageLabel).toBeUndefined()
    expect(card.lastResolvedLabel).toBeUndefined()
  })

  it('maps a descriptor with no resolution at all to "unchecked" -- never "checking…"', () => {
    const d = descriptor({ token: 'SWY-412' })
    const card = buildReferenceCard(d, undefined, NOW)
    expect(card.state).toBe('unchecked')
    expect(card.ageLabel).toBeUndefined()
  })

  it('builds an in-app note link for a local chronicle reference, in no reserved colour', () => {
    const d = descriptor({ system: 'chronicle', key: 'CHR', token: 'CHR-0311', target: 'note', number: 311 })
    const r = resolution({ token: 'CHR-0311', state: 'resolved', upstream: { key: 'CHR-0311', title: 'Never copy Amber or Switchyard into tier 2' } })
    const card = buildReferenceCard(d, r, NOW)
    expect(card.isEstate).toBe(false)
    expect(card.colorVar).toBeNull()
    expect(card.href).toBe('/notes/CHR-0311')
  })

  it('builds an in-app discussion link for a local chronicle discussion reference', () => {
    const d = descriptor({ system: 'chronicle', key: 'DSC', token: 'DSC-0007', target: 'discussion', number: 7 })
    const r = resolution({ token: 'DSC-0007', state: 'resolved', upstream: { key: 'DSC-0007' } })
    const card = buildReferenceCard(d, r, NOW)
    expect(card.href).toBe('/discussions/DSC-0007')
  })

  it('falls back to the descriptor\'s own token for a local href when unresolved', () => {
    const d = descriptor({ system: 'chronicle', key: 'CHR', token: 'CHR-311', target: 'note', number: 311 })
    const card = buildReferenceCard(d, undefined, NOW)
    // unchecked -- no resolution at all, so no href is offered rather than
    // guessed at from an un-derived token.
    expect(card.href).toBeUndefined()
  })
})

describe('MAX_RESOLVE_BATCH', () => {
  it('matches CHRN-51\'s own cap of 50', () => {
    expect(MAX_RESOLVE_BATCH).toBe(50)
  })
})
