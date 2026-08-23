// Command chronicle is Chronicle's single static binary.
//
// CHRN-14 ships the migrate and version subcommands; CHRN-15 adds serve.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/Einlanzerous/chronicle/internal/api"
	"github.com/Einlanzerous/chronicle/internal/config"
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
  chronicle version                    print the build version

migrate defaults to "up". "down" without -n rolls everything back.
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

	handler := api.NewRouter(api.Deps{DB: pool, Logger: logger, Version: buildVersion(), Commit: commit})
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	logger.Info("chronicle starting", "version", buildVersion(), "addr", cfg.Addr, "log_format", cfg.LogFormat)
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
