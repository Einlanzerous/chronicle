// Pure tier-1-pane path logic (CHRN-56): whether a note's own page path is
// one `getTier1Page` could ever answer for. Mirrors the corpus's own rule
// (internal/estatewiki/estatewiki.go's ValidPath, transcribed in
// internal/api/tier1.go's 400 message: "path must be relative,
// slash-separated, without an empty or dot-prefixed segment, and without
// .md") rather than guessing at it -- a client-side check that disagreed
// with the server's would either call an endpoint sure to answer 400, or
// hide a pane for a path the server would actually have served.
export function isValidTier1Path(path: string): boolean {
  if (!path || path.endsWith('.md') || path.startsWith('/') || path.endsWith('/')) return false
  const segments = path.split('/')
  return segments.every((s) => s.length > 0 && !s.startsWith('.'))
}

/**
 * The tier-1 pane beside a note is keyed on the note's OWN page path
 * (CHRN-56's Done-when: "the tier-1 page whose path matches the note's page
 * path"). No prefix, no guessing at a different corpus address -- just the
 * one path a note already carries, validated against the same rule the
 * server enforces so an invalid one never reaches `getTier1Page` at all
 * (there answering 400, not the honest-404 "no such page" this component
 * would otherwise have to explain away as something else).
 */
export function tier1PathForNotePage(pagePath: string): string | null {
  const trimmed = pagePath.trim()
  return isValidTier1Path(trimmed) ? trimmed : null
}
