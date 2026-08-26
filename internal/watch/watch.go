// Package watch turns files appearing in an inbox directory into memo rows.
//
// Copyparty already serves the estate's NVMe, so this removes the Google Drive
// dependency vox-dictate had rather than adding a new one. Chronicle does not
// talk to Copyparty: it watches the directory Copyparty writes into, which
// means the seam survives Copyparty being replaced.
//
// # The watcher observes, it never consumes
//
// The obvious design — move each file out of the inbox into the audio store —
// is wrong here and CHRN-18 §3 says so. The inbox is fed by a sync client the
// estate does not control. Moving a file out from under a two-way sync either
// causes it to be re-pushed (a loop) or propagates the removal back to the
// phone, which deletes the person's own recording. So files are COPIED, and the
// inbox is left exactly as it was found.
//
// # Which means a rescan must not be a re-delivery
//
// Leaving files in place means seeing them again on every poll, forever. A
// durable tier-1 ledger keyed on (path, size, mtime) is what stops that being a
// re-hash of the whole corpus and a second arrival row every few seconds.
package watch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/audio"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// Ingestor is the slice of the store that turns an arrival into a memo.
type Ingestor interface {
	IngestMemo(ctx context.Context, in store.Arrival) (store.IngestResult, error)
}

// Ledger is the tier-1 seen-ledger.
type Ledger interface {
	LoadSeen(ctx context.Context) (store.SeenIndex, error)
	MarkSeen(ctx context.Context, f store.SeenFile) error
}

// Directory resolves an inbox subdirectory name to an account.
type Directory interface {
	ListMembers(ctx context.Context) ([]store.Member, error)
}

const (
	// DefaultInterval is how often the inbox is rescanned. The Done-when is
	// "appears as a row within seconds"; the ledger makes a scan a stat per
	// file, so this can be short without costing anything.
	DefaultInterval = 5 * time.Second

	// DefaultSettle is how long a file must have been untouched before it is
	// read. This is the first of two guards against reading a half-written
	// upload, and the weaker one — see Scan for the second.
	DefaultSettle = 10 * time.Second
)

// Options configures a Watcher. Only Root, Audio, Ingest, Ledger and Accounts
// are required.
type Options struct {
	Root     string
	Audio    *audio.Store
	Ingest   Ingestor
	Ledger   Ledger
	Accounts Directory
	Logger   *slog.Logger

	Interval time.Duration
	Settle   time.Duration

	// Now is injectable so the settle window is testable without sleeping.
	Now func() time.Time
}

// Watcher scans an inbox and ingests what it finds.
type Watcher struct {
	root     string
	audio    *audio.Store
	ingest   Ingestor
	ledger   Ledger
	accounts Directory
	logger   *slog.Logger
	interval time.Duration
	settle   time.Duration
	now      func() time.Time

	// warned remembers which unresolvable directories have already been
	// reported, so an inbox subdirectory that belongs to nobody does not
	// produce one line every five seconds forever.
	warned map[string]bool
}

// New validates the options and returns a Watcher.
func New(o Options) (*Watcher, error) {
	switch {
	case o.Root == "":
		return nil, fmt.Errorf("watch: inbox root is required")
	case !filepath.IsAbs(o.Root):
		return nil, fmt.Errorf("watch: inbox root %q must be an absolute path", o.Root)
	case o.Audio == nil:
		return nil, fmt.Errorf("watch: an audio store is required")
	case o.Ingest == nil, o.Ledger == nil, o.Accounts == nil:
		return nil, fmt.Errorf("watch: ingest, ledger and accounts are all required")
	}

	w := &Watcher{
		root:     filepath.Clean(o.Root),
		audio:    o.Audio,
		ingest:   o.Ingest,
		ledger:   o.Ledger,
		accounts: o.Accounts,
		logger:   o.Logger,
		interval: o.Interval,
		settle:   o.Settle,
		now:      o.Now,
		warned:   map[string]bool{},
	}
	if w.logger == nil {
		w.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	if w.interval <= 0 {
		w.interval = DefaultInterval
	}
	if w.settle <= 0 {
		w.settle = DefaultSettle
	}
	if w.now == nil {
		w.now = time.Now
	}
	return w, nil
}

// Result is what one scan did.
type Result struct {
	Considered int
	Ingested   int
	Collapsed  int
	Skipped    int // already in the ledger
	Unsettled  int // still being written, or too recently touched
	Failed     int
}

// Run scans until ctx is cancelled.
//
// A scan that fails is logged and retried on the next tick rather than stopping
// the loop: the failure modes here are a database blip and a file that vanished
// mid-read, and neither is a reason to stop watching for memos.
func (w *Watcher) Run(ctx context.Context) error {
	w.logger.Info("watching for memos",
		"root", w.root, "interval", w.interval, "settle", w.settle)

	t := time.NewTicker(w.interval)
	defer t.Stop()

	for {
		// Scan immediately rather than after the first tick. Restarting the
		// service is the commonest way it is ever down, and "recovers what it
		// missed" should not begin with a deliberate wait.
		if _, err := w.Scan(ctx); err != nil && !errors.Is(err, context.Canceled) {
			w.logger.Error("inbox scan failed", "root", w.root, "error", err)
		}
		select {
		case <-ctx.Done():
			w.logger.Info("stopped watching for memos", "root", w.root)
			return nil
		case <-t.C:
		}
	}
}

// Scan reads the inbox once.
//
// Recovery after downtime is not a separate path: a file that arrived while the
// service was down is simply a file the ledger has not seen, and its mtime is
// well outside the settle window, so the first scan after a restart ingests it.
// The third Done-when is a property of there being only one code path, not of
// code written for it.
func (w *Watcher) Scan(ctx context.Context) (Result, error) {
	var res Result

	accounts, err := w.accountDirs(ctx)
	if err != nil {
		return res, err
	}
	if len(accounts) == 0 {
		return res, nil
	}

	seen, err := w.ledger.LoadSeen(ctx)
	if err != nil {
		return res, err
	}

	for dir, authorID := range accounts {
		if err := ctx.Err(); err != nil {
			return res, err
		}
		if err := w.scanAccount(ctx, dir, authorID, seen, &res); err != nil {
			return res, err
		}
	}
	return res, nil
}

// accountDirs maps each inbox subdirectory to the account it belongs to.
//
// The directory name is the account's UUID. A file carries no identity of its
// own and tier2.memos.author_id is NOT NULL, so the directory is the only thing
// that can supply one — which is exactly why the watcher NEVER CREATES ONE. A
// directory that does not resolve to an existing account is reported and
// skipped, so dropping files into an invented path ingests nothing.
func (w *Watcher) accountDirs(ctx context.Context) (map[string]uuid.UUID, error) {
	entries, err := os.ReadDir(w.root)
	if err != nil {
		if os.IsNotExist(err) {
			// Not an error: the inbox is created by the deploy, and a service
			// that has not been given one yet has nothing to watch.
			return nil, nil
		}
		return nil, fmt.Errorf("watch: reading inbox %s: %w", w.root, err)
	}

	members, err := w.accounts.ListMembers(ctx)
	if err != nil {
		return nil, fmt.Errorf("watch: listing accounts: %w", err)
	}
	byID := make(map[string]uuid.UUID, len(members))
	for _, m := range members {
		byID[m.ID.String()] = m.ID
	}

	out := map[string]uuid.UUID{}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		id, ok := byID[e.Name()]
		if !ok {
			if !w.warned[e.Name()] {
				w.warned[e.Name()] = true
				w.logger.Warn("inbox subdirectory does not name an account; ignoring it",
					"dir", e.Name(), "root", w.root,
					"remedy", "name the directory after the account's UUID")
			}
			continue
		}
		delete(w.warned, e.Name())
		out[filepath.Join(w.root, e.Name())] = id
	}
	return out, nil
}

func (w *Watcher) scanAccount(ctx context.Context, dir string, authorID uuid.UUID, seen store.SeenIndex, res *Result) error {
	return filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			// A directory that vanished mid-walk is ordinary when a sync
			// client is reorganising underneath us. Note it and carry on
			// rather than abandoning the other accounts.
			w.logger.Warn("skipping unreadable inbox path", "path", path, "error", err)
			if entry != nil && entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		if isPartial(entry.Name()) {
			res.Unsettled++
			return nil
		}

		info, err := entry.Info()
		if err != nil {
			w.logger.Warn("skipping unreadable inbox file", "path", path, "error", err)
			return nil
		}
		if info.Size() == 0 {
			// An upload that has been created but not written. Not an error,
			// and not something to hash — tier2.memos.byte_size is CHECK > 0.
			res.Unsettled++
			return nil
		}

		res.Considered++

		if seen.Matches(path, info.Size(), info.ModTime()) {
			res.Skipped++
			return nil
		}
		if w.now().Sub(info.ModTime()) < w.settle {
			res.Unsettled++
			return nil
		}

		out, err := w.ingestFile(ctx, path, authorID, info)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			res.Failed++
			w.logger.Error("failed to ingest a memo from the inbox",
				"path", path, "author_id", authorID, "error", err)
			return nil
		}
		if out.changedWhileReading {
			// Not a failure. The file was still being written; it will be
			// steady on some later scan and the ledger has not recorded it.
			res.Unsettled++
			return nil
		}
		res.Ingested++
		if out.collapsed {
			res.Collapsed++
		}
		return nil
	})
}

// isPartial reports names that are an upload in progress rather than a memo.
//
// Copyparty writes chunked uploads to a `.PARTIAL` sidecar and renames on
// completion, and the dotfile and tilde cases catch the editors and sync
// clients that do the same thing with their own spelling. This is cheap and it
// is NOT the guarantee — a file with an ordinary name can still be mid-write,
// which is what the settle window and the re-stat in ingestFile are for.
func isPartial(name string) bool {
	lower := strings.ToLower(name)
	switch {
	case strings.HasPrefix(name, "."), strings.HasPrefix(name, "~"):
		return true
	case strings.HasSuffix(lower, ".partial"),
		strings.HasSuffix(lower, ".part"),
		strings.HasSuffix(lower, ".tmp"),
		strings.HasSuffix(lower, ".crdownload"),
		strings.HasSuffix(lower, ".filepart"):
		return true
	}
	return false
}
