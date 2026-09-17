// Pure search-result labelling, kept out of SearchView so it is unit
// testable without mounting anything (CHRN-58). The comment on CHRN-58
// names the constraint this exists to satisfy: "a transcript hit is a hit
// on tier 2 that is not a note, so its row needs a source label as clearly
// as a tier-1 row needs steel." `SearchHit.kind` says which of the two
// mutually-exclusive field sets a hit carries (openapi.yaml).
import type { components } from '@/api/schema.d.ts'
import { formatTimestamp } from './format'

export type SearchHit = components['schemas']['SearchHit']

export interface SearchHitSource {
  kind: 'note' | 'transcript'
  /** The mono source label a row renders, e.g. `NOTE · CHR-0311`. */
  label: string
  /** Where the row links, under /app/ -- absent when the row is not yet linkable. */
  to: string | null
  /** Set only when `to` is null, so a row explains why it does not link. */
  title?: string
}

// CHRN-107 owns the transcript read; until it lands, a transcript hit links
// nowhere, and says so rather than pretending to be a dead link.
const TRANSCRIPT_NOT_YET_LINKABLE = 'The transcript reader is CHRN-107 — not linkable yet.'

export function describeSearchHit(hit: SearchHit): SearchHitSource {
  if (hit.kind === 'note') {
    return {
      kind: 'note',
      label: `NOTE · ${hit.ref}`,
      to: `/notes/${hit.ref}`,
    }
  }
  return {
    kind: 'transcript',
    // The appended timestamp is NOT the memo's captured time, despite the
    // shared field name: a transcript hit's `created_at` is
    // `tier2.transcripts.created_at` (internal/store/search.go), stamped
    // when THIS transcription row was written -- when the ASR result was
    // collected, not when the memo was recorded. A re-transcription (a
    // later, better decode of an old memo) surfaces with today's date, not
    // the memo's. `TRANSCRIPT` is still the right SOURCE label (parallel to
    // `NOTE · <ref>`, matching the CHRN-58 brief) -- it is the timestamp
    // next to it that is transcribed-at, not captured-at.
    label: `TRANSCRIPT · ${formatTimestamp(hit.created_at)}`,
    to: null,
    title: TRANSCRIPT_NOT_YET_LINKABLE,
  }
}
