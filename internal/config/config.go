// Package config loads Chronicle's configuration from the environment. No
// config files — house style is env-only, CHRONICLE_-prefixed, with a
// DATABASE_URL fallback so the shared-Postgres convention keeps working.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Default listen port. 4009 is the lowest free slot in the estate's 40xx block
// (4001-4008, 4010, 4012-4013 are taken).
const DefaultPort = 4009

// Config is the process-wide configuration.
type Config struct {
	DatabaseURL   string        // pgx DSN for Chronicle's own database
	Addr          string        // listen address, e.g. ":4009"
	LogLevel      slog.Level    // debug | info | warn | error
	LogFormat     string        // json | text
	ShutdownGrace time.Duration // how long in-flight work has to finish on SIGTERM
}

// Load reads configuration from the environment, naming the offending variable
// rather than failing later at connect time.
func Load() (Config, error) {
	var c Config
	var err error

	c.DatabaseURL = firstNonEmpty(os.Getenv("CHRONICLE_DATABASE_URL"), os.Getenv("DATABASE_URL"))
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("config: CHRONICLE_DATABASE_URL (or DATABASE_URL) is required")
	}

	port := DefaultPort
	if v := strings.TrimSpace(os.Getenv("CHRONICLE_PORT")); v != "" {
		port, err = strconv.Atoi(v)
		if err != nil || port < 1 || port > 65535 {
			return c, fmt.Errorf("config: CHRONICLE_PORT %q is not a valid port", v)
		}
	}
	c.Addr = fmt.Sprintf(":%d", port)

	c.LogLevel, err = parseLevel(firstNonEmpty(os.Getenv("CHRONICLE_LOG_LEVEL"), "info"))
	if err != nil {
		return c, err
	}

	// JSON by default: Datadog parses it into attributes with no pipeline
	// config, and Dozzle renders it fine. text is for a human at a terminal.
	c.LogFormat = strings.ToLower(firstNonEmpty(os.Getenv("CHRONICLE_LOG_FORMAT"), "json"))
	if c.LogFormat != "json" && c.LogFormat != "text" {
		return c, fmt.Errorf("config: CHRONICLE_LOG_FORMAT %q is not json or text", c.LogFormat)
	}

	c.ShutdownGrace = 20 * time.Second
	if v := strings.TrimSpace(os.Getenv("CHRONICLE_SHUTDOWN_GRACE")); v != "" {
		c.ShutdownGrace, err = time.ParseDuration(v)
		if err != nil || c.ShutdownGrace <= 0 {
			return c, fmt.Errorf("config: CHRONICLE_SHUTDOWN_GRACE %q is not a positive duration", v)
		}
	}
	return c, nil
}

// Logger builds the process logger this config describes.
func (c Config) Logger(w *os.File) *slog.Logger {
	opts := &slog.HandlerOptions{Level: c.LogLevel}
	if c.LogFormat == "text" {
		return slog.New(slog.NewTextHandler(w, opts))
	}
	return slog.New(slog.NewJSONHandler(w, opts))
}

func parseLevel(s string) (slog.Level, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug, nil
	case "info":
		return slog.LevelInfo, nil
	case "warn", "warning":
		return slog.LevelWarn, nil
	case "error":
		return slog.LevelError, nil
	default:
		return 0, fmt.Errorf("config: CHRONICLE_LOG_LEVEL %q is not debug/info/warn/error", s)
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
