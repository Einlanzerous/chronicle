// Command asrd is the estate's transcription service — the job contract from
// CHRN-25 over the whisper.cpp runner CHRN-24 pinned.
//
// A SECOND BINARY IN CHRONICLE'S REPOSITORY, not a Chronicle subcommand. It has
// its own database, its own role, its own migrations and its own migrator, and
// it links nothing from Chronicle at all: Catenary is the second client, and
// the whole reason this is an estate service is that Catenary must not depend
// on Chronicle's schema. CHRN-82 decided it stays here, in the asr/ subtree,
// with its own release (asr-v*) and a sealed boundary — nothing under asr/
// imports anything outside it — so that if it ever becomes its own repository
// the move is a filter-repo, a go.mod and an import-path rewrite, with nothing
// of Chronicle's to untangle. docs/decisions/chrn-82-asr-subtree-and-publish.md.
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

	"github.com/Einlanzerous/chronicle/asr/internal/asr"
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

	// THE DEVICE LOCK IS TAKEN ONCE, FOR THE PROCESS'S LIFETIME, and not per
	// inference. It says something more useful than "an inference is running":
	// this asrd owns the card. A process that cannot get it is a standby — it
	// claims no jobs and loads no model, so it holds no VRAM either.
	//
	// Unlike the reaper below, it runs ONLY in a process that works jobs: a
	// process with no worker has no device to own, and taking the lock would
	// make it a standby that nothing is waiting on.
	deviceLock := &asr.DeviceLock{
		DSN:      cfg.DatabaseURL,
		DeviceID: cfg.DeviceID,
		Logger:   logger,
		Poll:     5 * time.Second,
	}

	transcriber := &asr.Resident{
		Bin:            cfg.WhisperServerBin,
		Addr:           cfg.WhisperServerAddr,
		ModelDir:       cfg.ModelDir,
		FFmpegBin:      cfg.FFmpegBin,
		Model:          cfg.DefaultModel,
		Logger:         logger,
		ExpectedRates:  cfg.ExpectedRates,
		DeadlineFactor: cfg.InferenceDeadlineFactor,
		MinDeadline:    cfg.MinInferenceDeadline,
		LoadDeadline:   asr.DefaultLoadDeadline,
		DecodeDeadline: asr.DefaultDecodeDeadline,
		StartTimeout:   2 * time.Minute,
		Gate:           deviceLock.Held,
	}

	// Said out loud rather than discovered by a client. With no models on
	// disk every submit is a 400 naming a model the deployment does not have,
	// and the cause is a volume that was not mounted — which nothing in that
	// 400 points at.
	models := transcriber.Models()
	if len(models) == 0 {
		logger.Warn("no models found: every submit will be refused",
			"model_dir", cfg.ModelDir,
			"remedy", "mount the ggml-*.bin store at ASR_MODEL_DIR (asr/deploy/compose.asr.yml)")
	} else {
		logger.Info("models available", "models", models, "default", cfg.DefaultModel)
	}

	deps := asr.Deps{
		Store:         store,
		Transcriber:   transcriber,
		Logger:        logger,
		Version:       buildVersion(),
		Commit:        commit,
		Tokens:        cfg.ClientTokens,
		DefaultModel:  cfg.DefaultModel,
		MaxAudioBytes: cfg.MaxAudioBytes,
	}
	if cfg.Worker {
		// Readiness answers for the device only in a process that has one.
		deps.Device = transcriber.State
	}
	handler := asr.NewRouter(deps)
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// The worker id names this PROCESS and the DEVICE it works. Included in
	// every leased_by, so a stalled lease can be traced to the container
	// holding it — and, once there is more than one card, to the card.
	workerID := workerIdentity(cfg.DeviceID)

	logger.Info("asrd starting",
		"version", buildVersion(), "addr", cfg.Addr, "worker", cfg.Worker,
		"worker_id", workerID, "device", cfg.DeviceID, "backend", cfg.Backend,
		"model", cfg.DefaultModel, "clients", len(cfg.ClientTokens))

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
		MaxAttempts:   cfg.MaxAttempts,
	}
	running.Add(1)
	go func() {
		defer running.Done()
		if err := reaper.Run(ctx); err != nil {
			logger.Error("the job reaper stopped", "error", err)
		}
	}()

	if cfg.Worker {
		running.Add(1)
		go func() {
			defer running.Done()
			if err := deviceLock.Run(ctx); err != nil {
				logger.Error("the device lock stopped", "error", err)
			}
		}()

		// The supervisor. A resident process nobody supervises is a resident
		// process that dies once and turns the service into a queue that fills
		// forever — which looks exactly like a busy service.
		running.Add(1)
		go func() {
			defer running.Done()
			if err := transcriber.Run(ctx); err != nil {
				logger.Error("the resident worker supervisor stopped", "error", err)
			}
		}()

		worker := &asr.Worker{
			Store:              store,
			Transcriber:        transcriber,
			Logger:             logger,
			ID:                 workerID,
			LeaseTTL:           cfg.LeaseTTL,
			Idle:               time.Second,
			ResidentModel:      func() string { return transcriber.State().Model },
			ModelSwitchMaxWait: cfg.ModelSwitchMaxWait,
			MaxAttempts:        cfg.MaxAttempts,
			MaxAttemptsWedged:  cfg.MaxAttemptsWedged,
			Device:             deviceLock.Held,
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

// workerIdentity names this process in leased_by: the device it works, then
// the hostname — the container id under compose, which is the handle an
// operator already has — then the pid.
//
// The device is first because it is the part that will stop being constant.
// GET /admin/transcription can say which card transcribed what once there is
// more than one, without a schema change to get there.
func workerIdentity(deviceID string) string {
	host, err := os.Hostname()
	if err != nil || host == "" {
		host = "unknown"
	}
	return fmt.Sprintf("%s/%s/%d", deviceID, host, os.Getpid())
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
