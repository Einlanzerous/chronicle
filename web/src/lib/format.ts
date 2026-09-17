// Shared, deliberately locale- and timezone-independent formatting for the
// ISO instants the API answers everywhere (`created_at`, `updated_at`,
// `generated_at`, ...). Not `Intl` or a date library: a mono metadata field
// wants the sortable, unambiguous `YYYY-MM-DD HH:MM` slice of the instant
// itself, and using it keeps this deterministic between a laptop and CI.
export function formatTimestamp(iso: string): string {
  return iso.slice(0, 16).replace('T', ' ')
}
