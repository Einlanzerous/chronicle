package store

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// CHRN-52. The audit is the test that fails if someone grants chronicle_tier1
// what the tier boundary forbids — and, in the same file, the test that proves
// the detector detects. A guard that has never seen a violation is a guard
// whose failure mode is unknown, so every rule below is exercised in both
// directions: clean on the real schema, and exactly one finding when widened.
//
// The widenings run as `chronicle` inside a transaction that is always rolled
// back, and the audit runs ON THAT TRANSACTION (auditTier1 over the querier),
// which is the only way it can see an uncommitted GRANT. Handed the pool it
// would acquire another connection, see nothing, and pass for the wrong reason.

func TestTier1AuditIsCleanOnAFreshSchema(t *testing.T) {
	s, ctx := newTestStore(t)

	a, err := AuditTier1Role(ctx, s.Pool())
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	for _, f := range a.Findings {
		t.Error(f)
	}
	if !a.Clean() {
		t.Fatal("a freshly migrated schema is not clean; the grants no longer say what was decided")
	}
	// The counts are what make a green audit visibly not vacuous. tier2 and
	// public are always walked (schema_migrations lives in public), every
	// tier-2 table and both number sequences are relations, and the guard
	// trigger functions are functions.
	if a.Database == "" {
		t.Error("the audit did not record which database it examined")
	}
	if a.Schemas < 2 {
		t.Errorf("schemas walked = %d, want at least tier2 and public", a.Schemas)
	}
	if a.Relations < 12 {
		t.Errorf("relations examined = %d, want at least the tier-2 tables and sequences", a.Relations)
	}
	if a.Functions < 10 {
		t.Errorf("functions examined = %d, want at least the guard triggers", a.Functions)
	}
}

// One widening, one finding — for every rule in the allow-list.
func TestTier1AuditDetectsEachWidening(t *testing.T) {
	s, ctx := newTestStore(t)
	var db string
	if err := s.Pool().QueryRow(ctx, `SELECT current_database()`).Scan(&db); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name, sql string
		object    string // substring of Finding.Object
		privilege string // prefix of Finding.Privilege
	}{
		{"table-level INSERT", `GRANT INSERT ON tier2.memos TO chronicle_tier1`,
			"table tier2.memos", "INSERT"},
		// The column probe alone. has_table_privilege stays false for this
		// one, which is why every earlier isolation test passed over it.
		{"column-level UPDATE", `GRANT UPDATE (state) ON tier2.memos TO chronicle_tier1`,
			"table tier2.memos", "UPDATE (column-level)"},
		{"SELECT beyond the two readable tables", `GRANT SELECT ON tier2.users TO chronicle_tier1`,
			"table tier2.users", "SELECT"},
		{"a sequence write", `GRANT UPDATE ON SEQUENCE tier2.note_number_seq TO chronicle_tier1`,
			"sequence tier2.note_number_seq", "UPDATE"},
		{"CREATE on schema tier2", `GRANT CREATE ON SCHEMA tier2 TO chronicle_tier1`,
			"schema tier2", "CREATE"},
		{"USAGE on another schema", `GRANT USAGE ON SCHEMA public TO chronicle_tier1`,
			"schema public", "USAGE"},
		// The ensure_db trap (SERV-169): invisible to a table probe and to a
		// schema dump alike.
		{"CREATE on the database", `GRANT CREATE ON DATABASE ` + ident(db) + ` TO chronicle_tier1`,
			"database " + db, "CREATE"},
		{"default privileges to the role",
			`ALTER DEFAULT PRIVILEGES IN SCHEMA tier2 GRANT SELECT ON TABLES TO chronicle_tier1`,
			"default privileges in schema tier2", "SELECT ON TABLES TO chronicle_tier1"},
		{"default privileges to PUBLIC, which the role inherits",
			`ALTER DEFAULT PRIVILEGES IN SCHEMA tier2 GRANT SELECT ON TABLES TO PUBLIC`,
			"default privileges in schema tier2", "SELECT ON TABLES TO PUBLIC"},
		{"a SECURITY DEFINER function",
			`CREATE FUNCTION tier2.probe() RETURNS int LANGUAGE sql SECURITY DEFINER AS 'SELECT 1'`,
			"function tier2.probe()", "SECURITY DEFINER"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tx := begin(t, ctx, s)
			if _, err := tx.Exec(ctx, tc.sql); err != nil {
				t.Fatalf("widen: %v", err)
			}
			a, err := auditTier1(ctx, tx)
			if err != nil {
				t.Fatalf("audit: %v", err)
			}
			if len(a.Findings) != 1 {
				for _, f := range a.Findings {
					t.Log(f)
				}
				t.Fatalf("got %d findings, want exactly one for %q", len(a.Findings), tc.sql)
			}
			f := a.Findings[0]
			if !strings.Contains(f.Object, tc.object) || !strings.HasPrefix(f.Privilege, tc.privilege) {
				t.Fatalf("finding names %q / %q, want %q / %q", f.Object, f.Privilege, tc.object, tc.privilege)
			}
			if f.Rule == "" || f.Remedy == "" {
				t.Fatalf("finding carries no rule or no remedy: %+v", f)
			}
			if err := tx.Rollback(ctx); err != nil {
				t.Fatalf("rollback: %v", err)
			}
			// And the widening is gone with the transaction.
			after, err := AuditTier1Role(ctx, s.Pool())
			if err != nil {
				t.Fatalf("audit after rollback: %v", err)
			}
			if !after.Clean() {
				t.Fatalf("still %d findings after rollback; the widening leaked", len(after.Findings))
			}
		})
	}
}

// The audit covers relations and schemas that do not exist yet — without
// flagging their mere existence. This is the claim the name-based tests could
// never make.
func TestTier1AuditCoversWhatDoesNotExistYet(t *testing.T) {
	s, ctx := newTestStore(t)
	base, err := AuditTier1Role(ctx, s.Pool())
	if err != nil {
		t.Fatal(err)
	}

	t.Run("a new tier-2 table", func(t *testing.T) {
		tx := begin(t, ctx, s)
		mustExec(t, ctx, tx, `CREATE TABLE tier2.probe_future (id int)`)
		a, err := auditTier1(ctx, tx)
		if err != nil {
			t.Fatal(err)
		}
		if !a.Clean() {
			t.Fatalf("an ungranted new table is a finding: %v", a.Findings)
		}
		if a.Relations != base.Relations+1 {
			t.Fatalf("relations examined = %d, want %d: the new table was not walked", a.Relations, base.Relations+1)
		}
		mustExec(t, ctx, tx, `GRANT INSERT ON tier2.probe_future TO chronicle_tier1`)
		a, err = auditTier1(ctx, tx)
		if err != nil {
			t.Fatal(err)
		}
		if len(a.Findings) != 1 || a.Findings[0].Object != "table tier2.probe_future" || a.Findings[0].Privilege != "INSERT" {
			t.Fatalf("got %v, want one INSERT finding on tier2.probe_future", a.Findings)
		}
	})

	t.Run("a new schema", func(t *testing.T) {
		tx := begin(t, ctx, s)
		mustExec(t, ctx, tx, `CREATE SCHEMA probe_schema`)
		mustExec(t, ctx, tx, `CREATE TABLE probe_schema.t (id int)`)
		a, err := auditTier1(ctx, tx)
		if err != nil {
			t.Fatal(err)
		}
		if !a.Clean() {
			t.Fatalf("an ungranted new schema is a finding: %v", a.Findings)
		}
		if a.Schemas != base.Schemas+1 {
			t.Fatalf("schemas walked = %d, want %d: the new schema was not walked", a.Schemas, base.Schemas+1)
		}
		mustExec(t, ctx, tx, `GRANT SELECT ON probe_schema.t TO chronicle_tier1`)
		a, err = auditTier1(ctx, tx)
		if err != nil {
			t.Fatal(err)
		}
		if len(a.Findings) != 1 || a.Findings[0].Object != "table probe_schema.t" || a.Findings[0].Privilege != "SELECT" {
			t.Fatalf("got %v, want one SELECT finding on probe_schema.t", a.Findings)
		}
	})
}

// An audit that examined nothing is not a clean audit.
func TestTier1AuditRefusesToPassOnNothing(t *testing.T) {
	s, ctx := newTestStore(t)
	tx := begin(t, ctx, s)
	mustExec(t, ctx, tx, `DROP SCHEMA tier2 CASCADE`)
	mustExec(t, ctx, tx, `DROP SCHEMA public CASCADE`)
	a, err := auditTier1(ctx, tx)
	if err != nil {
		t.Fatal(err)
	}
	if a.Relations != 0 {
		t.Fatalf("relations examined = %d after dropping both schemas", a.Relations)
	}
	if len(a.Findings) != 1 || a.Findings[0].Privilege != "NOTHING EXAMINED" {
		t.Fatalf("got %v, want the single coverage finding", a.Findings)
	}
}

// The role's own attributes, pinned independently of the audit's rule so a
// change to either is visible on its own.
func TestTier1RoleIsAPlainLoginRole(t *testing.T) {
	s, ctx := newTestStore(t)
	var super, createdb, createrole, bypassrls, replication, login bool
	var memberships, owned int
	err := s.Pool().QueryRow(ctx, `
		SELECT r.rolsuper, r.rolcreatedb, r.rolcreaterole, r.rolbypassrls, r.rolreplication, r.rolcanlogin,
		       (SELECT count(*) FROM pg_auth_members m WHERE m.member = r.oid),
		       (SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		         WHERE c.relowner = r.oid AND n.nspname <> 'tier1')
		FROM pg_roles r WHERE r.rolname = $1`, Tier1RoleName).
		Scan(&super, &createdb, &createrole, &bypassrls, &replication, &login, &memberships, &owned)
	if err != nil {
		t.Fatal(err)
	}
	if super || createdb || createrole || bypassrls || replication {
		t.Errorf("chronicle_tier1 holds a role attribute it must not: super=%v createdb=%v createrole=%v bypassrls=%v replication=%v",
			super, createdb, createrole, bypassrls, replication)
	}
	if !login {
		t.Error("chronicle_tier1 cannot LOGIN; nothing can connect as it")
	}
	if memberships != 0 {
		t.Errorf("chronicle_tier1 is a member of %d role(s); membership is every privilege at once", memberships)
	}
	if owned != 0 {
		t.Errorf("chronicle_tier1 owns %d relation(s) outside tier1; an owner bypasses grants", owned)
	}
}

func begin(t *testing.T, ctx context.Context, s *Store) pgx.Tx {
	t.Helper()
	tx, err := s.Pool().Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(ctx) })
	return tx
}

func mustExec(t *testing.T, ctx context.Context, q querier, sql string) {
	t.Helper()
	if _, err := q.Exec(ctx, sql); err != nil {
		t.Fatalf("%s: %v", sql, err)
	}
}
