// Package invite builds the link that carries a one-time invite to another
// device (CHRN-71). CHRN-59's Android onboarding scans it as a QR: one scan
// sets both the server address and the invite, so nobody types a URL.
package invite

import (
	"fmt"
	"net/url"
	"strings"
)

// NormalizeBase canonicalizes an origin for SignInURL — trimmed, no trailing
// slash — and rejects anything that is not an absolute http(s) URL with a host.
// An empty base is not an error: it means "not configured".
//
// Validating matters more here than the usual config-typo argument, and Lyceum
// learned it the hard way (LYCM-102). A base that cannot be resolved is worse
// than no base at all, because clients prefer the server's URL over one they
// would have built themselves: a malformed value both produces a QR that scans
// to nothing and switches off the fallback that would have worked. Refusing it
// leaves that fallback in place.
func NormalizeBase(base string) (string, error) {
	trimmed := strings.TrimRight(strings.TrimSpace(base), "/")
	if trimmed == "" {
		return "", nil
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("%q is not a URL: %w", base, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("%q needs an http:// or https:// scheme", base)
	}
	if u.Host == "" {
		return "", fmt.Errorf("%q has no host", base)
	}
	return trimmed, nil
}

// SignInURL builds the scannable `<base>/sign-in?token=…` redemption link, or
// "" when no base is configured.
//
// One encoder, deliberately not in either caller: the API hands the URL back in
// the invite payload and `chronicle mint-invite` renders it at a terminal. When
// each client built the link from whatever origin it happened to know, they
// disagreed — which is how Lyceum's web app came to encode its own
// Cloudflare-gated origin into a QR meant for a phone that cannot pass that
// gate. Only the server knows which of its origins a phone can reach.
func SignInURL(base, token string) string {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return ""
	}
	return base + "/sign-in?token=" + url.QueryEscape(token)
}
