// Shared, deliberately locale- and timezone-independent formatting for the
// ISO instants the API answers everywhere (`created_at`, `updated_at`,
// `generated_at`, ...). Not `Intl` or a date library: a mono metadata field
// wants the sortable, unambiguous `YYYY-MM-DD HH:MM` slice of the instant
// itself, and using it keeps this deterministic between a laptop and CI.
export function formatTimestamp(iso: string): string {
  return iso.slice(0, 16).replace('T', ' ')
}

// CHRN-106: board 1d's DEVICES rows show a bare date for `SINCE` and for a
// `SEEN` that has aged out of the relative window (`SINCE 2026-08-25`, `SEEN
// 2026-04-30`) -- no time-of-day, unlike formatTimestamp above. Same
// deterministic slice-of-the-instant approach, just to the day.
export function formatDate(iso: string): string {
  return iso.slice(0, 10)
}
