package cfaccess_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Einlanzerous/chronicle/internal/cfaccess"
	"github.com/Einlanzerous/chronicle/internal/cfaccess/cfaccesstest"
)

const (
	webAUD = "web-application-tag"
	mcpAUD = "mcp-application-tag"
)

// The audience a token matched is the whole of what tells a browser from the
// hosted MCP endpoint, so it is reported, and reported as the CONFIGURED tag.
func TestVerifyReportsWhichAudienceMatched(t *testing.T) {
	iss := cfaccesstest.New(t)
	v := iss.Verifier(webAUD, mcpAUD)

	for _, aud := range []string{webAUD, mcpAUD} {
		id, err := v.Verify(context.Background(), iss.Sign(t, cfaccesstest.Claims{Email: "magos@example.com", Audience: aud}))
		if err != nil {
			t.Fatalf("Verify(%s): %v", aud, err)
		}
		if id.Email != "magos@example.com" || id.Audience != aud {
			t.Errorf("Verify(%s) = %+v, want that email and that audience", aud, id)
		}
	}
}

// Every refusal is the same opaque error, and none of them reports an identity.
func TestVerifyRefuses(t *testing.T) {
	iss := cfaccesstest.New(t)
	v := iss.Verifier(webAUD)
	good := cfaccesstest.Claims{Email: "magos@example.com", Audience: webAUD}

	with := func(f func(*cfaccesstest.Claims)) cfaccesstest.Claims { c := good; f(&c); return c }
	cases := map[string]string{
		"another application's audience":  iss.Sign(t, with(func(c *cfaccesstest.Claims) { c.Audience = mcpAUD })),
		"no audience":                     iss.Sign(t, with(func(c *cfaccesstest.Claims) { c.Audience = "" })),
		"another team's issuer":           iss.Sign(t, with(func(c *cfaccesstest.Claims) { c.Issuer = "https://elsewhere.invalid" })),
		"expired":                         iss.Sign(t, with(func(c *cfaccesstest.Claims) { c.Expires = -time.Minute })),
		"no email":                        iss.Sign(t, with(func(c *cfaccesstest.Claims) { c.Email = "" })),
		"a key the team does not publish": iss.SignWithUnknownKey(t, good),
		"not a JWT":                       "nonsense",
		"alg none":                        "eyJhbGciOiJub25lIn0.eyJlbWFpbCI6Im1hZ29zQGV4YW1wbGUuY29tIn0.",
	}
	for name, token := range cases {
		t.Run(name, func(t *testing.T) {
			id, err := v.Verify(context.Background(), token)
			if !errors.Is(err, cfaccess.ErrInvalid) {
				t.Fatalf("err = %v, want ErrInvalid", err)
			}
			if id != (cfaccess.Identity{}) {
				t.Errorf("a refused token reported an identity: %+v", id)
			}
		})
	}
}

// "No audience configured" must never read as "any audience will do".
func TestVerifierWithNoAudienceAcceptsNothing(t *testing.T) {
	iss := cfaccesstest.New(t)
	v := iss.Verifier()
	_, err := v.Verify(context.Background(), iss.Sign(t, cfaccesstest.Claims{Email: "magos@example.com", Audience: webAUD}))
	if !errors.Is(err, cfaccess.ErrInvalid) {
		t.Fatalf("err = %v, want ErrInvalid", err)
	}
}

func TestNormalizeTeamDomain(t *testing.T) {
	for in, want := range map[string]string{
		"team.cloudflareaccess.com":          "team.cloudflareaccess.com",
		"https://team.cloudflareaccess.com/": "team.cloudflareaccess.com",
		"  http://team.cloudflareaccess.com": "team.cloudflareaccess.com",
	} {
		if got := cfaccess.NormalizeTeamDomain(in); got != want {
			t.Errorf("NormalizeTeamDomain(%q) = %q, want %q", in, got, want)
		}
	}
}
