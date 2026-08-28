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
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/api"
	"github.com/Einlanzerous/chronicle/internal/asrclient"
	"github.com/Einlanzerous/chronicle/internal/audio"
	"github.com/Einlanzerous/chronicle/internal/config"
	"github.com/Einlanzerous/chronicle/internal/invite"
	"github.com/Einlanzerous/chronicle/internal/store"
	"github.com/Einlanzerous/chronicle/internal/transcribe"
	"github.com/Einlanzerous/chronicle/internal/upload"
	"github.com/Einlanzerous/chronicle/internal/watch"
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
	case "retranscribe":
		return runRetranscribe(args[1:])
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
  chronicle retranscribe [--memo ID]   release held memos back to the queue
  chronicle version                    print the build version

migrate defaults to "up". "down" without -n rolls everything back.
mint-invite defaults to the owner. The invite is shown once and expires.
retranscribe with no --memo releases every held memo. GET /admin/transcription
lists them and why each stopped.
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

	// Announced rather than silent: with no proxy secret the sign-in limiter
	// keys on Traefik's own address, so every browser and app request shares one
	// bucket and a stranger hammering the direct host can lock the owner out.
	//
	// The other half of that visibility is in clientIP: a secret that is set but
	// does not MATCH produces the same coarse behaviour, and warns per request
	// rather than at boot, because it is a running condition rather than a
	// configuration one (CHRN-75 §3).
	if cfg.ProxySecret == "" {
		logger.Warn("CHRONICLE_PROXY_SECRET is unset: the sign-in rate limit will apply "+
			"globally rather than per client, because every request through Traefik shares its address",
			"remedy", "set CHRONICLE_PROXY_SECRET to the value Traefik stamps on "+api.ProxySecretHeader)
	}
	// Warned and ignored rather than refused. compose pins :latest and
	// construct-server still sets this, so erroring would turn a retired knob
	// into a crash loop the moment the image lands ahead of the SERV change --
	// and unlike the half-configured Access pair, a retired variable that
	// affects nothing is not a silent security failure. It becomes an error one
	// release later.
	if cfg.RetiredTrustedProxies {
		logger.Warn("CHRONICLE_TRUSTED_PROXIES is set and is being IGNORED: it was retired by CHRN-75, "+
			"because no CIDR can distinguish Traefik from a neighbour on construct_net",
			"remedy", "remove it and set CHRONICLE_PROXY_SECRET instead")
	}
	// The corpus has nowhere to live until this is set, so the storage report
	// answers 503 and CHRN-19/20 will have nothing to write to. A warning
	// rather than a refusal to boot: nothing writes audio yet, and crash-
	// looping over an unused directory would be the worse default.
	if cfg.AudioDir == "" {
		logger.Warn("CHRONICLE_AUDIO_DIR is unset: there is no store for memo audio, "+
			"and GET /admin/storage will report that rather than a corpus",
			"remedy", "set CHRONICLE_AUDIO_DIR to an absolute path on the NVMe")
	}
	if !cfg.SecureCookies {
		logger.Warn("CHRONICLE_COOKIE_SECURE is off: the session cookie will be sent over plain HTTP. " +
			"This is only appropriate for a LAN install.")
	}

	var watcher *watch.Watcher
	var uploads *upload.Service
	var transcriber *transcribe.Service
	deps := api.Deps{
		DB:            st,
		Accounts:      st,
		Logger:        logger,
		Version:       buildVersion(),
		Commit:        commit,
		MobileBaseURL: cfg.MobileBaseURL,
		SecureCookies: cfg.SecureCookies,
		ProxySecret:   cfg.ProxySecret,
		Transcription: st,
	}
	if cfg.AudioDir != "" {
		audioStore, err := audio.New(cfg.AudioDir)
		if err != nil {
			return err
		}
		// Existence is checked here rather than created: a typo'd path
		// springing into being is how audio ends up on the container's
		// ephemeral layer instead of the NVMe, which looks like it works
		// right up until a redeploy takes the corpus with it.
		//
		// IsDir, not merely "it stats". A path naming a regular file would
		// otherwise boot cleanly and log "audio store ready", and the first
		// sign of it would be a storage report claiming one stray named "."
		// — which says nothing about the actual cause.
		info, err := os.Stat(audioStore.Root())
		if err != nil {
			return fmt.Errorf("CHRONICLE_AUDIO_DIR %s: %w (create it, or mount the volume)", audioStore.Root(), err)
		}
		if !info.IsDir() {
			return fmt.Errorf("CHRONICLE_AUDIO_DIR %s is not a directory", audioStore.Root())
		}
		deps.Audio = audioStore
		deps.Corpus = st
		logger.Info("audio store ready", "root", audioStore.Root())

		// The app's ingest path (CHRN-20). Wired inside this block for the same
		// reason the watcher is: an upload endpoint with nowhere to put a
		// finished recording would accept forty minutes of audio and have no
		// destination for it.
		uploads, err = upload.New(upload.Options{
			Audio:    audioStore,
			Sessions: st,
			Ingest:   st,
			Logger:   logger,
		})
		if err != nil {
			return err
		}
		deps.Uploads = uploads

		// Transcription (CHRN-27). Wired inside this block because it reads
		// recordings off disk: a pump with no audio store would find memos to
		// submit and have no bytes to send for any of them.
		if cfg.TranscriptionEnabled() {
			asr, err := asrclient.NewClientWithResponses(cfg.ASRBaseURL,
				asrclient.WithRequestEditorFn(transcribe.BearerAuth(cfg.ASRToken)))
			if err != nil {
				return fmt.Errorf("CHRONICLE_ASR_URL %s: %w", cfg.ASRBaseURL, err)
			}
			transcriber, err = transcribe.New(transcribe.Options{
				Store:    st,
				Audio:    audioStore,
				ASR:      asr,
				Logger:   logger,
				Model:    cfg.ASRModel,
				Interval: cfg.TranscribeInterval,
			})
			if err != nil {
				return err
			}
			deps.Transcribing = true
			// The URL, never the token. A credential in a log line is a
			// credential in every aggregator the estate has.
			logger.Info("transcription enabled", "asr", cfg.ASRBaseURL,
				"model", firstNonEmptyString(cfg.ASRModel, "the service default"))
		}

		// The Copyparty seam (CHRN-19). It needs the audio store, so it is
		// wired inside this block: an inbox with nowhere to copy files TO
		// would read every memo and drop it on the floor.
		if cfg.InboxDir != "" {
			// Checked at boot for the same reason the audio root is, and it
			// matters more here: a typo'd inbox is not a loud failure, it is a
			// watcher that reads an empty directory forever while memos pile
			// up somewhere nobody is looking. Scan() tolerates the directory
			// disappearing at RUNTIME — a sync client reorganising underneath
			// is ordinary and is not a reason to stop the loop — but it must
			// exist at the moment it is configured.
			inbox, err := os.Stat(cfg.InboxDir)
			if err != nil {
				return fmt.Errorf("CHRONICLE_INBOX_DIR %s: %w (create it, or mount the volume)", cfg.InboxDir, err)
			}
			if !inbox.IsDir() {
				return fmt.Errorf("CHRONICLE_INBOX_DIR %s is not a directory", cfg.InboxDir)
			}
			watcher, err = watch.New(watch.Options{
				Root:     cfg.InboxDir,
				Audio:    audioStore,
				Ingest:   st,
				Ledger:   st,
				Accounts: st,
				Logger:   logger,
				Interval: cfg.WatchInterval,
				Settle:   cfg.WatchSettle,
			})
			if err != nil {
				return err
			}
		}
	}
	if cfg.AudioDir == "" {
		// Said out loud, for the reason the inbox branch below is. Unset, there
		// is no upload service, and POST /memos/uploads answers 503 naming the
		// variable -- which a client sees but nobody watching the logs would.
		logger.Info("no upload endpoint: CHRONICLE_AUDIO_DIR is unset, so /memos/uploads will answer 503",
			"ingest", "none")
	}
	if cfg.InboxDir == "" {
		// Said out loud. Unset, no watcher is constructed and the configured
		// branch's "watching for memos" line never appears — so an operator
		// asking why nothing arrives from their phone has nothing to find.
		// REVIEW.md §8: the estate's cautionary tale is an integration that was
		// a silent no-op and looked like a working feature.
		logger.Info("no inbox watcher: CHRONICLE_INBOX_DIR is unset, so the Copyparty path is off",
			"ingest", "upload only")
	}
	if !cfg.TranscriptionEnabled() {
		// Said out loud, and at WARN rather than INFO. Without this, memos are
		// ingested, filed, and never transcribed -- and the system looks
		// entirely healthy right up until somebody goes looking for a
		// transcript and finds eight hundred memos in `captured`. REVIEW.md
		// section 8: the estate's cautionary tale is an integration that was a
		// silent no-op and looked like a working feature.
		logger.Warn("transcription is OFF: memos will be ingested and never transcribed",
			"remedy", "set CHRONICLE_ASR_URL and CHRONICLE_ASR_TOKEN (deploy/asr/)",
			"visible_at", "GET /admin/transcription")
	} else if cfg.AudioDir == "" {
		// Configured and unusable. Refused rather than warned, for the reason
		// the watcher pair below is: a pump that can read no recordings would
		// hold every memo it sees with an audio_missing reason, which reads as
		// a corpus problem rather than a configuration one.
		return fmt.Errorf("CHRONICLE_ASR_URL is set but CHRONICLE_AUDIO_DIR is not: " +
			"there are no recordings to submit for transcription")
	}
	if cfg.InboxDir != "" && cfg.AudioDir == "" {
		// Refused rather than warned. Watching with no audio store would read
		// every file, copy it nowhere, and record memos whose audio is
		// immediately CHRN-23's `missing` — the one state that means something
		// irreplaceable is gone. Better to not start.
		return fmt.Errorf("CHRONICLE_INBOX_DIR is set but CHRONICLE_AUDIO_DIR is not: " +
			"the watcher has nowhere to copy recordings to")
	}
	if cfg.SSOEnabled() {
		deps.CFAccess = api.NewCFAccessVerifier(cfg.CFAccessTeamDomain, cfg.CFAccessAUD...)
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

	// The watcher shares the server's context, so SIGTERM stops both. It is
	// waited on rather than abandoned: a scan is mid-copy often enough that
	// exiting under it would leave a temp file in the audio store on every
	// redeploy, and stop_grace_period already allows for it.
	var watching sync.WaitGroup
	if watcher != nil {
		watching.Add(1)
		go func() {
			defer watching.Done()
			if err := watcher.Run(ctx); err != nil {
				logger.Error("the inbox watcher stopped", "error", err)
			}
		}()
	}
	// The sweep collects ABANDONED PARTIAL UPLOADS, which is not what CHRN-22
	// does and must not be confused with it: a partial upload is regenerable
	// because the phone still holds the recording, while a finished memo's
	// audio is not. internal/upload/sweep.go states the difference at length.
	if uploads != nil {
		watching.Add(1)
		go func() {
			defer watching.Done()
			if err := uploads.Run(ctx); err != nil {
				logger.Error("the upload sweeper stopped", "error", err)
			}
		}()
	}
	// The transcription pump (CHRN-27). Shares the server's context, so
	// SIGTERM stops it, and is waited on rather than abandoned: a sweep is
	// often mid-submit, and exiting under one would leave an attempt row with
	// no job id -- recoverable, because the key is persisted and the next boot
	// re-sends it, but worth not manufacturing on every redeploy.
	if transcriber != nil {
		watching.Add(1)
		go func() {
			defer watching.Done()
			if err := transcriber.Run(ctx); err != nil {
				logger.Error("the transcription pump stopped", "error", err)
			}
		}()
	}

	err = api.Serve(ctx, srv, cfg.ShutdownGrace, logger)
	watching.Wait()
	return err
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

	token, err := st.MintInvite(ctx, owner.ID, store.InviteLabelBootstrap)
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
		if errors.Is(err, store.ErrNotFound) {
			// Naming the empty --email here would report the wrong problem:
			// nobody asked for an account called "".
			return fmt.Errorf("mint-invite: there is no owner account — has `chronicle migrate` run?")
		}
	} else {
		target, err = st.GetUserByEmail(ctx, *email)
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("mint-invite: no account for %q", *email)
		}
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

// firstNonEmptyString is the log-line helper for a value with a documented
// fallback, so a line never reads `model=""`.
func firstNonEmptyString(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

// runRetranscribe releases held memos back into the queue.
//
// This is the second half of CHRN-27's Done-when: *"a transcription failure
// leaves the memo in a state a human can see and retry."* GET
// /admin/transcription is the seeing; this is the retry.
//
// On the HOST rather than behind an HTTP verb, deliberately. Re-running
// transcription costs GPU time on a device Chronicle shares with Ollama and
// with Catenary, and an unmetered endpoint that queues work onto it is not
// something to ship before CHRN-26 has put a lease on that device.
func runRetranscribe(args []string) error {
	fs := flag.NewFlagSet("retranscribe", flag.ContinueOnError)
	memoID := fs.String("memo", "", "the memo to retry (default: every held memo)")
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

	var targets []store.HeldMemo
	if *memoID != "" {
		id, err := uuid.Parse(*memoID)
		if err != nil {
			return fmt.Errorf("retranscribe: %q is not a memo id", *memoID)
		}
		m, err := st.GetMemo(ctx, id)
		if errors.Is(err, store.ErrNotFound) {
			return fmt.Errorf("retranscribe: no memo %s", id)
		}
		if err != nil {
			return err
		}
		if m.State != store.StateHeld {
			// Named rather than shrugged at: releasing a memo that is already
			// transcribing would be a second attempt on the same recording.
			return fmt.Errorf("retranscribe: memo %s is in state %q, not held", id, m.State)
		}
		reason := ""
		if m.StateReason != nil {
			reason = *m.StateReason
		}
		targets = []store.HeldMemo{{ID: m.ID, AuthorID: m.AuthorID, CapturedAt: m.CapturedAt, Reason: reason}}
	} else {
		// No limit that hides work: releasing "some" held memos and not
		// saying which is worse than releasing none.
		targets, err = st.HeldMemos(ctx, 10000)
		if err != nil {
			return err
		}
	}

	if len(targets) == 0 {
		fmt.Println("no held memos")
		return nil
	}

	released := 0
	for _, h := range targets {
		if _, err := st.AdvanceMemoState(ctx, h.ID, store.StateHeld, store.StateQueued, ""); err != nil {
			fmt.Printf("  %s  NOT released: %v\n", h.ID, err)
			continue
		}
		fmt.Printf("  %s  released (was: %s)\n", h.ID, firstNonEmptyString(h.Reason, "no reason recorded"))
		released++
	}

	fmt.Printf("released %d of %d held memo(s)\n", released, len(targets))
	if released > 0 {
		fmt.Println("the running server picks these up on its next sweep; nothing else to do")
	}
	return nil
}
