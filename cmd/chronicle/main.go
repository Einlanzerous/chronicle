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
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Einlanzerous/chronicle/internal/api"
	"github.com/Einlanzerous/chronicle/internal/asrclient"
	"github.com/Einlanzerous/chronicle/internal/audio"
	"github.com/Einlanzerous/chronicle/internal/config"
	"github.com/Einlanzerous/chronicle/internal/invite"
	"github.com/Einlanzerous/chronicle/internal/retention"
	"github.com/Einlanzerous/chronicle/internal/scribe"
	"github.com/Einlanzerous/chronicle/internal/scribe/catalogue"
	"github.com/Einlanzerous/chronicle/internal/scribe/prompt"
	"github.com/Einlanzerous/chronicle/internal/store"
	"github.com/Einlanzerous/chronicle/internal/switchyard"
	"github.com/Einlanzerous/chronicle/internal/transcribe"
	"github.com/Einlanzerous/chronicle/internal/triage"
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
	case "prune":
		return runPrune(args[1:])
	case "eval":
		return runEval(args[1:])
	case "tier1-audit":
		return runTier1Audit(args[1:])
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
  chronicle prune [--dry-run]          delete audio past its retention window
  chronicle eval [--dry-run]           score the router against the labelled set
  chronicle tier1-audit                prove chronicle_tier1 holds only what the tier boundary allows
  chronicle version                    print the build version

migrate defaults to "up". "down" without -n rolls everything back.
mint-invite defaults to the owner. The invite is shown once and expires.
retranscribe with no --memo releases every held memo. GET /admin/transcription
lists them and why each stopped.

prune deletes the audio of memos past their window that have a durable
transcript, and nothing else — never a pinned memo, never one that was not
transcribed, and never a transcript. --dry-run reads the same predicate a real
run marks with, and deletes nothing.

eval scores Scribe against CHRN-36's labelled set (docs/eval/routing-v1.yaml).
--dry-run resolves every label and checks its transcript pin without running a
model, and is the half that needs no GPU. --stratum synthetic needs no database
either — it routes against the committed fixture catalogue and reads only the
CHRONICLE_SCRIBE_* variables. Scoring --stratum real waits on CHRN-31's live
project list; when it lands, that stratum is HELD OUT, so log every run.

tier1-audit walks the catalogue on CHRONICLE_DATABASE_URL and reports every
privilege chronicle_tier1 holds outside the allow-list (CHRN-52), one per line
with the REVOKE that clears it. Exit 1 on any. Read-only. serve runs the same
audit at boot and refuses to start on a finding; run this after a provisioning
change and before promoting, so the refusal is never the first anyone hears.
`)
}

// openTier1Pool opens the pool derived writers run on and proves it is the
// right role. Three refusals and no fallback (CHRN-52 ruling 1):
//
//   - unset: the variable is missing, or equals the main DSN.
//   - unreachable: ConnectWithRetry's budget elapses. This STAYS an error
//     rather than becoming "use the main pool while the tier-1 one is down",
//     which is the tempting fix during exactly the outage it describes and
//     would reintroduce the fallback under a better excuse.
//   - wrong role: the DSN connects, but not as chronicle_tier1. Asked of the
//     database (current_user) rather than inferred from the variable, so an
//     operator who points the variable at the wrong DSN gets a refusal and
//     not a configuration that changes nothing and looks like it worked.
func openTier1Pool(ctx context.Context, cfg config.Config, logger *slog.Logger, maxWait time.Duration) (*pgxpool.Pool, *store.Tier1Store, error) {
	if !cfg.Tier1IsSeparate() {
		return nil, nil, fmt.Errorf("CHRONICLE_TIER1_DATABASE_URL is unset (or equals CHRONICLE_DATABASE_URL): " +
			"derived writers would run as the main role, which can write tier 2, and the tier boundary " +
			"would be granted with nothing standing behind it. Set it to a DSN for the chronicle_tier1 role; " +
			"there is no fallback (CHRN-52)")
	}
	pool, err := store.ConnectWithRetry(ctx, cfg.Tier1DatabaseURL, maxWait)
	if err != nil {
		return nil, nil, fmt.Errorf("tier-1 pool (CHRONICLE_TIER1_DATABASE_URL): %w; "+
			"not falling back to the main role — fix the DSN, the host, or the role's password", err)
	}
	tier1 := store.NewTier1(pool)
	role, err := tier1.Role(ctx)
	if err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("tier-1 pool: %w", err)
	}
	if role != store.Tier1RoleName {
		pool.Close()
		return nil, nil, fmt.Errorf("CHRONICLE_TIER1_DATABASE_URL connects as %q, want %q: the tier boundary "+
			"would be granted and nothing would stand behind it. Point it at a DSN for the chronicle_tier1 role",
			role, store.Tier1RoleName)
	}
	logger.Info("derived writers are isolated", "role", role,
		"reads", "tier2.memos, tier2.transcripts", "writes", "tier1 only")
	return pool, tier1, nil
}

// refuseIfTier1Widened runs the audit on the main pool and refuses on any
// finding (CHRN-52 ruling 2).
func refuseIfTier1Widened(ctx context.Context, pool *pgxpool.Pool, logger *slog.Logger) error {
	audit, err := store.AuditTier1Role(ctx, pool)
	if err != nil {
		return fmt.Errorf("tier-1 audit: %w", err)
	}
	return refuseIfWidened(audit, logger)
}

// refuseIfWidened is the decision, separated from the query so it can be
// tested against a report without a widened database: one error line per
// finding, carrying the REVOKE, then a refusal that says how many there were.
func refuseIfWidened(audit store.Tier1Audit, logger *slog.Logger) error {
	for _, f := range audit.Findings {
		logger.Error("chronicle_tier1 holds a privilege the tier boundary does not allow",
			"object", f.Object, "privilege", f.Privilege, "rule", f.Rule, "remedy", f.Remedy)
	}
	if n := len(audit.Findings); n > 0 {
		return fmt.Errorf("tier-1 audit: chronicle_tier1 holds %d privilege(s) outside the allow-list on %s; "+
			"refusing to serve until they are revoked (each is logged above with its remedy)", n, audit.Database)
	}
	logger.Info("tier-1 role audited", "database", audit.Database,
		"schemas", audit.Schemas, "relations", audit.Relations, "functions", audit.Functions, "findings", 0)
	return nil
}

// runTier1Audit is `chronicle tier1-audit`: the boot audit, on demand, for an
// operator and for construct-server's deploy gate. Read-only; exits 1 on any
// finding so a deploy step can gate on it.
func runTier1Audit(args []string) error {
	fs := flag.NewFlagSet("tier1-audit", flag.ContinueOnError)
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

	audit, err := store.AuditTier1Role(ctx, pool)
	if err != nil {
		return fmt.Errorf("tier-1 audit: %w", err)
	}
	fmt.Printf("tier-1 audit: database %s — %d schemas, %d relations, %d functions examined\n",
		audit.Database, audit.Schemas, audit.Relations, audit.Functions)
	for _, f := range audit.Findings {
		fmt.Println(f)
	}
	if n := len(audit.Findings); n > 0 {
		return fmt.Errorf("tier-1 audit: %d finding(s); chronicle_tier1 holds more than the tier boundary allows", n)
	}
	fmt.Println("tier-1 audit: clean")
	return nil
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

	// THE TIER-1 POOL (CHRN-32 §1.1, ruling R4; CHRN-52 rulings 1 and 2).
	// Derived work — Scribe now, CHRN-100's reads later — reaches the database
	// through this and not through `pool`, because migration 0007 grants
	// chronicle_tier1 SELECT on tier2.memos and tier2.transcripts and no write
	// anywhere in tier 2.
	//
	// IT NO LONGER FALLS BACK. Until CHRN-52 an unset CHRONICLE_TIER1_DATABASE_URL
	// meant "run derived writers as chronicle and warn", and production ran
	// that way from E4 until its deployment caught up — the boundary was
	// granted and nothing stood behind it. Ruling 1: serve refuses instead, in
	// all three ways the pool can be wrong, before the listener opens and
	// before any background loop starts. A crash loop with one clear line
	// beats a service that quietly derives as the role that can write tier 2.
	tier1Pool, tier1, err := openTier1Pool(ctx, cfg, logger, 30*time.Second)
	if err != nil {
		return err
	}
	defer tier1Pool.Close()

	// AND THE ROLE IS AUDITED before anything is served (ruling 2), on the
	// main pool, which can see the catalogue whatever the tier-1 pool can do.
	// The grant is the enforcement mechanism; this checks that the grant still
	// says what was decided, at the one cadence Chronicle itself controls: its
	// own boot. The other cadence — the deploy that runs another repository's
	// provisioning script — is construct-server's deploy gate, which runs
	// `chronicle tier1-audit` after the stack is healthy.
	if err := refuseIfTier1Widened(ctx, pool, logger); err != nil {
		return err
	}

	// The owner row is seeded by migration 0002 with a placeholder identity;
	// this is where CHRONICLE_OWNER_EMAIL / _NAME actually land, and where the
	// first human gets a way in.
	if err := bootstrapOwner(ctx, st, cfg, logger); err != nil {
		return err
	}

	// And the Scribe, which is an ACCOUNT rather than a process (CHRN-44).
	// Migration 0002 gave tier2.users a `kind` column for exactly this and
	// seeded no agent to put in it, so until now the participant an entire
	// epic is designed around did not exist.
	if err := bootstrapScribe(ctx, st, logger); err != nil {
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
	var pruner *retention.Pruner
	var transcriber *transcribe.Service
	var triager *triage.Service
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

		// CHRN-22's retention pruner, built HERE because this is where the
		// corpus is known to exist and to be a directory. A destructive job
		// whose root might be a typo is one that deletes the wrong thing, or
		// silently nothing at all.
		pruner = &retention.Pruner{
			Store:    st,
			Audio:    audioStore,
			Logger:   logger,
			Window:   audio.ProjectionWindow,
			Interval: retention.DefaultInterval,
		}

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
				Store:       st,
				Audio:       audioStore,
				ASR:         asr,
				Logger:      logger,
				Model:       cfg.ASRModel,
				Interval:    cfg.TranscribeInterval,
				MaxAttempts: cfg.ASRMaxAttempts,
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
			"remedy", "set CHRONICLE_ASR_URL and CHRONICLE_ASR_TOKEN (asr/)",
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
	// TRIAGE (CHRN-33) — the batch surface, and the one place derived state
	// becomes authored state.
	//
	// It needs BOTH halves of the routing configuration and not one, which is
	// why it is gated on the pair rather than on ScribeEnabled alone:
	//
	//   * the Scribe config names the PROPOSER, and the proposer is the
	//     identity of every proposal this surface reads. Without it there is no
	//     `(memo_id, proposer)` to check a generation echo against, so there is
	//     nothing to accept.
	//   * the Switchyard config lands a TICKET and supplies the deep link. A
	//     triage screen that could accept a TICKET and had nowhere to file it
	//     would refuse every accept at the last step, after the operator had
	//     already decided.
	//
	// Unset, the three routes answer 503 naming the variables. NOT 404: "not
	// configured here" and "wrong URL" are different facts, and this is the
	// shape /admin/storage and /admin/transcription already use.
	if cfg.ScribeEnabled() && cfg.SwitchyardConfigured() {
		proposer, err := scribe.Proposer("ollama", cfg.ScribeModel, prompt.Version)
		if err != nil {
			return err
		}
		sw, err := switchyard.New(cfg.SwitchyardURL, cfg.SwitchyardToken)
		if err != nil {
			return err
		}
		triager, err = triage.New(triage.Options{
			Store: st,
			// PROPOSALS ARE READ THROUGH THE TIER-1 STORE, on the tier-1 pool.
			// The accept writes tier 2 through `st`. The two never share a
			// transaction, and cannot: they connect as different roles.
			Tier1:        tier1,
			Tickets:      sw,
			Catalogue:    catalogue.NewLive(sw),
			Logger:       logger,
			Proposer:     proposer,
			PreacceptMin: cfg.ScribePreacceptMin,
		})
		if err != nil {
			return err
		}
		deps.Triage = triager
		logger.Info("triage enabled", "proposer", proposer,
			"switchyard", cfg.SwitchyardURL, "preaccept_min", cfg.ScribePreacceptMin)
	} else {
		// Said out loud. Without it, memos are transcribed and routed and then
		// sit in `transcribed` forever with nothing to accept them, and the
		// system looks entirely healthy — REVIEW.md section 8's cautionary tale
		// is an integration that was a silent no-op and looked like a feature.
		logger.Warn("triage is OFF: memos will be transcribed and never triaged",
			"remedy", "set CHRONICLE_SCRIBE_OLLAMA_URL, CHRONICLE_SCRIBE_MODEL, "+
				"CHRONICLE_SWITCHYARD_URL and CHRONICLE_SWITCHYARD_TOKEN",
			"visible_at", "GET /triage/batch")
	}

	// THE CAPTURE ARCHIVE (CHRN-50). Said out loud in both directions, because
	// neither state is discoverable from anywhere else yet: no surface renders
	// a note, so an operator has no page to look at and nothing else to read.
	//
	// INFO AND NOT WARN, in both branches, and that is the honest level. An
	// unset Amber is not a broken deployment — citations render as unconfigured,
	// which is a true thing to say — and a SET one is not yet a working feature
	// either, which is what the second half of that line exists to admit.
	// CHRN-97 owns whether references resolve server-side inside the note
	// payload or client-side against an endpoint of their own, and that is what
	// decides where the transport is registered; saying so here costs nothing
	// and answers none of it.
	if cfg.AmberConfigured() {
		// The URL, never the token.
		logger.Info("the capture archive is configured, and nothing resolves citations yet",
			"amber", cfg.AmberURL,
			"pending", "CHRN-97 decides where a reference resolver is built")
	} else {
		logger.Info("no capture archive: CHRONICLE_AMBER_URL and CHRONICLE_AMBER_TOKEN are unset, "+
			"so an amber1 citation will render as unconfigured rather than as an outage",
			"remedy", "set both to Amber's base URL and its shared API token")
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
	// CHRN-22's pruner. It exists only where there is a corpus, so a deployment
	// with no audio directory starts nothing here rather than sweeping a guess.
	if pruner != nil {
		watching.Add(1)
		go func() {
			defer watching.Done()
			if err := pruner.Run(ctx); err != nil {
				logger.Error("the retention pruner stopped", "error", err)
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

	// CHRN-33's sweep. It resolves decisions whose T2 never finished — a crash
	// between the outward call and the confirm — and every batch also sweeps
	// before it starts. This loop is for the rows nobody comes back to.
	if triager != nil {
		watching.Add(1)
		go func() {
			defer watching.Done()
			if err := triager.Run(ctx, triage.DefaultSweepInterval); err != nil {
				logger.Error("the triage sweep stopped", "error", err)
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

// bootstrapScribe makes sure the built-in agent account exists — CHRN-44.
//
// IT MINTS NOTHING, and the contrast with bootstrapOwner above is the point.
// The owner gets an invite because a person has to sign in; the Scribe is an
// identity for AUTHORSHIP and never signs in at all. `0002:19` says it in the
// schema: "'agent' is not a permission level, it is an authorship fact."
//
// A CONFLICT IS A WARNING AND NOT A BOOT FAILURE. If somebody has taken
// scribe@localhost as a person, refusing to start would take the whole service
// down over an account name; the discussion surface degrades to human-only,
// which is loud enough in the logs and recoverable by renaming that account.
func bootstrapScribe(ctx context.Context, st *store.Store, logger *slog.Logger) error {
	before, err := st.CountUsers(ctx)
	if err != nil {
		return fmt.Errorf("count users: %w", err)
	}

	scribe, err := st.EnsureAgent(ctx, store.ScribeEmail, store.ScribeDisplayName)
	if errors.Is(err, store.ErrNotAnAgent) {
		logger.Warn("the scribe address belongs to a person; agents cannot author discussion turns",
			"address", store.ScribeEmail,
			"remedy", "rename that account, then restart")
		return nil
	}
	if err != nil {
		return fmt.Errorf("ensure scribe: %w", err)
	}

	// Announced only when it actually created the row, so a restart is quiet.
	if after, err := st.CountUsers(ctx); err == nil && after > before {
		logger.Info("created the scribe account", "id", scribe.ID, "address", scribe.Email)
	}
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
// runPrune is CHRN-22's operator surface, and a SUBCOMMAND rather than an
// endpoint on purpose: a destructive job's rehearsal should not be reachable
// over HTTP.
func runPrune(args []string) error {
	fs := flag.NewFlagSet("prune", flag.ContinueOnError)
	dryRun := fs.Bool("dry-run", false, "list what a real run would delete, and delete nothing")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if cfg.AudioDir == "" {
		return fmt.Errorf("prune: CHRONICLE_AUDIO_DIR is not set, so there is no corpus to prune")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := store.ConnectWithRetry(ctx, cfg.DatabaseURL, 30*time.Second)
	if err != nil {
		return err
	}
	defer pool.Close()

	audioStore, err := audio.New(cfg.AudioDir)
	if err != nil {
		return err
	}

	p := &retention.Pruner{
		Store:  store.New(pool),
		Audio:  audioStore,
		Logger: cfg.Logger(os.Stdout),
		Window: audio.ProjectionWindow,
	}
	rep, err := p.Sweep(ctx, *dryRun)
	if err != nil {
		return err
	}
	for _, m := range rep.Considered {
		fmt.Printf("%s  %-12s  %9d bytes  captured %s\n",
			m.MemoID, m.Retention, m.ByteSize, m.CapturedAt.Format(time.RFC3339))
	}
	fmt.Println(rep)
	return nil
}

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
	var truncated bool
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
		// A cap, AND IT SAYS SO WHEN IT HITS ONE. The comment here used to
		// argue against a limit that hides work and then pass 10000, which is
		// a limit that hides work with a comment claiming otherwise.
		//
		// Fetched one over the cap so truncation is detectable rather than
		// inferred from a round number.
		targets, err = st.HeldMemos(ctx, retranscribeCap+1)
		if err != nil {
			return err
		}
		if len(targets) > retranscribeCap {
			targets = targets[:retranscribeCap]
			truncated = true
		}
	}

	if len(targets) == 0 {
		fmt.Println("no held memos")
		return nil
	}

	released := 0
	skipped := 0
	for _, h := range targets {
		// NEVER RELEASE A MEMO WHOSE AUDIO IS GONE. Nothing sets
		// audio_pruned_at until CHRN-22, so this cannot fire today — and the
		// day it can, releasing one would put it in `queued`, where
		// MemosAwaitingTranscription (which excludes pruned memos) never
		// returns it, no report lists it as stuck, and it counts in `pending`
		// forever. A silent permanent limbo is worse than a refusal that says
		// why.
		if m, err := st.GetMemo(ctx, h.ID); err == nil && m.AudioPruned() {
			fmt.Printf("  %s  NOT released: its audio was pruned, so there is nothing to transcribe\n", h.ID)
			skipped++
			continue
		}
		// SUPERSEDE FIRST, and this order is the whole of the command working.
		//
		// The pump's attempt ceiling counts attempt rows. Releasing the memo
		// without declaring those attempts spent means the next sweep counts
		// the same rows and holds it straight back -- so the command prints
		// "released", the operator believes it, and the memo is untranscribable
		// by any path this service or this CLI offers.
		spent, err := st.SupersedeMemoJobs(ctx, h.ID)
		if err != nil {
			fmt.Printf("  %s  NOT released: %v\n", h.ID, err)
			continue
		}
		if _, err := st.AdvanceMemoState(ctx, h.ID, store.StateHeld, store.StateQueued, ""); err != nil {
			fmt.Printf("  %s  NOT released: %v\n", h.ID, err)
			continue
		}
		fmt.Printf("  %s  released, %d previous attempt(s) cleared (was: %s)\n",
			h.ID, spent, firstNonEmptyString(h.Reason, "no reason recorded"))
		released++
	}

	fmt.Printf("released %d of %d held memo(s)\n", released, len(targets))
	if skipped > 0 {
		fmt.Printf("%d skipped because their audio is gone; a transcript is the only record now\n", skipped)
	}
	if truncated {
		fmt.Printf("MORE THAN %d held memos exist. Only the %d most recently updated were "+
			"considered; run this again to continue.\n", retranscribeCap, retranscribeCap)
	}
	if released > 0 {
		fmt.Println("the running server picks these up on its next sweep; nothing else to do")
	}
	return nil
}

// retranscribeCap bounds one run of `chronicle retranscribe`. It exists so the
// command cannot build an unbounded slice on a corpus that has gone badly
// wrong; when it binds, the command says so rather than reporting a total that
// silently means "the first ten thousand".
const retranscribeCap = 10000
