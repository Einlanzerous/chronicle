package api

// Cloudflare Access JWT verification (CHRN-71), ported from Lyceum's
// internal/api/cfaccess.go, which mirrors Switchyard's SWY-161.
//
// A browser reaching Chronicle through the tunnel has already cleared Access,
// and the edge injects a `Cf-Access-Jwt-Assertion` header carrying a
// Cloudflare-signed identity. handleAuthCFAccess turns that into a Chronicle
// session, so there is no second login.
//
// Chronicle verifies the JWT itself rather than trusting the header, even
// though Traefik's cf-access-jwt middleware (SERV-106) already verified it on
// the internal entrypoint and strip-cf-access removes it on the public one.
// The reason is not distrust of the edge: it is that trusting the header makes
// correctness depend on a middleware staying attached to a router in a
// different repo. Chronicle publishes a direct router on the WAN-forwarded
// entrypoint; the day that router is edited and the strip is dropped, a forged
// header would authenticate as anybody. Verifying costs one cached JWKS fetch
// and closes that off for good.
//
// Hand-rolled rather than a JWT dependency: the check is narrow and fixed —
// RS256 against a published JWKS, one issuer, one audience — and the signature
// verification uses only audited stdlib primitives. The classic footguns are
// closed explicitly: the algorithm is pinned to RS256, so a token cannot
// downgrade to `none` and an RSA public key can never be replayed as an HMAC
// secret, and issuer, audience and expiry are all checked rather than trusted.

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// cfJWKSCacheMaxAge bounds how long a fetched key set is served before a
	// refresh; cfJWKSRefreshCooldown throttles refetches triggered by an
	// unknown key id, so a burst of tokens signed by a rotated-away key cannot
	// hammer the certs endpoint.
	cfJWKSCacheMaxAge     = 10 * time.Minute
	cfJWKSRefreshCooldown = 30 * time.Second
	cfJWKSFetchTimeout    = 10 * time.Second
)

// CFAccessVerifier verifies Access JWTs for one Access application, caching the
// team domain's JWKS in memory. Build it with NewCFAccessVerifier; the zero
// value is not usable.
type CFAccessVerifier struct {
	issuer     string // https://<teamDomain>
	aud        string // the Access application's audience tag
	certsURL   string // https://<teamDomain>/cdn-cgi/access/certs
	httpClient *http.Client

	mu        sync.RWMutex
	keys      map[string]*rsa.PublicKey // kid -> key
	fetchedAt time.Time                 // when keys was last populated
	lastFetch time.Time                 // when a fetch was last attempted
}

// NewCFAccessVerifier builds a verifier for a team domain and audience tag.
// Keys are fetched lazily on first use, so construction cannot fail and boot
// does not depend on Cloudflare being reachable.
func NewCFAccessVerifier(teamDomain, aud string) *CFAccessVerifier {
	teamDomain = strings.TrimSpace(teamDomain)
	return &CFAccessVerifier{
		issuer:     "https://" + teamDomain,
		aud:        strings.TrimSpace(aud),
		certsURL:   "https://" + teamDomain + "/cdn-cgi/access/certs",
		httpClient: &http.Client{Timeout: cfJWKSFetchTimeout},
	}
}

// errCFAccessInvalid is the single opaque error every verification failure maps
// to, so a probe cannot distinguish a bad signature from a wrong audience from
// an expired token.
var errCFAccessInvalid = errors.New("invalid Cloudflare Access token")

// cfAccessAudience decodes the `aud` claim, which Cloudflare emits as either a
// string or an array of strings.
type cfAccessAudience []string

func (a *cfAccessAudience) UnmarshalJSON(b []byte) error {
	var one string
	if err := json.Unmarshal(b, &one); err == nil {
		*a = cfAccessAudience{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return err
	}
	*a = many
	return nil
}

func (a cfAccessAudience) contains(want string) bool {
	for _, v := range a {
		if v == want {
			return true
		}
	}
	return false
}

// cfAccessClaims is the subset of the Access JWT payload Chronicle reads.
type cfAccessClaims struct {
	Iss   string           `json:"iss"`
	Aud   cfAccessAudience `json:"aud"`
	Exp   int64            `json:"exp"`
	Nbf   int64            `json:"nbf"`
	Email string           `json:"email"`
}

// jwtHeader is the decoded JWS header.
type jwtHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
}

// Verify checks a Cf-Access-Jwt-Assertion value and returns the verified email.
// It enforces, in order: three JWS segments, RS256, a known signing key, a
// valid RSA signature, the exact issuer and audience, expiry (and not-before
// when present), and a non-empty email. Every failure returns the same error.
func (v *CFAccessVerifier) Verify(ctx context.Context, token string) (string, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", errCFAccessInvalid
	}

	var hdr jwtHeader
	if err := decodeSegment(parts[0], &hdr); err != nil {
		return "", errCFAccessInvalid
	}
	// Pinning the algorithm here is what closes the downgrade holes: only the
	// RSA path below is ever reachable.
	if hdr.Alg != "RS256" || hdr.Kid == "" {
		return "", errCFAccessInvalid
	}

	key, err := v.key(ctx, hdr.Kid)
	if err != nil {
		return "", errCFAccessInvalid
	}

	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return "", errCFAccessInvalid
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, digest[:], sig); err != nil {
		return "", errCFAccessInvalid
	}

	var claims cfAccessClaims
	if err := decodeSegment(parts[1], &claims); err != nil {
		return "", errCFAccessInvalid
	}
	if claims.Iss != v.issuer || !claims.Aud.contains(v.aud) {
		return "", errCFAccessInvalid
	}
	now := time.Now()
	if claims.Exp == 0 || now.After(time.Unix(claims.Exp, 0)) {
		return "", errCFAccessInvalid
	}
	if claims.Nbf != 0 && now.Before(time.Unix(claims.Nbf, 0)) {
		return "", errCFAccessInvalid
	}
	if claims.Email == "" {
		return "", errCFAccessInvalid
	}
	return claims.Email, nil
}

// key returns the RSA public key for kid, refreshing the JWKS when the cache is
// stale or the key is unknown. A stale-but-present key beats a failed refresh,
// so a transient certs-endpoint outage does not reject a token signed by a key
// already held.
func (v *CFAccessVerifier) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	v.mu.RLock()
	k, ok := v.keys[kid]
	fresh := time.Since(v.fetchedAt) < cfJWKSCacheMaxAge
	v.mu.RUnlock()
	if ok && fresh {
		return k, nil
	}

	if err := v.refresh(ctx); err != nil {
		if ok {
			return k, nil
		}
		return nil, err
	}

	v.mu.RLock()
	k, ok = v.keys[kid]
	v.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("cf access: no signing key for kid %q", kid)
	}
	return k, nil
}

// refresh refetches the JWKS, subject to a cooldown once a key set is held so
// repeated unknown-kid tokens cannot spin the certs endpoint.
func (v *CFAccessVerifier) refresh(ctx context.Context) error {
	v.mu.RLock()
	cooling := v.keys != nil && time.Since(v.lastFetch) < cfJWKSRefreshCooldown
	v.mu.RUnlock()
	if cooling {
		return nil
	}

	keys, err := fetchJWKS(ctx, v.httpClient, v.certsURL)

	v.mu.Lock()
	v.lastFetch = time.Now()
	if err == nil {
		v.keys = keys
		v.fetchedAt = time.Now()
	}
	v.mu.Unlock()
	return err
}

// jwksResponse is the shape of Cloudflare's certs endpoint.
type jwksResponse struct {
	Keys []struct {
		Kty string `json:"kty"`
		Kid string `json:"kid"`
		N   string `json:"n"` // base64url big-endian modulus
		E   string `json:"e"` // base64url big-endian exponent
	} `json:"keys"`
}

// fetchJWKS retrieves and parses the RSA keys at certsURL, keyed by kid.
func fetchJWKS(ctx context.Context, client *http.Client, certsURL string) (map[string]*rsa.PublicKey, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, certsURL, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cf access: fetch jwks: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("cf access: fetch jwks: status %d", resp.StatusCode)
	}

	var jwks jwksResponse
	if err := json.NewDecoder(resp.Body).Decode(&jwks); err != nil {
		return nil, fmt.Errorf("cf access: decode jwks: %w", err)
	}

	keys := make(map[string]*rsa.PublicKey, len(jwks.Keys))
	for _, k := range jwks.Keys {
		if k.Kty != "RSA" || k.Kid == "" {
			continue
		}
		pub, err := rsaPublicKey(k.N, k.E)
		if err != nil {
			continue // skip one malformed key rather than fail the whole set
		}
		keys[k.Kid] = pub
	}
	if len(keys) == 0 {
		return nil, errors.New("cf access: jwks had no usable RSA keys")
	}
	return keys, nil
}

// rsaPublicKey rebuilds an RSA public key from a JWK's base64url modulus and
// exponent.
func rsaPublicKey(nB64, eB64 string) (*rsa.PublicKey, error) {
	nBytes, err := base64.RawURLEncoding.DecodeString(nB64)
	if err != nil {
		return nil, err
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(eB64)
	if err != nil {
		return nil, err
	}
	// The upper bound is not paranoia about RSA: without it, a >8-byte exponent
	// from a hostile or malformed key set slices eBuf with a negative index and
	// panics the process. A real exponent is three bytes.
	if len(nBytes) == 0 || len(eBytes) == 0 || len(eBytes) > 8 {
		return nil, errors.New("cf access: unusable RSA modulus or exponent")
	}
	var eBuf [8]byte
	copy(eBuf[8-len(eBytes):], eBytes)
	e := binary.BigEndian.Uint64(eBuf[:])
	if e == 0 || e > 1<<31-1 {
		return nil, errors.New("cf access: RSA exponent out of range")
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: int(e)}, nil
}

// decodeSegment base64url-decodes a JWS segment and unmarshals its JSON.
func decodeSegment(seg string, v any) error {
	raw, err := base64.RawURLEncoding.DecodeString(seg)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, v)
}
