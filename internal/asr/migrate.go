package asr

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Einlanzerous/chronicle/internal/asr/migrations"
)

// This migrator is a deliberate second copy of the one in internal/store, and
// the duplication is the point rather than an oversight.
//
// The obvious alternative — extract a shared migrator taking an fs.FS — was
// considered and rejected twice over. It would rewrite Chronicle's own migrator
// and its tests from inside an E3 ticket, which is exactly the "wants to touch
// something outside its own epic" the working agreement says to stop on. And
// the two need not stay in sync: CHRN-29 names the moment this service may move
// to its own repository, at which point a shared package has to be un-shared
// again.
//
// What must NOT be shared is the embedded FS, and that is the hazard the
// decision actually names: the repo-root `migrations` package embeds
// Chronicle's SQL, so a second service reusing it would apply tier1, tier2,
// users and memos to the `asr` database.

// migration is a single parsed migration, both directions.
type migration struct {
	version string // numeric prefix, e.g. "0001"
	name    string // base name, e.g. "0001_jobs"
	up      string
	down    string
}

// Migrate applies all pending up migrations embedded under
// internal/asr/migrations, in ascending version order. Each runs in its own
// transaction and is recorded in schema_migrations. Idempotent, so it is safe
// to call on every boot.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	if err := ensureTable(ctx, pool); err != nil {
		return err
	}
	migs, err := loadMigrations()
	if err != nil {
		return err
	}
	applied, err := appliedVersions(ctx, pool)
	if err != nil {
		return err
	}
	for _, m := range migs {
		if applied[m.version] {
			continue
		}
		if err := applyOne(ctx, pool, m.up, m.version, true); err != nil {
			return fmt.Errorf("asr: apply %s up: %w", m.name, err)
		}
	}
	return nil
}

// MigrateDown rolls back the n most recently applied migrations, newest first.
// n <= 0 rolls back everything.
func MigrateDown(ctx context.Context, pool *pgxpool.Pool, n int) error {
	if err := ensureTable(ctx, pool); err != nil {
		return err
	}
	migs, err := loadMigrations()
	if err != nil {
		return err
	}
	applied, err := appliedVersions(ctx, pool)
	if err != nil {
		return err
	}

	sort.Slice(migs, func(i, j int) bool { return migs[i].version > migs[j].version })

	done := 0
	for _, m := range migs {
		if n > 0 && done >= n {
			break
		}
		if !applied[m.version] {
			continue
		}
		if err := applyOne(ctx, pool, m.down, m.version, false); err != nil {
			return fmt.Errorf("asr: apply %s down: %w", m.name, err)
		}
		done++
	}
	return nil
}

func ensureTable(ctx context.Context, pool *pgxpool.Pool) error {
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("asr: ensure schema_migrations: %w", err)
	}
	return nil
}

// applyOne runs one migration body and records or removes its version, in a
// single transaction so the two can never disagree.
func applyOne(ctx context.Context, pool *pgxpool.Pool, body, version string, up bool) error {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, body); err != nil {
		return err
	}
	if up {
		_, err = tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version)
	} else {
		_, err = tx.Exec(ctx, `DELETE FROM schema_migrations WHERE version = $1`, version)
	}
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func appliedVersions(ctx context.Context, pool *pgxpool.Pool) (map[string]bool, error) {
	rows, err := pool.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("asr: query schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

// loadMigrations reads every embedded pair and fails loudly on a missing down
// file. An up migration with no down is a migration nobody can reverse, and
// discovering that during an incident is the wrong time.
func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return nil, fmt.Errorf("asr: read migrations dir: %w", err)
	}

	var migs []migration
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		base := strings.TrimSuffix(name, ".up.sql")
		version, _, ok := strings.Cut(base, "_")
		if !ok || version == "" {
			return nil, fmt.Errorf("asr: migration %q lacks a version prefix", name)
		}
		up, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			return nil, fmt.Errorf("asr: read %s: %w", name, err)
		}
		down, err := fs.ReadFile(migrations.FS, base+".down.sql")
		if err != nil {
			return nil, fmt.Errorf("asr: %s has no .down.sql: %w", base, err)
		}
		migs = append(migs, migration{
			version: version, name: base,
			up: string(up), down: string(down),
		})
	}

	sort.Slice(migs, func(i, j int) bool { return migs[i].version < migs[j].version })
	return migs, nil
}
