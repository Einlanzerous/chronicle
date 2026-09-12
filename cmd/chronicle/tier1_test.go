package main

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Einlanzerous/chronicle/internal/config"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// CHRN-52 ruling 1: serve refuses to stand behind a tier-1 pool that is not
// chronicle_tier1, in all three ways it can fail to be — and never falls back
// to the main role in any of them. Ruling 2: serve refuses when the audit
// finds the role holding more than the allow-list.

func quietLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewTextHandler(&buf, nil)), &buf
}

func TestOpenTier1PoolRefusesTheFallback(t *testing.T) {
	logger, _ := quietLogger()
	// What config.Load produces when CHRONICLE_TIER1_DATABASE_URL is unset:
	// the tier-1 DSN equals the main one. Keyword form with no password —
	// nothing here connects, and a URI with a placeholder password reads as a
	// credential to a secret scanner.
	cfg := config.Config{
		DatabaseURL:      "host=127.0.0.1 port=1 user=chronicle dbname=chronicle",
		Tier1DatabaseURL: "host=127.0.0.1 port=1 user=chronicle dbname=chronicle",
	}
	pool, tier1, err := openTier1Pool(context.Background(), cfg, logger, time.Second)
	if err == nil {
		pool.Close()
		t.Fatal("an unset tier-1 DSN was accepted; the fallback survived")
	}
	if pool != nil || tier1 != nil {
		t.Fatal("a refusal handed back a pool")
	}
	if !strings.Contains(err.Error(), "CHRONICLE_TIER1_DATABASE_URL") {
		t.Fatalf("the refusal does not name the variable: %v", err)
	}
}

// Set, right shape, unreachable: stays an error after the budget, and does
// not quietly open the main pool instead.
func TestOpenTier1PoolRefusesAnUnreachableDSN(t *testing.T) {
	logger, _ := quietLogger()
	// Port 1 answers nothing; connect_timeout keeps each attempt short so the
	// budget, not the TCP stack, decides when the refusal comes.
	cfg := config.Config{
		DatabaseURL:      "host=127.0.0.1 port=1 user=chronicle dbname=chronicle",
		Tier1DatabaseURL: "host=127.0.0.1 port=1 user=chronicle_tier1 dbname=chronicle connect_timeout=1",
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pool, _, err := openTier1Pool(ctx, cfg, logger, 500*time.Millisecond)
	if err == nil {
		pool.Close()
		t.Fatal("an unreachable tier-1 DSN was accepted")
	}
	if !strings.Contains(err.Error(), "CHRONICLE_TIER1_DATABASE_URL") || !strings.Contains(err.Error(), "not falling back") {
		t.Fatalf("the refusal does not say what it refused or that it did not fall back: %v", err)
	}
}

// Set, reachable, and the wrong role: a DSN that differs from the main one in
// text but connects as `chronicle`. Before CHRN-52 this was a warning that an
// operator could silence by setting any value at all.
func TestOpenTier1PoolRefusesTheWrongRole(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("CHRONICLE_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("CHRONICLE_TEST_DATABASE_URL not set; skipping database test")
	}
	logger, _ := quietLogger()
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	cfg := config.Config{DatabaseURL: dsn, Tier1DatabaseURL: dsn + sep + "application_name=tier1-wrong-role"}
	if !cfg.Tier1IsSeparate() {
		t.Fatal("test setup: the two DSNs must differ textually for this to be the wrong-role branch")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, _, err := openTier1Pool(ctx, cfg, logger, 5*time.Second)
	if err == nil {
		pool.Close()
		t.Fatal("a tier-1 DSN connecting as the main role was accepted")
	}
	if !strings.Contains(err.Error(), `want "chronicle_tier1"`) {
		t.Fatalf("the refusal does not name the role it wanted: %v", err)
	}
}

// And the one shape that is accepted: the role itself.
func TestOpenTier1PoolAcceptsTheRole(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("CHRONICLE_TEST_DATABASE_URL"))
	t1 := strings.TrimSpace(os.Getenv("CHRONICLE_TEST_TIER1_DATABASE_URL"))
	if dsn == "" || t1 == "" {
		t.Skip("CHRONICLE_TEST_DATABASE_URL or CHRONICLE_TEST_TIER1_DATABASE_URL not set; skipping")
	}
	logger, out := quietLogger()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, tier1, err := openTier1Pool(ctx, config.Config{DatabaseURL: dsn, Tier1DatabaseURL: t1}, logger, 5*time.Second)
	if err != nil {
		t.Fatalf("the tier-1 role was refused: %v", err)
	}
	defer pool.Close()
	role, err := tier1.Role(ctx)
	if err != nil || role != store.Tier1RoleName {
		t.Fatalf("role = %q, %v", role, err)
	}
	if !strings.Contains(out.String(), "derived writers are isolated") {
		t.Fatalf("boot did not announce the isolation; log was:\n%s", out.String())
	}
}

// Ruling 2, on the decision alone: one error line per finding, then a refusal
// naming the count. The findings themselves are the store's tests to produce;
// this proves serve does not shrug at them.
func TestRefuseIfWidenedNamesEveryFinding(t *testing.T) {
	logger, out := quietLogger()
	audit := store.Tier1Audit{
		Database: "chronicle", Schemas: 2, Relations: 20, Functions: 17,
		Findings: []store.Finding{
			{Object: "database chronicle", Privilege: "CREATE", Rule: "r1", Remedy: "REVOKE CREATE ON DATABASE chronicle FROM chronicle_tier1"},
			{Object: "table tier2.memos", Privilege: "UPDATE (column-level)", Rule: "r2", Remedy: "REVOKE UPDATE (…) ON tier2.memos FROM chronicle_tier1"},
		},
	}
	err := refuseIfWidened(audit, logger)
	if err == nil {
		t.Fatal("a widened role was allowed to serve")
	}
	if !strings.Contains(err.Error(), "2 privilege(s)") || !strings.Contains(err.Error(), "refusing to serve") {
		t.Fatalf("refusal does not say how many or that it refused: %v", err)
	}
	log := out.String()
	for _, want := range []string{"database chronicle", "CREATE", "tier2.memos", "UPDATE (column-level)", "REVOKE"} {
		if !strings.Contains(log, want) {
			t.Errorf("log does not carry %q:\n%s", want, log)
		}
	}
	if strings.Count(log, "level=ERROR") != 2 {
		t.Errorf("want one error line per finding, got:\n%s", log)
	}

	// A clean audit is announced with its counts and refuses nothing.
	logger, out = quietLogger()
	if err := refuseIfWidened(store.Tier1Audit{Database: "chronicle", Schemas: 2, Relations: 20, Functions: 17}, logger); err != nil {
		t.Fatalf("a clean audit was refused: %v", err)
	}
	if !strings.Contains(out.String(), "relations=20") {
		t.Fatalf("a clean audit did not report its counts:\n%s", out.String())
	}
}

// `chronicle tier1-audit` against the real test database: clean, exit 0.
func TestTier1AuditSubcommandIsCleanOnTheTestDatabase(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("CHRONICLE_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("CHRONICLE_TEST_DATABASE_URL not set; skipping database test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	pool, err := store.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if err := store.MigrateDown(ctx, pool, 0); err != nil {
		t.Fatal(err)
	}
	if err := store.Migrate(ctx, pool); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CHRONICLE_DATABASE_URL", dsn)
	if err := run([]string{"tier1-audit"}); err != nil {
		t.Fatalf("tier1-audit on a fresh schema: %v", err)
	}
}
