package store

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Tier1RoleName is the regeneration role: the one derived writers connect as,
// and the one this file audits.
const Tier1RoleName = "chronicle_tier1"

// THE AUDIT (CHRN-52). The tier boundary is a Postgres grant, and a grant is
// only a boundary while it says what was decided. Every isolation test before
// this one checked NAMED objects with TABLE-LEVEL probes, so a tier-2 table
// nobody had written yet, a column-level grant, a write on a sequence, a
// SECURITY DEFINER function, a default privilege on the schema, or CREATE on
// the database itself all passed unseen. This walks the catalogue instead and
// holds the role to an ALLOW-LIST: everything it may hold outside tier1 is
// written once, below, as data, and anything else it holds is a Finding.
//
// It runs AS ANY ROLE. Every probe names chronicle_tier1 explicitly, so the
// main pool can run it at boot whether or not a tier-1 pool exists, and the
// test can run it inside the very transaction it has just widened.
//
// A legitimate widening — a future ticket that derives rows from a third
// tier-2 table — edits two places on purpose: its migration and the allow-list
// here. That is one more place than a GRANT alone, and it is the point: the
// widening becomes a decision in a code review rather than a side effect.

// The allow-list, as the rules a Finding cites. Each is a sentence a reader
// can check against the migrations without reading this file's queries.
const (
	ruleRole     = "the role is a plain LOGIN role: no superuser, no CREATEDB/CREATEROLE/REPLICATION/BYPASSRLS, member of nothing"
	ruleDatabase = "on the database the role holds CONNECT and nothing else"
	ruleSchema   = "on schema tier2 the role holds USAGE; on every other schema outside tier1, nothing"
	ruleTables   = "outside tier1 the role holds table-level SELECT on tier2.memos and tier2.transcripts (0007) and no other privilege on any relation"
	ruleDefaults = "no default privilege outside tier1 names the role or PUBLIC"
	ruleFuncs    = "no function in any schema is SECURITY DEFINER"
	ruleOwner    = "the role owns nothing outside tier1"
	ruleCoverage = "the audit examined at least one relation; an audit of nothing proves nothing"
)

// tier1Readable is the whole of R4's grant: the two tier-2 tables the role may
// SELECT from, table-level. Adding to this list is the deliberate half of a
// widening; the other half is the migration that grants it.
var tier1Readable = map[string]bool{
	"tier2.memos":       true,
	"tier2.transcripts": true,
}

// Finding is one privilege chronicle_tier1 holds that the allow-list does not
// grant it, keyed on (Object, Privilege) so one GRANT yields one Finding.
type Finding struct {
	Object    string // "table tier2.memos", "schema public", "database chronicle", ...
	Privilege string // "INSERT", "UPDATE (column-level)", "SECURITY DEFINER", ...
	Rule      string // the allow-list line it violates
	Remedy    string // the statement that clears it
}

func (f Finding) String() string {
	return fmt.Sprintf("%s holds %s on %s; %s; remedy: %s",
		Tier1RoleName, f.Privilege, f.Object, f.Rule, f.Remedy)
}

// Tier1Audit is one run of the audit: what it examined and what it found. The
// counts are part of the result because a green audit is only evidence if it
// visibly examined something.
type Tier1Audit struct {
	Database  string
	Schemas   int // schemas walked, tier1 excluded
	Relations int // tables, views, foreign tables and sequences examined
	Functions int
	Findings  []Finding
}

// Clean reports whether the role holds exactly what the allow-list permits.
func (a Tier1Audit) Clean() bool { return len(a.Findings) == 0 }

// AuditTier1Role audits chronicle_tier1 on the database pool reaches. This is
// the entry `serve` runs at boot and `chronicle tier1-audit` runs for an
// operator; the test uses auditTier1 directly so it can pass a transaction.
func AuditTier1Role(ctx context.Context, pool *pgxpool.Pool) (Tier1Audit, error) {
	return auditTier1(ctx, pool)
}

// auditTier1 takes the package's querier rather than a pool for one reason
// that is load-bearing: a pool acquires a fresh connection per call, so an
// audit handed the pool during the mutation self-test would never see the
// uncommitted GRANT it was meant to catch, and the detector would fail in
// exactly the direction the self-test exists to rule out.
func auditTier1(ctx context.Context, q querier) (Tier1Audit, error) {
	var a Tier1Audit
	add := func(object, privilege, rule, remedy string) {
		a.Findings = append(a.Findings, Finding{Object: object, Privilege: privilege, Rule: rule, Remedy: remedy})
	}

	// 1. The role itself. An attribute or a membership is a privilege on
	// everything at once, which no table probe would report as its own.
	var super, createdb, createrole, bypassrls, replication bool
	err := q.QueryRow(ctx, `
		SELECT rolsuper, rolcreatedb, rolcreaterole, rolbypassrls, rolreplication
		FROM pg_roles WHERE rolname = $1`, Tier1RoleName).
		Scan(&super, &createdb, &createrole, &bypassrls, &replication)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, fmt.Errorf("store: role %s does not exist, so there is nothing to audit", Tier1RoleName)
	}
	if err != nil {
		return a, fmt.Errorf("store: audit role: %w", err)
	}
	for _, attr := range []struct {
		held bool
		name string
	}{
		{super, "SUPERUSER"}, {createdb, "CREATEDB"}, {createrole, "CREATEROLE"},
		{bypassrls, "BYPASSRLS"}, {replication, "REPLICATION"},
	} {
		if attr.held {
			add("role "+Tier1RoleName, attr.name, ruleRole,
				fmt.Sprintf("ALTER ROLE %s NO%s", Tier1RoleName, attr.name))
		}
	}
	members, err := collect(ctx, q, `
		SELECT r.rolname
		FROM pg_auth_members m JOIN pg_roles r ON r.oid = m.roleid
		WHERE m.member = (SELECT oid FROM pg_roles WHERE rolname = $1)
		ORDER BY 1`, Tier1RoleName)
	if err != nil {
		return a, fmt.Errorf("store: audit memberships: %w", err)
	}
	for _, of := range members {
		add("role "+Tier1RoleName, "MEMBER OF "+of, ruleRole,
			fmt.Sprintf("REVOKE %s FROM %s", ident(of), Tier1RoleName))
	}

	// 2. The database. CREATE here is the ensure_db trap SERV-169 named: a
	// privilege no table probe can see and no schema dump ever shows.
	var canCreate, canTemp bool
	if err := q.QueryRow(ctx, `
		SELECT current_database(),
		       has_database_privilege($1, current_database(), 'CREATE'),
		       has_database_privilege($1, current_database(), 'TEMP')`, Tier1RoleName).
		Scan(&a.Database, &canCreate, &canTemp); err != nil {
		return a, fmt.Errorf("store: audit database: %w", err)
	}
	for _, p := range []struct {
		held bool
		name string
	}{{canCreate, "CREATE"}, {canTemp, "TEMP"}} {
		if p.held {
			add("database "+a.Database, p.name, ruleDatabase,
				fmt.Sprintf("REVOKE %s ON DATABASE %s FROM %s", p.name, ident(a.Database), Tier1RoleName))
		}
	}

	// 3. Schemas. Every non-system schema other than tier1 — not only tier2,
	// because a migration that put authored rows in public or a third schema
	// would otherwise be invisible however it was granted. The predicate is
	// the standard idiom: it excludes pg_catalog, pg_toast, every pg_temp_N
	// and pg_toast_temp_N, and information_schema, and nothing else.
	rows, err := q.Query(ctx, `
		SELECT n.nspname,
		       has_schema_privilege($1, n.oid, 'USAGE'),
		       has_schema_privilege($1, n.oid, 'CREATE')
		FROM pg_namespace n
		WHERE n.nspname NOT LIKE 'pg\_%' AND n.nspname <> 'information_schema' AND n.nspname <> 'tier1'
		ORDER BY 1`, Tier1RoleName)
	if err != nil {
		return a, fmt.Errorf("store: audit schemas: %w", err)
	}
	for rows.Next() {
		var name string
		var usage, create bool
		if err := rows.Scan(&name, &usage, &create); err != nil {
			rows.Close()
			return a, fmt.Errorf("store: audit schemas: %w", err)
		}
		a.Schemas++
		if usage && name != "tier2" {
			add("schema "+name, "USAGE", ruleSchema,
				fmt.Sprintf("REVOKE USAGE ON SCHEMA %s FROM %s", ident(name), Tier1RoleName))
		}
		if create {
			add("schema "+name, "CREATE", ruleSchema,
				fmt.Sprintf("REVOKE CREATE ON SCHEMA %s FROM %s", ident(name), Tier1RoleName))
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return a, fmt.Errorf("store: audit schemas: %w", err)
	}

	// 4. Relations other than sequences, in every schema above. Table-level
	// and column-level are probed together and REPORTED ONCE: a table-level
	// grant satisfies has_any_column_privilege too (Postgres semantics), so the
	// column probe speaks only when the table-level one is silent. That is what
	// makes `GRANT UPDATE (state)` a finding of its own and `GRANT INSERT` one
	// finding rather than two.
	rows, err = q.Query(ctx, `
		SELECT n.nspname, c.relname, c.relkind::text,
		       c.relowner = (SELECT oid FROM pg_roles WHERE rolname = $1),
		       has_table_privilege($1, c.oid, 'SELECT'),
		       has_table_privilege($1, c.oid, 'INSERT'),
		       has_table_privilege($1, c.oid, 'UPDATE'),
		       has_table_privilege($1, c.oid, 'DELETE'),
		       has_table_privilege($1, c.oid, 'TRUNCATE'),
		       has_table_privilege($1, c.oid, 'REFERENCES'),
		       has_table_privilege($1, c.oid, 'TRIGGER'),
		       has_any_column_privilege($1, c.oid, 'SELECT'),
		       has_any_column_privilege($1, c.oid, 'INSERT'),
		       has_any_column_privilege($1, c.oid, 'UPDATE'),
		       has_any_column_privilege($1, c.oid, 'REFERENCES')
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname NOT LIKE 'pg\_%' AND n.nspname <> 'information_schema' AND n.nspname <> 'tier1'
		  AND c.relkind IN ('r', 'p', 'v', 'm', 'f')
		ORDER BY 1, 2`, Tier1RoleName)
	if err != nil {
		return a, fmt.Errorf("store: audit relations: %w", err)
	}
	for rows.Next() {
		var schema, name, kind string
		var owned bool
		var tbl [7]bool
		var col [4]bool
		if err := rows.Scan(&schema, &name, &kind, &owned,
			&tbl[0], &tbl[1], &tbl[2], &tbl[3], &tbl[4], &tbl[5], &tbl[6],
			&col[0], &col[1], &col[2], &col[3]); err != nil {
			rows.Close()
			return a, fmt.Errorf("store: audit relations: %w", err)
		}
		a.Relations++
		qualified := schema + "." + name
		object := relkindLabel(kind) + " " + qualified
		if owned {
			add(object, "OWNER", ruleOwner,
				fmt.Sprintf("ALTER TABLE %s OWNER TO chronicle", qualifiedIdent(schema, name)))
		}
		tablePrivs := [7]string{"SELECT", "INSERT", "UPDATE", "DELETE", "TRUNCATE", "REFERENCES", "TRIGGER"}
		for i, p := range tablePrivs {
			if !tbl[i] {
				continue
			}
			if p == "SELECT" && tier1Readable[qualified] {
				continue
			}
			add(object, p, ruleTables,
				fmt.Sprintf("REVOKE %s ON %s FROM %s (and FROM PUBLIC, if it was granted that way)",
					p, qualifiedIdent(schema, name), Tier1RoleName))
		}
		colPrivs := [4]string{"SELECT", "INSERT", "UPDATE", "REFERENCES"}
		colTable := [4]int{0, 1, 2, 5} // index of the matching table-level probe
		for i, p := range colPrivs {
			if !col[i] || tbl[colTable[i]] {
				continue
			}
			add(object, p+" (column-level)", ruleTables,
				fmt.Sprintf("REVOKE %s (<the granted columns>) ON %s FROM %s; information_schema.column_privileges lists them",
					p, qualifiedIdent(schema, name), Tier1RoleName))
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return a, fmt.Errorf("store: audit relations: %w", err)
	}

	// 5. Sequences. UPDATE on a sequence is a write, and USAGE is nextval.
	rows, err = q.Query(ctx, `
		SELECT n.nspname, c.relname,
		       c.relowner = (SELECT oid FROM pg_roles WHERE rolname = $1),
		       has_sequence_privilege($1, c.oid, 'USAGE'),
		       has_sequence_privilege($1, c.oid, 'SELECT'),
		       has_sequence_privilege($1, c.oid, 'UPDATE')
		FROM pg_class c JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname NOT LIKE 'pg\_%' AND n.nspname <> 'information_schema' AND n.nspname <> 'tier1'
		  AND c.relkind = 'S'
		ORDER BY 1, 2`, Tier1RoleName)
	if err != nil {
		return a, fmt.Errorf("store: audit sequences: %w", err)
	}
	for rows.Next() {
		var schema, name string
		var owned, usage, sel, upd bool
		if err := rows.Scan(&schema, &name, &owned, &usage, &sel, &upd); err != nil {
			rows.Close()
			return a, fmt.Errorf("store: audit sequences: %w", err)
		}
		a.Relations++
		object := "sequence " + schema + "." + name
		if owned {
			add(object, "OWNER", ruleOwner,
				fmt.Sprintf("ALTER SEQUENCE %s OWNER TO chronicle", qualifiedIdent(schema, name)))
		}
		for _, p := range []struct {
			held bool
			name string
		}{{usage, "USAGE"}, {sel, "SELECT"}, {upd, "UPDATE"}} {
			if p.held {
				add(object, p.name, ruleTables,
					fmt.Sprintf("REVOKE %s ON SEQUENCE %s FROM %s", p.name, qualifiedIdent(schema, name), Tier1RoleName))
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return a, fmt.Errorf("store: audit sequences: %w", err)
	}

	// 6. Default privileges: the forward-looking half. The probes above honour
	// a PUBLIC grant on relations that exist; this catches the ALTER DEFAULT
	// PRIVILEGES that would reach every relation created afterwards — whether
	// it names the role or PUBLIC (aclitem grantee 0), and whether it is
	// scoped to a schema or to none (defaclnamespace 0 means every schema).
	rows, err = q.Query(ctx, `
		SELECT coalesce(n.nspname, ''), d.defaclobjtype::text, o.rolname, a.grantee, a.privilege_type
		FROM pg_default_acl d
		LEFT JOIN pg_namespace n ON n.oid = d.defaclnamespace
		JOIN pg_roles o ON o.oid = d.defaclrole
		CROSS JOIN LATERAL aclexplode(d.defaclacl) a
		WHERE coalesce(n.nspname, '') <> 'tier1'
		  AND (a.grantee = 0 OR a.grantee = (SELECT oid FROM pg_roles WHERE rolname = $1))
		ORDER BY 1, 2, 3, 5`, Tier1RoleName)
	if err != nil {
		return a, fmt.Errorf("store: audit default privileges: %w", err)
	}
	for rows.Next() {
		var schema, objtype, forRole, priv string
		var grantee uint32
		if err := rows.Scan(&schema, &objtype, &forRole, &grantee, &priv); err != nil {
			rows.Close()
			return a, fmt.Errorf("store: audit default privileges: %w", err)
		}
		to := "PUBLIC"
		if grantee != 0 {
			to = Tier1RoleName
		}
		on := defaclLabel(objtype)
		scope, in := "every schema", ""
		if schema != "" {
			scope, in = "schema "+schema, " IN SCHEMA "+ident(schema)
		}
		add(fmt.Sprintf("default privileges in %s (for role %s)", scope, forRole),
			fmt.Sprintf("%s ON %s TO %s", priv, on, to), ruleDefaults,
			fmt.Sprintf("ALTER DEFAULT PRIVILEGES FOR ROLE %s%s REVOKE %s ON %s FROM %s",
				ident(forRole), in, priv, on, to))
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return a, fmt.Errorf("store: audit default privileges: %w", err)
	}

	// 7. Functions, tier1 included. A SECURITY DEFINER function owned by
	// chronicle is a write path with a different name: the role executing it
	// writes as chronicle, and EXECUTE defaults to PUBLIC.
	rows, err = q.Query(ctx, `
		SELECT n.nspname, p.proname, pg_get_function_identity_arguments(p.oid), p.prosecdef,
		       p.proowner = (SELECT oid FROM pg_roles WHERE rolname = $1)
		FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		WHERE n.nspname NOT LIKE 'pg\_%' AND n.nspname <> 'information_schema'
		ORDER BY 1, 2, 3`, Tier1RoleName)
	if err != nil {
		return a, fmt.Errorf("store: audit functions: %w", err)
	}
	for rows.Next() {
		var schema, name, args string
		var secdef, owned bool
		if err := rows.Scan(&schema, &name, &args, &secdef, &owned); err != nil {
			rows.Close()
			return a, fmt.Errorf("store: audit functions: %w", err)
		}
		a.Functions++
		sig := fmt.Sprintf("%s.%s(%s)", schema, name, args)
		if secdef {
			add("function "+sig, "SECURITY DEFINER", ruleFuncs,
				fmt.Sprintf("ALTER FUNCTION %s(%s) SECURITY INVOKER", qualifiedIdent(schema, name), args))
		}
		if owned && schema != "tier1" {
			add("function "+sig, "OWNER", ruleOwner,
				fmt.Sprintf("ALTER FUNCTION %s(%s) OWNER TO chronicle", qualifiedIdent(schema, name), args))
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return a, fmt.Errorf("store: audit functions: %w", err)
	}

	// 8. Coverage. Pointed at an empty database this would otherwise report a
	// clean bill of health for a boundary it never looked at.
	if a.Relations == 0 {
		add("the audit itself", "NOTHING EXAMINED", ruleCoverage,
			"run it against Chronicle's own database, after the migrations")
	}

	sort.Slice(a.Findings, func(i, j int) bool {
		if a.Findings[i].Object != a.Findings[j].Object {
			return a.Findings[i].Object < a.Findings[j].Object
		}
		return a.Findings[i].Privilege < a.Findings[j].Privilege
	})
	return a, nil
}

func collect(ctx context.Context, q querier, sql string, args ...any) ([]string, error) {
	rows, err := q.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func relkindLabel(kind string) string {
	switch kind {
	case "r":
		return "table"
	case "p":
		return "partitioned table"
	case "v":
		return "view"
	case "m":
		return "materialized view"
	case "f":
		return "foreign table"
	}
	return "relation"
}

func defaclLabel(objtype string) string {
	switch objtype {
	case "r":
		return "TABLES"
	case "S":
		return "SEQUENCES"
	case "f":
		return "FUNCTIONS"
	case "T":
		return "TYPES"
	case "n":
		return "SCHEMAS"
	}
	return strings.ToUpper(objtype)
}

var plainIdent = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)

// ident quotes an identifier only when it needs quoting, so a remedy reads as
// the statement an operator would type.
func ident(name string) string {
	if plainIdent.MatchString(name) {
		return name
	}
	return pgx.Identifier{name}.Sanitize()
}

func qualifiedIdent(schema, name string) string {
	return ident(schema) + "." + ident(name)
}
