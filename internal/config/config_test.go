package config

import (
	"log/slog"
	"testing"
	"time"
)

func TestLoadRequiresDatabaseURL(t *testing.T) {
	t.Setenv("CHRONICLE_DATABASE_URL", "")
	t.Setenv("DATABASE_URL", "")
	if _, err := Load(); err == nil {
		t.Fatal("want an error when no database URL is set")
	}
}

// withOwner satisfies the one required variable a successful Load now needs,
// so each test states only the thing it is actually about.
func withOwner(t *testing.T) {
	t.Helper()
	t.Setenv("CHRONICLE_OWNER_EMAIL", "owner@example.com")
}

func TestLoadFallsBackToDatabaseURL(t *testing.T) {
	withOwner(t)
	t.Setenv("CHRONICLE_DATABASE_URL", "")
	t.Setenv("DATABASE_URL", "postgres://x/y")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.DatabaseURL != "postgres://x/y" {
		t.Errorf("DatabaseURL = %q", c.DatabaseURL)
	}
}

func TestLoadDefaults(t *testing.T) {
	withOwner(t)
	t.Setenv("CHRONICLE_DATABASE_URL", "postgres://x/y")
	for _, k := range []string{"CHRONICLE_PORT", "CHRONICLE_LOG_LEVEL", "CHRONICLE_LOG_FORMAT", "CHRONICLE_SHUTDOWN_GRACE"} {
		t.Setenv(k, "")
	}
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.Addr != ":4009" {
		t.Errorf("Addr = %q, want :4009", c.Addr)
	}
	if c.LogLevel != slog.LevelInfo {
		t.Errorf("LogLevel = %v, want info", c.LogLevel)
	}
	if c.LogFormat != "json" {
		t.Errorf("LogFormat = %q, want json", c.LogFormat)
	}
	if c.ShutdownGrace != 20*time.Second {
		t.Errorf("ShutdownGrace = %v, want 20s", c.ShutdownGrace)
	}
}

func TestLoadRejectsBadValues(t *testing.T) {
	for _, tc := range []struct{ key, val string }{
		{"CHRONICLE_PORT", "no"},
		{"CHRONICLE_PORT", "0"},
		{"CHRONICLE_PORT", "70000"},
		{"CHRONICLE_LOG_LEVEL", "chatty"},
		{"CHRONICLE_LOG_FORMAT", "xml"},
		{"CHRONICLE_SHUTDOWN_GRACE", "soon"},
		{"CHRONICLE_SHUTDOWN_GRACE", "-5s"},
		// A base that cannot be resolved is worse than none at all: clients
		// prefer the server's URL over the one they would have built, so it
		// must fail loudly rather than produce a QR that scans to nothing.
		{"CHRONICLE_MOBILE_BASE_URL", "chronicle.example.com"},
		{"CHRONICLE_MOBILE_BASE_URL", "ftp://chronicle.example.com"},
		{"CHRONICLE_MOBILE_BASE_URL", "https://"},
	} {
		t.Run(tc.key+"="+tc.val, func(t *testing.T) {
			withOwner(t)
			t.Setenv("CHRONICLE_DATABASE_URL", "postgres://x/y")
			t.Setenv(tc.key, tc.val)
			if _, err := Load(); err == nil {
				t.Errorf("%s=%q was accepted; want an error naming the variable", tc.key, tc.val)
			}
		})
	}
}

// CHRN-71: auth is unconditional, so a serving Chronicle needs an owner
// identity. Unset, the owner row keeps migration 0002's placeholder, which can
// never match a Cloudflare-verified email — SSO would look configured and
// silently never work.
//
// migrate and mint-invite do not need it, which is why the check is on serve
// rather than on Load.
func TestServeRequiresOwnerEmail(t *testing.T) {
	t.Setenv("CHRONICLE_DATABASE_URL", "postgres://x/y")
	t.Setenv("CHRONICLE_OWNER_EMAIL", "")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load should not require an owner email: %v", err)
	}
	if err := c.ValidateForServe(); err == nil {
		t.Error("ValidateForServe accepted an unset CHRONICLE_OWNER_EMAIL")
	}

	withOwner(t)
	c, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := c.ValidateForServe(); err != nil {
		t.Errorf("ValidateForServe: %v", err)
	}
}

// Half-configured SSO fails every browser sign-in with a message blaming the
// token rather than the server, so refuse it at boot instead.
func TestLoadRejectsHalfConfiguredSSO(t *testing.T) {
	for _, tc := range []struct{ name, domain, aud string }{
		{"domain without aud", "team.cloudflareaccess.com", ""},
		{"aud without domain", "", "abc123"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			withOwner(t)
			t.Setenv("CHRONICLE_DATABASE_URL", "postgres://x/y")
			t.Setenv("CHRONICLE_CF_ACCESS_TEAM_DOMAIN", tc.domain)
			t.Setenv("CHRONICLE_CF_ACCESS_AUD", tc.aud)
			if _, err := Load(); err == nil {
				t.Error("half-configured SSO was accepted; want a boot error")
			}
		})
	}
}

func TestLoadSSOEnabled(t *testing.T) {
	withOwner(t)
	t.Setenv("CHRONICLE_DATABASE_URL", "postgres://x/y")

	t.Setenv("CHRONICLE_CF_ACCESS_TEAM_DOMAIN", "")
	t.Setenv("CHRONICLE_CF_ACCESS_AUD", "")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.SSOEnabled() {
		t.Error("SSOEnabled with neither variable set")
	}

	t.Setenv("CHRONICLE_CF_ACCESS_TEAM_DOMAIN", "team.cloudflareaccess.com")
	t.Setenv("CHRONICLE_CF_ACCESS_AUD", "abc123")
	c, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !c.SSOEnabled() {
		t.Error("SSOEnabled false with both variables set")
	}
}

// The base is normalized once, at boot, so no caller has to think about a
// trailing slash.
func TestLoadNormalizesMobileBaseURL(t *testing.T) {
	withOwner(t)
	t.Setenv("CHRONICLE_DATABASE_URL", "postgres://x/y")
	t.Setenv("CHRONICLE_MOBILE_BASE_URL", "  https://chronicle.example.com/  ")
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.MobileBaseURL != "https://chronicle.example.com" {
		t.Errorf("MobileBaseURL = %q", c.MobileBaseURL)
	}
}

// CHRN-23. The audio root is where tier-2 recordings live, so a value that
// resolves differently depending on how the process was started is a corpus
// that gets half-pruned.
func TestLoadAudioDirMustBeAbsoluteOrAbsent(t *testing.T) {
	withOwner(t)
	t.Setenv("CHRONICLE_DATABASE_URL", "postgres://x/y")

	t.Run("absolute", func(t *testing.T) {
		t.Setenv("CHRONICLE_AUDIO_DIR", "/data/chronicle/audio")
		c, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if c.AudioDir != "/data/chronicle/audio" {
			t.Errorf("AudioDir = %q", c.AudioDir)
		}
	})

	t.Run("relative is refused", func(t *testing.T) {
		t.Setenv("CHRONICLE_AUDIO_DIR", "data/audio")
		if _, err := Load(); err == nil {
			t.Error("Load accepted a relative audio root")
		}
	})

	// Unset is allowed and means "no audio store". Nothing writes recordings
	// until CHRN-19 or CHRN-20 lands, so refusing to boot over a directory the
	// binary has no use for yet would be the worse default; the storage report
	// answers 503 naming the variable instead.
	t.Run("unset is allowed", func(t *testing.T) {
		t.Setenv("CHRONICLE_AUDIO_DIR", "")
		c, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if c.AudioDir != "" {
			t.Errorf("AudioDir = %q, want empty", c.AudioDir)
		}
	})
}
