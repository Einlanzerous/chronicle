// Command chronicle is Chronicle's single static binary.
//
// CHRN-14 ships the migrate and version subcommands, CHRN-15 adds serve, and
// CHRN-71 adds mint-invite.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/Einlanzerous/chronicle/internal/api"
	"github.com/Einlanzerous/chronicle/internal/config"
	"github.com/Einlanzerous/chronicle/internal/invite"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// version is stamped at build time with -ldflags "-X main.version=...".
// It defaults to EMPTY, not to "dev": an -X flag passed with an empty value
// overwrites whatever default is written here, so the fallback has to live in
// code rather than in the variable. buildVersion() is the only reader.
var version = ""

// commit is the full 40-char git SHA, stamped the same way. Reported verbatim
// by /healthz — the SWY-192 delivery-reconciler contract reads it to record
// what is actually running.
var commit = ""

// buildVersion reports the stamped build identity, or "dev" for a local build.
func buildVersion() string {
	if version == "" {
		return "dev"
	}
	return version
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "chronicle: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return fmt.Errorf("no subcommand given")
	}

	switch args[0] {
	case "version":
		fmt.Println(buildVersion())
		if commit != "" {
			fmt.Println(commit)
		}
		return nil
	case "serve":
		return runServe(args[1:])
	case "migrate":
		return runMigrate(args[1:])
	case "mint-invite":
		return runMintInvite(args[1:])
	case "-h", "--help", "help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown subcommand %q", args[0])
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `chronicle — voice-note ingestion into a notes and discussion wiki

usage:
  chronicle serve                      run the HTTP service
  chronicle migrate [up|down] [-n N]   apply or roll back migrations
  chronicle mint-invite [--email E]    issue a one-time sign-in invite
  chronicle version                    print the build version

migrate defaults to "up". "down" without -n rolls everything back.
mint-invite defaults to the owner. The invite is shown once and expires.
`)
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := cfg.ValidateForServe(); err != nil {
		return err
	}
	logger := cfg.Logger(os.Stdout)

	// SIGTERM is what docker stop sends; Ctrl-C sends SIGINT. Both mean the
	// same thing here: stop accepting work, finish what is in hand.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := store.ConnectWithRetry(ctx, cfg.DatabaseURL, 30*time.Second)
	if err != nil {
		return err
	}
	defer pool.Close()

	// Migrations are applied on boot, as the house pattern does — the binary
	// and its schema ship together, so there is no window where they disagree.
	if err := store.Migrate(ctx, pool); err != nil {
		return err
	}
	logger.Info("migrations applied")

	st := store.New(pool)

	// The owner row is seeded by migration 0002 with a placeholder identity;
	// this is where CHRONICLE_OWNER_EMAIL / _NAME actually land, and where the
	// first human gets a way in.
	if err := bootstrapOwner(ctx, st, cfg, logger); err != nil {
		return err
	}

	deps := api.Deps{
		DB:            st,
		Accounts:      st,
		Logger:        logger,
		Version:       buildVersion(),
		Commit:        commit,
		MobileBaseURL: cfg.MobileBaseURL,
	}
	if cfg.SSOEnabled() {
		deps.CFAccess = api.NewCFAccessVerifier(cfg.CFAccessTeamDomain, cfg.CFAccessAUD)
	}
	handler := api.NewRouter(deps)
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	logger.Info("chronicle starting",
		"version", buildVersion(), "addr", cfg.Addr, "log_format", cfg.LogFormat,
		"sso", cfg.SSOEnabled())
	return api.Serve(ctx, srv, cfg.ShutdownGrace, logger)
}

func runMigrate(args []string) error {
	direction := "up"
	if len(args) > 0 && !isFlag(args[0]) {
		direction, args = args[0], args[1:]
	}

	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	n := fs.Int("n", 0, "how many migrations to roll back (down only; 0 = all)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := store.ConnectWithRetry(ctx, cfg.DatabaseURL, 30*time.Second)
	if err != nil {
		return err
	}
	defer pool.Close()

	switch direction {
	case "up":
		if err := store.Migrate(ctx, pool); err != nil {
			return err
		}
		fmt.Println("migrations up: ok")
	case "down":
		if err := store.MigrateDown(ctx, pool, *n); err != nil {
			return err
		}
		fmt.Println("migrations down: ok (" + scope(*n) + ")")
	default:
		return fmt.Errorf("migrate: unknown direction %q (want up or down)", direction)
	}
	return nil
}

func scope(n int) string {
	if n <= 0 {
		return "all"
	}
	return "last " + strconv.Itoa(n)
}

func isFlag(s string) bool { return len(s) > 0 && s[0] == '-' }

// bootstrapOwner reconciles the seeded owner with the configured identity and,
// if nobody can sign in as them yet, mints a first invite and logs it.
//
// The invite goes into a structured log on stdout, which Dozzle reads and
// Datadog could. That is a live credential in a log line, and it is done anyway
// because a container's log is the one channel the first operator always has:
// the alternative assumes they know a CLI exists before they can reach
// anything. It is bounded — single-use, seven days, emitted at warn so it is
// conspicuous, and never re-emitted while an unredeemed one is outstanding.
func bootstrapOwner(ctx context.Context, st *store.Store, cfg config.Config, logger *slog.Logger) error {
	owner, err := st.ReconcileOwner(ctx, cfg.OwnerEmail, cfg.OwnerName)
	if errors.Is(err, store.ErrDuplicateEmail) {
		// A typo should not stop the server booting, but it must not silently
		// do nothing either.
		logger.Warn("owner email belongs to another account; leaving the owner unchanged",
			"configured", cfg.OwnerEmail, "owner", owner.Email)
	} else if err != nil {
		return fmt.Errorf("reconcile owner: %w", err)
	}

	sessions, err := st.CountTokens(ctx, owner.ID, store.TokenSession)
	if err != nil {
		return fmt.Errorf("count owner sessions: %w", err)
	}
	if sessions > 0 {
		return nil
	}

	// If an invite is already outstanding, do not mint a second one on every
	// restart — its plaintext is unrecoverable, so say how to get a fresh one.
	invites, err := st.CountTokens(ctx, owner.ID, store.TokenInvite)
	if err != nil {
		return fmt.Errorf("count owner invites: %w", err)
	}
	if invites > 0 {
		logger.Warn("no device is signed in as the owner and an unredeemed invite is outstanding",
			"owner", owner.Email, "remedy", "chronicle mint-invite")
		return nil
	}

	token, err := st.MintInvite(ctx, owner.ID, "bootstrap")
	if err != nil {
		return fmt.Errorf("mint owner invite: %w", err)
	}
	logger.Warn("first-boot sign-in invite — single use, shown once, not recoverable",
		"owner", owner.Email,
		"invite_token", token,
		"sign_in_url", invite.SignInURL(cfg.MobileBaseURL, token),
		"expires_in", store.InviteTTL.String())
	return nil
}

// runMintInvite issues a one-time invite from the host, which is the way back
// in when the first-boot one was lost or has lapsed.
func runMintInvite(args []string) error {
	fs := flag.NewFlagSet("mint-invite", flag.ContinueOnError)
	email := fs.String("email", "", "account to invite (default: the owner)")
	label := fs.String("label", "cli", "label recorded against the invite")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := store.ConnectWithRetry(ctx, cfg.DatabaseURL, 30*time.Second)
	if err != nil {
		return err
	}
	defer pool.Close()
	st := store.New(pool)

	var target store.User
	if *email == "" {
		target, err = st.GetOwner(ctx)
	} else {
		target, err = st.GetUserByEmail(ctx, *email)
	}
	if errors.Is(err, store.ErrNotFound) {
		return fmt.Errorf("mint-invite: no account for %q", *email)
	}
	if err != nil {
		return err
	}

	token, err := st.MintInvite(ctx, target.ID, *label)
	if err != nil {
		return err
	}

	// Straight to stdout rather than through the logger: this is the command's
	// output, and it should not be reshaped into JSON that a human then has to
	// unpick to find the token.
	fmt.Printf("invite for %s (%s)\n", target.DisplayName, target.Email)
	fmt.Printf("  token:   %s\n", token)
	if url := invite.SignInURL(cfg.MobileBaseURL, token); url != "" {
		fmt.Printf("  sign-in: %s\n", url)
	}
	fmt.Printf("  expires: %s from now, single use, not shown again\n", store.InviteTTL)
	return nil
}
