package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// testDSN returns the test database DSN or skips. CI and verify.sh inject it
// from Signet; there is no fallback DSN baked into the tests on purpose.
func testDSN(t *testing.T) string {
	t.Helper()
	dsn := os.Getenv("CHRONICLE_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("CHRONICLE_TEST_DATABASE_URL not set; skipping database test")
	}
	return dsn
}

func TestLoadMigrationsHasBothDirections(t *testing.T) {
	migs, err := loadMigrations()
	if err != nil {
		t.Fatalf("loadMigrations: %v", err)
	}
	if len(migs) == 0 {
		t.Fatal("no migrations embedded")
	}
	for _, m := range migs {
		if m.up == "" {
			t.Errorf("%s: empty up migration", m.name)
		}
		// A migration with no down is one nobody can reverse. loadMigrations
		// is expected to have already failed, but assert it so a future
		// loosening of that rule breaks here loudly.
		if m.down == "" {
			t.Errorf("%s: empty down migration", m.name)
		}
	}
}

func TestMigrateRoundTrip(t *testing.T) {
	dsn := testDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	// Start from a known-empty state so the test is not order-dependent.
	if err := MigrateDown(ctx, pool, 0); err != nil {
		t.Fatalf("initial down: %v", err)
	}
	if n := countSchemas(ctx, t, pool); n != 0 {
		t.Fatalf("after down: want 0 tier schemas, got %d", n)
	}

	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("up: %v", err)
	}
	if n := countSchemas(ctx, t, pool); n != 2 {
		t.Fatalf("after up: want tier1+tier2, got %d schemas", n)
	}
	if n := countApplied(ctx, t, pool); n == 0 {
		t.Fatal("after up: no rows in schema_migrations")
	}

	// Idempotent: a second up is a no-op, not a duplicate-key error.
	before := countApplied(ctx, t, pool)
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("second up: %v", err)
	}
	if after := countApplied(ctx, t, pool); after != before {
		t.Fatalf("second up changed applied count: %d -> %d", before, after)
	}

	if err := MigrateDown(ctx, pool, 0); err != nil {
		t.Fatalf("down: %v", err)
	}
	if n := countSchemas(ctx, t, pool); n != 0 {
		t.Fatalf("after down: want 0 tier schemas, got %d", n)
	}
	if n := countApplied(ctx, t, pool); n != 0 {
		t.Fatalf("after down: want 0 applied, got %d", n)
	}

	// Leave the database migrated, as any other consumer expects to find it.
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("re-up: %v", err)
	}
}

// TestTierRoleCannotReachTier2 is the doctrine, asserted. CHRN-52 owns the
// full isolation proof; this is the cheap version that fails the moment a
// grant is loosened.
func TestTierRoleCannotReachTier2(t *testing.T) {
	dsn := testDSN(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pool, err := Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()
	if err := Migrate(ctx, pool); err != nil {
		t.Fatalf("up: %v", err)
	}

	for _, tc := range []struct {
		schema string
		priv   string
		want   bool
	}{
		{"tier1", "USAGE", true},
		{"tier1", "CREATE", true},
		{"tier2", "USAGE", false},
		{"tier2", "CREATE", false},
		{"public", "CREATE", false},
	} {
		var got bool
		err := pool.QueryRow(ctx,
			`SELECT has_schema_privilege('chronicle_tier1', $1, $2)`,
			tc.schema, tc.priv).Scan(&got)
		if err != nil {
			t.Fatalf("has_schema_privilege(%s, %s): %v", tc.schema, tc.priv, err)
		}
		if got != tc.want {
			t.Errorf("chronicle_tier1 %s on %s = %v, want %v", tc.priv, tc.schema, got, tc.want)
		}
	}
}

func countSchemas(ctx context.Context, t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	err := pool.QueryRow(ctx,
		`SELECT count(*) FROM pg_namespace WHERE nspname IN ('tier1','tier2')`).Scan(&n)
	if err != nil {
		t.Fatalf("count schemas: %v", err)
	}
	return n
}

func countApplied(ctx context.Context, t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&n)
	if err != nil {
		t.Fatalf("count schema_migrations: %v", err)
	}
	return n
}
