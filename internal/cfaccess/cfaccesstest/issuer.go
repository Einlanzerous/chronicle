// Package cfaccesstest stands in for Cloudflare Access in tests: an issuer
// with a key it generated, the JWKS endpoint that publishes it, and a way to
// sign whatever assertion a test needs -- including the wrong ones.
//
// It exists because the accepting path of internal/cfaccess cannot be reached
// any other way. Every test before CHRN-65 exercised only refusals, which need
// no key; a test that an assertion for one audience is treated differently
// from an assertion for another needs two that verify.
//
// It is a package rather than a _test.go file because it has two users in two
// packages: the sign-in exchange's tests in internal/api, and the hosted MCP
// transport's.
package cfaccesstest

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Einlanzerous/chronicle/internal/cfaccess"
)

// Team is the team domain every Issuer signs for. It never resolves; the keys
// are served from the Issuer's own listener.
const Team = "team.invalid"

// Issuer signs Access assertions and serves the matching key set.
type Issuer struct {
	key *rsa.PrivateKey
	kid string
	srv *httptest.Server
}

// New starts an issuer and stops it when the test ends.
func New(t testing.TB) *Issuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("cfaccesstest: generate key: %v", err)
	}
	i := &Issuer{key: key, kid: "test-key"}
	i.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{i.jwk(i.kid, &i.key.PublicKey)}})
	}))
	t.Cleanup(i.srv.Close)
	return i
}

func (i *Issuer) jwk(kid string, pub *rsa.PublicKey) map[string]string {
	return map[string]string{
		"kty": "RSA", "use": "sig", "alg": "RS256", "kid": kid,
		"n": base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
	}
}

// Verifier builds a verifier that trusts this issuer for the given audiences.
func (i *Issuer) Verifier(aud ...string) *cfaccess.Verifier {
	return cfaccess.New(Team, aud, cfaccess.WithCertsURL(i.srv.URL))
}

// CertsURL is where this issuer publishes its keys, for a caller that builds
// its verifier some other way.
func (i *Issuer) CertsURL() string { return i.srv.URL }

// Claims is an assertion's payload. The zero value of a field is omitted, so a
// test states only what it is about.
type Claims struct {
	Email    string
	Audience string
	Issuer   string        // defaults to https://<Team>
	Expires  time.Duration // from now; defaults to five minutes. Negative is already expired.
}

// Sign returns an assertion this issuer's verifiers accept, given claims they
// would accept.
func (i *Issuer) Sign(t testing.TB, c Claims) string {
	t.Helper()
	return sign(t, i.key, i.kid, c)
}

// SignWithUnknownKey returns an assertion that is well-formed and correctly
// claimed but signed by a key this issuer does not publish.
func (i *Issuer) SignWithUnknownKey(t testing.TB, c Claims) string {
	t.Helper()
	other, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("cfaccesstest: generate key: %v", err)
	}
	return sign(t, other, "unknown-key", c)
}

func sign(t testing.TB, key *rsa.PrivateKey, kid string, c Claims) string {
	t.Helper()
	if c.Issuer == "" {
		c.Issuer = "https://" + Team
	}
	if c.Expires == 0 {
		c.Expires = 5 * time.Minute
	}
	payload := map[string]any{"iss": c.Issuer, "exp": time.Now().Add(c.Expires).Unix()}
	if c.Email != "" {
		payload["email"] = c.Email
	}
	if c.Audience != "" {
		payload["aud"] = []string{c.Audience}
	}
	seg := func(v any) string {
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatalf("cfaccesstest: marshal: %v", err)
		}
		return base64.RawURLEncoding.EncodeToString(raw)
	}
	signed := seg(map[string]string{"alg": "RS256", "kid": kid, "typ": "JWT"}) + "." + seg(payload)
	digest := sha256.Sum256([]byte(signed))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, digest[:])
	if err != nil {
		t.Fatalf("cfaccesstest: sign: %v", err)
	}
	return signed + "." + base64.RawURLEncoding.EncodeToString(sig)
}
