// Pure reference-resolution rendering logic (CHRN-56), kept out of
// ReferenceCard.vue so the estate rule it exists to enforce is unit testable
// without mounting anything: CLAUDE.md invariant 2 -- "a reference resolves
// at render time and is never written into Chronicle's tables... coral is
// Switchyard, gold is Amber, anywhere either resolves... a cache with no
// visible staleness is a copy that lies."
//
// `Note.references` (ReferenceDescriptor[]) is what the note's OWN text
// scanned to -- no upstream state, nothing that can go stale (openapi.yaml).
// `POST /references/resolve` answers one `Resolution` per token, and this
// file is the mapping from a (descriptor, resolution) pair to the card a
// component renders: which of the two reserved colour tokens (if any), the
// upstream state word exactly as returned, the visible cache age, and the
// unreachable/unconfigured/unchecked shapes the API describes rather than a
// confident stale value.
import type { components } from '@/api/schema.d.ts'

export type ReferenceDescriptor = components['schemas']['ReferenceDescriptor']
export type Resolution = components['schemas']['Resolution']
export type ResolutionState = Resolution['state']

/** CHRN-51's own cap on one resolve call. Mirrored client-side so a note
 *  with more distinct references than one render can check gets an honest
 *  "not checked" card for the remainder, rather than this client sending a
 *  batch the server would refuse outright. */
export const MAX_RESOLVE_BATCH = 50

/**
 * The two RESERVED tokens (tokens.css) and nothing else may ever answer
 * this -- `null` for `chronicle`, which is never coral or gold (rendered as
 * a plain in-app link instead).
 */
export function colorVarForSystem(system: ReferenceDescriptor['system']): string | null {
  switch (system) {
    case 'switchyard':
      return '--ch-ref-switchyard'
    case 'amber':
      return '--ch-ref-amber'
    default:
      return null
  }
}

/** Estate systems are dialled out to and cached; `chronicle` never is. */
export function isEstateSystem(system: ReferenceDescriptor['system']): boolean {
  return system === 'switchyard' || system === 'amber'
}

/**
 * `Note.references` in order of first appearance, deduplicated by `token` --
 * "a token named three times appears three times" in the array (openapi.yaml),
 * but it names ONE thing, and rendering three identical cards for one mention
 * would not be honest about the note, only about how many times it was typed.
 */
export function dedupeReferences(descriptors: readonly ReferenceDescriptor[]): ReferenceDescriptor[] {
  const seen = new Set<string>()
  const out: ReferenceDescriptor[] = []
  for (const d of descriptors) {
    if (seen.has(d.token)) continue
    seen.add(d.token)
    out.push(d)
  }
  return out
}

/** The local-vs-estate split: `chronicle` (CHR-####, DSC-####) never leaves
 *  the process and is never coral or gold; `switchyard`/`amber` do and are. */
export function splitReferences(descriptors: readonly ReferenceDescriptor[]): {
  estate: ReferenceDescriptor[]
  local: ReferenceDescriptor[]
} {
  const estate: ReferenceDescriptor[] = []
  const local: ReferenceDescriptor[] = []
  for (const d of descriptors) {
    if (isEstateSystem(d.system)) estate.push(d)
    else local.push(d)
  }
  return { estate, local }
}

/**
 * "4 MIN" / "2 HR" / "3 DAYS" -- no "AS OF", no "ago": callers compose the
 * full sentence, because the unreachable card and the ordinary cache-age
 * label say different things around the same number.
 */
export function relativeAgeLabel(iso: string, nowMs: number): string {
  const ms = Math.max(0, nowMs - new Date(iso).getTime())
  const minutes = Math.floor(ms / 60000)
  if (minutes < 1) return '<1 MIN'
  if (minutes < 60) return `${minutes} MIN`
  const hours = Math.floor(minutes / 60)
  if (hours < 24) return `${hours} HR`
  const days = Math.floor(hours / 24)
  return `${days} DAY${days === 1 ? '' : 'S'}`
}

/** The card's visible cache age -- CHRN-56's own example, "AS OF 4 MIN AGO". */
export function formatCacheAge(iso: string, nowMs: number): string {
  return `AS OF ${relativeAgeLabel(iso, nowMs)} AGO`
}

/** The unreachable card's honest secondary line: a fact about the past, not
 *  a disguised claim about now -- never rendered as if it were current. */
export function formatLastResolved(iso: string, nowMs: number): string {
  return `Last reached ${relativeAgeLabel(iso, nowMs)} ago`
}

function localHref(descriptor: ReferenceDescriptor, resolution: Resolution | undefined): string {
  const key = resolution?.upstream?.key ?? descriptor.token
  return descriptor.target === 'discussion' ? `/discussions/${key}` : `/notes/${key}`
}

/** One resolved reference, as a card is built from it. */
export interface ReferenceCardView {
  token: string
  system: ReferenceDescriptor['system']
  /** `switchyard` or `amber` -- gets the coral/gold "live card" treatment. */
  isEstate: boolean
  /** One of the two reserved token names, or `null` for a local reference. */
  colorVar: string | null
  state: ResolutionState
  key?: string
  title?: string
  /** The upstream's own state word, exactly as returned -- never composed. */
  stateWord?: string
  url?: string
  /** In-app route, for a local (`chronicle`) reference only. */
  href?: string
  ageLabel?: string
  lastResolvedLabel?: string
  explain?: string
}

/**
 * Builds the card view model CHRN-56's Done-when names: "key and title, the
 * upstream state word exactly as returned, the label LINKED · NOT COPIED, an
 * outbound arrow..., coral for Switchyard and gold for Amber..., the cache
 * age rendered visibly..., and the unreachable/stale state rendered as the
 * API describes it." `resolution` absent means this descriptor was never
 * sent to `resolveReferences` at all (see `MAX_RESOLVE_BATCH`) -- rendered
 * identically to the server's own `unchecked`, per the same rule: "not
 * checked", never "checking…".
 */
export function buildReferenceCard(
  descriptor: ReferenceDescriptor,
  resolution: Resolution | undefined,
  nowMs: number,
): ReferenceCardView {
  const isEstate = isEstateSystem(descriptor.system)
  const colorVar = colorVarForSystem(descriptor.system)

  if (!resolution) {
    return {
      token: descriptor.token,
      system: descriptor.system,
      isEstate,
      colorVar,
      state: 'unchecked',
      key: descriptor.key,
    }
  }

  const upstream = resolution.upstream
  // Switchyard publishes `display_name` ("IN PROGRESS"), the status "as a
  // person reads it". Amber and Chronicle have none, so `outcome` -- the
  // upstream's own vocabulary member where it has one -- stands in: still
  // "the state word exactly as returned", never composed here.
  const stateWord = upstream?.display_name ?? upstream?.outcome

  return {
    token: resolution.token,
    system: descriptor.system,
    isEstate,
    colorVar,
    state: resolution.state,
    key: upstream?.key ?? descriptor.key,
    title: upstream?.title,
    stateWord,
    url: upstream?.url,
    href: !isEstate ? localHref(descriptor, resolution) : undefined,
    ageLabel: resolution.fetched_at ? formatCacheAge(resolution.fetched_at, nowMs) : undefined,
    lastResolvedLabel:
      resolution.state === 'unreachable' && resolution.last_resolved_at
        ? formatLastResolved(resolution.last_resolved_at, nowMs)
        : undefined,
    explain: resolution.explain,
  }
}
