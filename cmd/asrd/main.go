// Command asrd is the estate's transcription service — the job contract from
// CHRN-25 over the whisper.cpp runner CHRN-24 pinned.
//
// A SECOND BINARY IN CHRONICLE'S REPOSITORY, not a Chronicle subcommand. It has
// its own database, its own role, its own migrations and its own migrator, and
// it links nothing from Chronicle's store: Catenary is the second client, and
// the whole reason this is an estate service is that Catenary must not depend
// on Chronicle's schema. CHRN-29 decides whether it moves to its own repo, and
// says plainly that it is the last cheap moment to do so.
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/Einlanzerous/chronicle/internal/asr"
)

// Stamped at build time with -ldflags "-X main.version=..." / "-X main.commit=...".
// version defaults to EMPTY rather than to "dev": an -X flag passed with an
// empty value overwrites whatever default is written here, so the fallback has
// to live in code.
var (
	version = ""
	commit  = ""
)

func buildVersion() string {
	if version == "" {
		return "dev"
	}
	return version
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "asrd: %v\n", err)
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
	fmt.Fprint(os.Stderr, `asrd — the estate's transcription service (CHRN-25)

usage:
  asrd serve                           run the HTTP service and the worker
  asrd migrate [up|down] [-n N]        apply or roll back migrations
  asrd version                         print the build version

migrate defaults to "up". "down" without -n rolls everything back.
`)
}

func runServe(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := asr.Load()
	if err != nil {
		return err
	}
	logger := cfg.Logger(os.Stdout)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := asr.ConnectWithRetry(ctx, cfg.DatabaseURL, 30*time.Second)
	if err != nil {
		return err
	}
	defer pool.Close()

	// Applied on boot, as the house pattern does: the binary and its schema
	// ship together, so there is no window in which they disagree.
	if err := asr.Migrate(ctx, pool); err != nil {
		return err
	}
	logger.Info("migrations applied")

	store := asr.New(pool, cfg.Backend, cfg.ResultTTL)
	transcriber := &asr.CLITranscriber{
		WhisperBin: cfg.WhisperBin,
		FFmpegBin:  cfg.FFmpegBin,
		ModelDir:   cfg.ModelDir,
		Logger:     logger,
	}

	// Said out loud rather than discovered by a client. With no models on
	// disk every submit is a 400 naming a model the deployment does not have,
	// and the cause is a volume that was not mounted — which nothing in that
	// 400 points at.
	models := transcriber.Models()
	if len(models) == 0 {
		logger.Warn("no models found: every submit will be refused",
			"model_dir", cfg.ModelDir,
			"remedy", "mount the ggml-*.bin store at ASR_MODEL_DIR (deploy/asr/compose.asr.yml)")
	} else {
		logger.Info("models available", "models", models, "default", cfg.DefaultModel)
	}

	handler := asr.NewRouter(asr.Deps{
		Store:         store,
		Transcriber:   transcriber,
		Logger:        logger,
		Version:       buildVersion(),
		Commit:        commit,
		Tokens:        cfg.ClientTokens,
		DefaultModel:  cfg.DefaultModel,
		MaxAudioBytes: cfg.MaxAudioBytes,
	})
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// The worker id names this PROCESS. Included in every leased_by, so a
	// stalled lease can be traced to the container holding it.
	workerID := workerIdentity()

	logger.Info("asrd starting",
		"version", buildVersion(), "addr", cfg.Addr, "worker", cfg.Worker,
		"worker_id", workerID, "backend", cfg.Backend,
		"clients", len(cfg.ClientTokens))

	var running sync.WaitGroup

	// THE REAPER RUNS WHETHER OR NOT THIS PROCESS WORKS JOBS. It is what
	// returns a dead worker's job to the queue, and a deployment where every
	// process that could reap is also a process that could die holding a lease
	// is a deployment where the last one to die takes its job with it.
	reaper := &asr.Reaper{
		Store:  store,
		Logger: logger,
		// Well under the lease TTL: a lease that expires and then waits a full
		// interval to be noticed leaves the job idle for the sum of the two.
		Interval:      cfg.LeaseTTL / 3,
		PurgeInterval: time.Hour,
	}
	running.Add(1)
	go func() {
		defer running.Done()
		if err := reaper.Run(ctx); err != nil {
			logger.Error("the job reaper stopped", "error", err)
		}
	}()

	if cfg.Worker {
		worker := &asr.Worker{
			Store:       store,
			Transcriber: transcriber,
			Logger:      logger,
			ID:          workerID,
			LeaseTTL:    cfg.LeaseTTL,
			Idle:        time.Second,
		}
		running.Add(1)
		go func() {
			defer running.Done()
			if err := worker.Run(ctx); err != nil {
				logger.Error("the transcription worker stopped", "error", err)
			}
		}()
	} else {
		// Announced, for the reason Chronicle announces an absent watcher: an
		// operator asking why nothing transcribes has nothing to find
		// otherwise, and a queue that fills forever looks exactly like a
		// working service right up until someone polls a job.
		logger.Warn("ASR_WORKER is off: jobs will be accepted and never transcribed",
			"remedy", "unset ASR_WORKER, or run a worker process elsewhere")
	}

	err = asr.Serve(ctx, srv, cfg.ShutdownGrace, logger)
	running.Wait()
	return err
}

// workerIdentity names this process in leased_by. The hostname is the
// container id under compose, which is the handle an operator already has.
func workerIdentity() string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown"
	}
	return fmt.Sprintf("%s/%d", host, os.Getpid())
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

	cfg, err := asr.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := asr.ConnectWithRetry(ctx, cfg.DatabaseURL, 30*time.Second)
	if err != nil {
		return err
	}
	defer pool.Close()

	switch direction {
	case "up":
		if err := asr.Migrate(ctx, pool); err != nil {
			return err
		}
		fmt.Println("migrations up: ok")
	case "down":
		if err := asr.MigrateDown(ctx, pool, *n); err != nil {
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
