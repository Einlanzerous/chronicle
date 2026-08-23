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

func TestLoadFallsBackToDatabaseURL(t *testing.T) {
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
	} {
		t.Run(tc.key+"="+tc.val, func(t *testing.T) {
			t.Setenv("CHRONICLE_DATABASE_URL", "postgres://x/y")
			t.Setenv(tc.key, tc.val)
			if _, err := Load(); err == nil {
				t.Errorf("%s=%q was accepted; want an error naming the variable", tc.key, tc.val)
			}
		})
	}
}
