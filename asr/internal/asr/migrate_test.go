package asr

import (
	"context"
	"strings"
	"testing"
)

// An up migration with no down is one nobody can reverse, and discovering that
// during an incident is the wrong time. Chronicle asserts the same thing about
// its own set; this is the ASR service's, which is a DIFFERENT embedded FS —
// the point of the separate migrations package.
func TestLoadMigrationsHasBothDirections(t *testing.T) {
	migs, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	if len(migs) == 0 {
		t.Fatal("no migrations were embedded")
	}
	for _, m := range migs {
		if m.up == "" {
			t.Errorf("%s has an empty up migration", m.name)
		}
		if m.down == "" {
			t.Errorf("%s has no down migration", m.name)
		}
	}
}

// The ASR migrator must not be able to reach Chronicle's SQL. If it ever did,
// running asrd would apply tier1, tier2, users and memos to the `asr` database
// — the exact failure the separate embed exists to prevent.
func TestTheASRMigratorEmbedsOnlyItsOwnSQL(t *testing.T) {
	migs, err := loadMigrations()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range migs {
		for _, forbidden := range []string{"tier1.", "tier2.", "CREATE SCHEMA tier"} {
			if strings.Contains(m.up, forbidden) {
				t.Errorf("%s mentions %q — this is not Chronicle's database", m.name, forbidden)
			}
		}
	}
}

func TestMigrateRoundTrip(t *testing.T) {
	ctx := context.Background()
	pool := testPool(t)

	if err := MigrateDown(ctx, pool, 0); err != nil {
		t.Fatalf("down: %v", err)
	}
	var exists bool
	if err := pool.QueryRow(ctx,
		`SELECT to_regclass('public.jobs') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if exists {
		t.Fatal("jobs survived a full rollback")
	}

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("up: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT to_regclass('public.jobs') IS NOT NULL`).Scan(&exists); err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Fatal("jobs was not recreated")
	}

	// Idempotent: a second up is a no-op rather than an error, because it runs
	// on every boot.
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("second up: %v", err)
	}
}
