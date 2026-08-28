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

// CHRN-19. The inbox is checked the same way as the audio root, and the two
// watcher durations are refused rather than clamped when they are nonsense — a
// settle window silently set to zero is how a half-written upload becomes a
// second memo.
func TestLoadWatcherSettings(t *testing.T) {
	withOwner(t)
	t.Setenv("CHRONICLE_DATABASE_URL", "postgres://x/y")

	t.Run("absolute inbox and explicit durations", func(t *testing.T) {
		t.Setenv("CHRONICLE_INBOX_DIR", "/data/chronicle/inbox")
		t.Setenv("CHRONICLE_WATCH_INTERVAL", "2s")
		t.Setenv("CHRONICLE_WATCH_SETTLE", "45s")
		c, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if c.InboxDir != "/data/chronicle/inbox" {
			t.Errorf("InboxDir = %q", c.InboxDir)
		}
		if c.WatchInterval != 2*time.Second || c.WatchSettle != 45*time.Second {
			t.Errorf("interval/settle = %v/%v", c.WatchInterval, c.WatchSettle)
		}
	})

	t.Run("unset durations mean the package default", func(t *testing.T) {
		t.Setenv("CHRONICLE_INBOX_DIR", "")
		t.Setenv("CHRONICLE_WATCH_INTERVAL", "")
		t.Setenv("CHRONICLE_WATCH_SETTLE", "")
		c, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if c.WatchInterval != 0 || c.WatchSettle != 0 {
			t.Errorf("interval/settle = %v/%v, want zero so internal/watch supplies the default",
				c.WatchInterval, c.WatchSettle)
		}
	})

	for name, v := range map[string]string{
		"zero":         "0s",
		"negative":     "-5s",
		"unparseable":  "soon",
		"bare integer": "10",
	} {
		t.Run("settle refuses "+name, func(t *testing.T) {
			t.Setenv("CHRONICLE_INBOX_DIR", "")
			t.Setenv("CHRONICLE_WATCH_INTERVAL", "")
			t.Setenv("CHRONICLE_WATCH_SETTLE", v)
			if _, err := Load(); err == nil {
				t.Errorf("Load accepted CHRONICLE_WATCH_SETTLE=%q", v)
			}
		})
	}

	t.Run("relative inbox is refused", func(t *testing.T) {
		t.Setenv("CHRONICLE_WATCH_SETTLE", "")
		t.Setenv("CHRONICLE_INBOX_DIR", "data/inbox")
		if _, err := Load(); err == nil {
			t.Error("Load accepted a relative inbox root")
		}
	})
}

// CHRN-75. The secret is the whole trust decision now; the CIDR list it
// replaced is noticed only so the warning can name it.
func TestLoadProxySecretAndTheRetiredCIDRList(t *testing.T) {
	withOwner(t)
	t.Setenv("CHRONICLE_DATABASE_URL", "postgres://x/y")

	t.Run("set", func(t *testing.T) {
		t.Setenv("CHRONICLE_TRUSTED_PROXIES", "")
		t.Setenv("CHRONICLE_PROXY_SECRET", "  config-test-fixture  ")
		c, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if c.ProxySecret != "config-test-fixture" {
			t.Errorf("ProxySecret = %q, want it trimmed", c.ProxySecret)
		}
		if c.RetiredTrustedProxies {
			t.Error("RetiredTrustedProxies is true with the variable unset")
		}
	})

	// The load-bearing half: a value that used to be parsed, and used to be
	// able to fail parsing, must now be ignored without erroring. compose pins
	// :latest and construct-server still sets this, so erroring here is a crash
	// loop the moment the image lands ahead of the SERV change.
	t.Run("the retired variable is noticed, never parsed, and never fatal", func(t *testing.T) {
		for _, v := range []string{"172.16.0.0/12", "not-an-ip-at-all", "10.0.0.1, garbage/99"} {
			t.Setenv("CHRONICLE_PROXY_SECRET", "config-test-fixture")
			t.Setenv("CHRONICLE_TRUSTED_PROXIES", v)
			c, err := Load()
			if err != nil {
				t.Fatalf("Load errored on a retired variable (%q): %v", v, err)
			}
			if !c.RetiredTrustedProxies {
				t.Errorf("RetiredTrustedProxies = false with %q set", v)
			}
		}
	})

	t.Run("unset means believe nobody", func(t *testing.T) {
		t.Setenv("CHRONICLE_TRUSTED_PROXIES", "")
		t.Setenv("CHRONICLE_PROXY_SECRET", "")
		c, err := Load()
		if err != nil {
			t.Fatalf("Load: %v", err)
		}
		if c.ProxySecret != "" {
			t.Errorf("ProxySecret = %q", c.ProxySecret)
		}
	})
}

// CHRN-27. The ASR pair is both-or-neither, for the reason the Cloudflare pair
// is: half-configured, every submission fails with a 401 that reads like the
// token was rejected rather than never set.
func TestASRCredentialsAreBothOrNeither(t *testing.T) {
	base := map[string]string{"CHRONICLE_DATABASE_URL": "postgres://x/y"}

	t.Run("neither is fine", func(t *testing.T) {
		c := loadWith(t, base)
		if c.TranscriptionEnabled() {
			t.Fatal("transcription reported as enabled with no URL")
		}
	})

	t.Run("both is fine", func(t *testing.T) {
		c := loadWith(t, merge(base, map[string]string{
			"CHRONICLE_ASR_URL":   "http://asr:4011/",
			"CHRONICLE_ASR_TOKEN": "a-token",
		}))
		if !c.TranscriptionEnabled() {
			t.Fatal("transcription reported as disabled with both set")
		}
		// The trailing slash is trimmed: the generated client appends its own
		// paths, and a doubled slash is a 404 nobody reads as a config typo.
		if c.ASRBaseURL != "http://asr:4011" {
			t.Fatalf("base URL = %q", c.ASRBaseURL)
		}
	})

	for _, only := range []string{"CHRONICLE_ASR_URL", "CHRONICLE_ASR_TOKEN"} {
		t.Run("only "+only+" is an error", func(t *testing.T) {
			env := merge(base, map[string]string{only: "value-aaaaaaaaaaaaaaaaaaaa"})
			for k, v := range env {
				t.Setenv(k, v)
			}
			if _, err := Load(); err == nil {
				t.Fatalf("setting only %s was accepted", only)
			}
		})
	}
}

func loadWith(t *testing.T, env map[string]string) Config {
	t.Helper()
	for k, v := range env {
		t.Setenv(k, v)
	}
	c, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return c
}

func merge(a, b map[string]string) map[string]string {
	out := make(map[string]string, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
