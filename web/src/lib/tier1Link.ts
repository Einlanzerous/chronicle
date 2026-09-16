// Tier1Pane's in-corpus links (CHRN-58, review). The generator this corpus
// comes from emits inter-page links as root-absolute markdown links --
// `/repos/<slug>`, `/services/<slug>` -- and goldmark passes the `href`
// through untouched. Left alone, a click on one of those is a full browser
// navigation out of the SPA to the API's own root, which answers JSON (or a
// 401), not a page. Tier1Pane.vue's click handler defers the "is this an
// in-corpus link" question to this pure function so it has coverage
// independent of a DOM click.
//
// The shape to intercept is root-absolute with no scheme: starts with a
// single `/` (not `//`, which is protocol-relative -- `//host/x` -- and not
// a bare fragment or a scheme like `https:` or `mailto:`, none of which
// start with `/` at all). That single check is also sufficient to exclude
// every scheme: RFC 3986 requires a scheme to start with a letter, which a
// string starting with `/` never does.
export function tier1RoutePath(href: string): string | null {
  if (!href.startsWith('/') || href.startsWith('//')) return null
  return `/tier1${href}`
}
