// Package config loads Chronicle's configuration from the environment. No
// config files — house style is env-only, CHRONICLE_-prefixed, with a
// DATABASE_URL fallback so the shared-Postgres convention keeps working.
//
// CHRN-15 extends this with the service's own settings; CHRN-14 needs only
// enough to reach the database.
package config

import (
	"fmt"
	"os"
	"strings"
)

// Config is the process-wide configuration.
type Config struct {
	// DatabaseURL is the pgx DSN for Chronicle's own database.
	DatabaseURL string
}

// Load reads configuration from the environment, returning an error naming the
// variable that is missing rather than failing later at connect time.
func Load() (Config, error) {
	var c Config

	c.DatabaseURL = firstNonEmpty(
		os.Getenv("CHRONICLE_DATABASE_URL"),
		os.Getenv("DATABASE_URL"),
	)
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("config: CHRONICLE_DATABASE_URL (or DATABASE_URL) is required")
	}
	return c, nil
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
