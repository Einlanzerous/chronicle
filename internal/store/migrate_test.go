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

// TestTierRoleGrantsAreExactlyWhatWasDecided is the doctrine, asserted. CHRN-52
// owns the full isolation proof; this is the cheap version that fails the moment
// a grant moves without a decision behind it — which is precisely what it did
// when 0007 landed, and why it is worth having.
//
// IT USED TO ASSERT NO USAGE ON tier2 AT ALL. CHRN-32's ruling R4 changed that
// deliberately: CLAUDE.md defines tier 1 to include what Chronicle "derives from
// its own corpus", every example of which derives from tier 2, so a role with no
// read on tier 2 could not implement the definition. 0007 grants USAGE on the
// schema plus SELECT on two named tables.
//
// The doctrine is unmoved, because the doctrine is about WRITES. So the table
// below now pins the shape of the compromise rather than a flat denial: schema
// USAGE yes, schema CREATE no, and — the part that stops this becoming a
// rubber stamp — no write privilege on either table, on any table.
func TestTierRoleGrantsAreExactlyWhatWasDecided(t *testing.T) {
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
		// R4. Reading the corpus is what a derived writer is for.
		{"tier2", "USAGE", true},
		// And nothing more than reading it. CREATE would let a tier-1 process
		// add a tier-2 table and then own it outright.
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

	// Schema USAGE on its own grants nothing on a table, so the assertions
	// above would still pass if 0007 had handed over every privilege on every
	// table. THIS is the part that fails when a grant is loosened.
	for _, tc := range []struct {
		table string
		priv  string
		want  bool
	}{
		{"tier2.memos", "SELECT", true},
		{"tier2.transcripts", "SELECT", true},

		// Not granted, and the standing proof that 0007 used no ALTER DEFAULT
		// PRIVILEGES: a tier-2 table it did not name is unreachable, so one
		// added tomorrow stays unreachable until somebody decides otherwise.
		{"tier2.memo_arrivals", "SELECT", false},
		{"tier2.users", "SELECT", false},
		{"tier2.user_tokens", "SELECT", false},

		// The line itself, on the two tables that ARE readable.
		{"tier2.memos", "INSERT", false},
		{"tier2.memos", "UPDATE", false},
		{"tier2.memos", "DELETE", false},
		{"tier2.transcripts", "INSERT", false},
		{"tier2.transcripts", "UPDATE", false},
		{"tier2.transcripts", "DELETE", false},
	} {
		var got bool
		err := pool.QueryRow(ctx,
			`SELECT has_table_privilege('chronicle_tier1', $1, $2)`,
			tc.table, tc.priv).Scan(&got)
		if err != nil {
			t.Fatalf("has_table_privilege(%s, %s): %v", tc.table, tc.priv, err)
		}
		if got != tc.want {
			t.Errorf("chronicle_tier1 %s on %s = %v, want %v", tc.priv, tc.table, got, tc.want)
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
