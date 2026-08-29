// Package upload is the direct ingest path: the endpoint the Chronicle app
// posts recordings to (CHRN-20).
//
// # Why it is resumable, and what that costs
//
// The client is a phone on mobile data sending Opus recorded at arbitrary
// length. CHRN-20 states the failure plainly: a 40-minute memo that fails at
// 90% and restarts from zero will fail again. So a transfer has to survive
// being cut, which means the server has to hold partial bytes and be able to
// say how many it holds.
//
// # The offset is the staging file's size. It is never also a column.
//
// That is the single decision the rest of this file falls out of. The obvious
// alternative — record the offset in the session row and append to the file —
// gives two accounts of one fact that a crash between the write and the commit
// puts permanently out of step, and the direction of the error decides whether
// the memo ends up truncated or corrupt. Reading the size back from the
// filesystem cannot disagree with the filesystem.
//
// # The client declares the hash before it sends anything
//
// CHRN-18 made the SHA-256 of the arriving bytes the memo's identity, and a
// hash is only knowable after the last byte. Asking the client for it up front
// buys two things a server-side-only hash cannot:
//
//   - **Re-delivery becomes free.** An author who already holds those bytes is
//     told so at the first request, and E2's "re-delivery is a no-op" costs one
//     round trip rather than a whole transfer. A phone whose queue flushed but
//     whose acknowledgement was lost is the ordinary case, not the exotic one.
//   - **A truncated or mangled transfer is caught.** What arrived is hashed and
//     compared, and a memo is never written from bytes that did not match.
//
// It is DECLARED, never trusted: nothing here believes the client's hash, it
// only checks against it. And the shortcut is gated on the file actually being
// on disk, because a memo row whose audio is not there is CHRN-23's `missing`,
// the one state that means something irreplaceable is gone.
//
// # Deliberately not tus
//
// The offset semantics are tus-shaped and `Upload-Offset` is spelled the way
// tus spells it, because it is the obvious name and the client here is one we
// write. This is not an implementation of tus: no `Tus-Resumable`, no OPTIONS
// discovery, no extensions. A real tus client fails fast on the missing version
// header rather than misbehaving, which is the right failure.
package upload

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/audio"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// Sessions is the tier-1 bookkeeping this needs.
type Sessions interface {
	OpenUpload(ctx context.Context, in store.Upload) (store.Upload, bool, error)
	GetUpload(ctx context.Context, id uuid.UUID) (store.Upload, error)
	TouchUpload(ctx context.Context, id uuid.UUID) error
	DeleteUpload(ctx context.Context, id uuid.UUID) error
	ClearUploadKey(ctx context.Context, authorID uuid.UUID, key string) error
	CountOpenUploads(ctx context.Context, authorID uuid.UUID) (int, error)
	StaleUploads(ctx context.Context, before time.Time) ([]store.Upload, error)
	LiveUploadIDs(ctx context.Context) (map[uuid.UUID]struct{}, error)
}

// Ingestor is the slice of the store that turns an arrival into a memo, and
// records what that recording turned out to be (CHRN-21). The same interface
// the watcher takes, and deliberately so: both paths converge on one function
// and one set of idempotency rules.
type Ingestor interface {
	IngestMemo(ctx context.Context, in store.Arrival) (store.IngestResult, error)
	SetMemoAudioInfo(ctx context.Context, id uuid.UUID, in store.AudioInfo) (store.Memo, error)

	// AudioPrunedFor is CHRN-22's: audio is delivered once, and a memo whose
	// audio the pruner has taken does not get it back by re-uploading.
	AudioPrunedFor(ctx context.Context, authorID uuid.UUID, contentHash string) (bool, error)
}

const (
	// DefaultMaxBytes bounds one declared upload.
	//
	// It is not a guess at how long a memo can be. An hour of Opus at the
	// bitrate a phone records voice at is around 14 MB, so this is roughly
	// seventy hours of audio and about 0.4% of the free NVMe. It exists to
	// bound a mistake — a client declaring a size it computed wrongly, or
	// sending something that is not a voice memo at all — rather than to be a
	// limit anybody meets.
	DefaultMaxBytes int64 = 1 << 30

	// DefaultMaxOpen bounds how many sessions one account may hold at once.
	//
	// Each one can hold its declared size on disk before anything checks
	// whether it will ever finish, so without this an authenticated client can
	// fill the volume with abandoned partials and nothing but the sweep would
	// notice, a week later. The Android queue uploads serially; 32 is room for
	// a backlog and a few interrupted attempts, not a workload.
	DefaultMaxOpen = 32

	// DefaultTTL is how long a session survives with no progress.
	//
	// Measured from last activity rather than from creation, so this bounds
	// ABANDONMENT and not duration: an upload still making progress after a
	// week is not stale, it is slow, and slow is the case this endpoint was
	// built for. Seven days is generous because the cost of being wrong is
	// asymmetric — expiring early makes a phone re-send bytes it still holds,
	// while expiring late costs disk that the sizing has three orders of
	// magnitude of headroom for.
	DefaultTTL = 7 * 24 * time.Hour

	// DefaultSweepInterval is how often abandoned sessions are collected.
	// Nothing depends on its promptness — a session is expired by the clock,
	// not by having been swept — so this is unhurried on purpose.
	DefaultSweepInterval = time.Hour

	// unclaimedGrace is how old a staging file with no session row must be
	// before the sweep removes it. See Sweep for why a second guard exists on
	// top of the ordering.
	unclaimedGrace = time.Hour
)

// OffsetConflict reports that the client and the server disagree about how far
// the upload has got. It carries the server's answer, which is what makes it
// the resume mechanism rather than merely an error: a client that has lost
// track resends from Offset instead of from zero.
type OffsetConflict struct {
	Offset int64
}

func (e *OffsetConflict) Error() string {
	return fmt.Sprintf("upload: offset does not match; the server holds %d bytes", e.Offset)
}

// TransferCut reports that the request body ended before the chunk did — the
// connection died mid-send.
//
// Whatever landed is KEPT and Offset is where the upload now stands, so this is
// a resume instruction rather than a fault. Decision 5 calls a cut transfer the
// ordinary event this endpoint was built for; typed rather than a bare wrapped
// error so the handler can classify it as one, instead of it falling through to
// a 500 and an ERROR line indistinguishable from a real fault.
type TransferCut struct {
	Offset int64
	Err    error
}

func (e *TransferCut) Error() string {
	return fmt.Sprintf("upload: the request body ended after %d bytes: %v", e.Offset, e.Err)
}

func (e *TransferCut) Unwrap() error { return e.Err }

var (
	// ErrHashMismatch means the bytes that arrived are not the bytes that were
	// declared. The session is destroyed: a memo is never written from
	// unverified content, and the client must start again.
	ErrHashMismatch = errors.New("upload: content does not match the declared hash")

	// ErrOversend means the client sent more than it said it would. The chunk
	// is discarded rather than truncated to fit — see Append.
	ErrOversend = errors.New("upload: more bytes than declared")

	// ErrStagingLost means a session's bytes vanished before they could be
	// recorded, so there is nothing to make a memo out of. The declaration is
	// still good and the session is left alone — the client resumes from zero.
	//
	// It exists so that "the bytes are gone" is answerable. The alternative was
	// to commit anyway on the strength of a comment about what the caller had
	// checked, which is how a memo gets written with no audio behind it.
	ErrStagingLost = errors.New("upload: the bytes are no longer on disk; send them again")

	// ErrTooLarge and ErrTooManyOpen are the two bounds on what an
	// authenticated client may ask for.
	ErrTooLarge    = errors.New("upload: declared size exceeds the maximum")
	ErrTooManyOpen = errors.New("upload: too many uploads already open")

	// ErrNotConfigured means CHRONICLE_AUDIO_DIR is unset, so there is nowhere
	// to put a recording.
	ErrNotConfigured = errors.New("upload: no audio store is configured")
)

// Options configures a Service. Audio, Sessions and Ingest are required.
type Options struct {
	Audio    *audio.Store
	Sessions Sessions
	Ingest   Ingestor
	Logger   *slog.Logger

	MaxBytes      int64
	MaxOpen       int
	TTL           time.Duration
	SweepInterval time.Duration

	// Now is injectable so expiry is testable without sleeping.
	Now func() time.Time
}

// Service owns uploads in flight.
type Service struct {
	audio    *audio.Store
	sessions Sessions
	ingest   Ingestor
	logger   *slog.Logger

	maxBytes      int64
	maxOpen       int
	ttl           time.Duration
	sweepInterval time.Duration
	now           func() time.Time

	// Appends to one session are serialised in-process. Two chunks racing on
	// one file would both pass the offset check and interleave their writes,
	// and the hash would then reject a transfer that was merely concurrent.
	//
	// In-process is enough because Chronicle runs as a single container and
	// there is no story here for two of them. If that ever changes, the offset
	// itself is already safe — it is the file's size — and what would be needed
	// is a Postgres advisory lock on the session id, the same shape IngestMemo
	// already uses for a same-key arrival.
	mu    sync.Mutex
	locks map[uuid.UUID]*sessionLock
}

// New builds a Service.
func New(o Options) (*Service, error) {
	if o.Audio == nil {
		return nil, ErrNotConfigured
	}
	if o.Sessions == nil || o.Ingest == nil {
		return nil, fmt.Errorf("upload: sessions and ingest are required")
	}
	s := &Service{
		audio:         o.Audio,
		sessions:      o.Sessions,
		ingest:        o.Ingest,
		logger:        o.Logger,
		maxBytes:      o.MaxBytes,
		maxOpen:       o.MaxOpen,
		ttl:           o.TTL,
		sweepInterval: o.SweepInterval,
		now:           o.Now,
		locks:         map[uuid.UUID]*sessionLock{},
	}
	if s.logger == nil {
		s.logger = slog.Default()
	}
	if s.maxBytes <= 0 {
		s.maxBytes = DefaultMaxBytes
	}
	if s.maxOpen <= 0 {
		s.maxOpen = DefaultMaxOpen
	}
	if s.ttl <= 0 {
		s.ttl = DefaultTTL
	}
	if s.sweepInterval <= 0 {
		s.sweepInterval = DefaultSweepInterval
	}
	if s.now == nil {
		s.now = time.Now
	}
	return s, nil
}

// MaxBytes is the largest declared size this service accepts.
func (s *Service) MaxBytes() int64 { return s.maxBytes }

// Session is an upload in flight, as a client sees it.
type Session struct {
	ID        uuid.UUID
	ByteSize  int64
	Offset    int64
	ExpiresAt time.Time
}

// Remaining is how many bytes are still expected.
func (s Session) Remaining() int64 { return s.ByteSize - s.Offset }

// Committed is a finished upload: the memo it became, and whether these bytes
// were already known.
type Committed struct {
	Memo store.Memo
	// Collapsed comes from IngestResult.Collapsed and NEVER from a delivery
	// count — CHRN-18 §10, and the round-1 finding on PR #7. A same-key retry
	// deliberately writes no arrival row, so the count stays at 1 through eight
	// retries and the inference would report false for exactly the case the
	// signal exists to make visible.
	Collapsed  bool
	Deliveries int
}

// Result is one or the other: a session still expecting bytes, or a memo.
type Result struct {
	Session   *Session
	Committed *Committed

	// Created distinguishes a session that did not exist a moment ago from one
	// that was resumed, so the handler can answer 201 or 200 truthfully.
	Created bool
}

// OpenRequest is what a client declares before it sends anything.
type OpenRequest struct {
	AuthorID         uuid.UUID
	IdempotencyKey   string
	ContentHash      string
	ByteSize         int64
	Retention        string
	OriginalFilename string
}

// Open starts an upload, resumes one, or reports that there is nothing to send.
//
// The three outcomes are the whole of the client's entry point, and the third
// is the one that makes re-delivery cheap:
//
//  1. This author does not hold these bytes → a session, offset 0, Created.
//  2. A session under this key already exists → the same session, at whatever
//     offset its staging file reached.
//  3. This author already holds these bytes ON DISK → the arrival is recorded
//     and the memo returned, with nothing transferred.
//  4. [CHRN-22] This author held them and the pruner has taken them → the same
//     answer as 3, and still nothing transferred. Audio is delivered once.
func (s *Service) Open(ctx context.Context, in OpenRequest) (Result, error) {
	if in.ByteSize > s.maxBytes {
		return Result{}, fmt.Errorf("%w: %d bytes, limit is %d", ErrTooLarge, in.ByteSize, s.maxBytes)
	}

	// Outcome 3, checked before anything is created. Gated on the FILE, not on
	// the memo row: without that gate a client could declare a hash it does not
	// have the bytes for and mint a memo whose audio is missing, which is the
	// one reconciliation state that means something irreplaceable is gone.
	ref := audio.Ref{AuthorID: in.AuthorID, ContentHash: in.ContentHash}
	if held, err := s.alreadyHeld(ref, in.ByteSize); err != nil {
		return Result{}, err
	} else if held {
		committed, err := s.commit(ctx, store.Upload{
			AuthorID:         in.AuthorID,
			IdempotencyKey:   in.IdempotencyKey,
			ContentHash:      in.ContentHash,
			ByteSize:         in.ByteSize,
			Retention:        in.Retention,
			OriginalFilename: in.OriginalFilename,
		}, "already held")
		if err != nil {
			return Result{}, err
		}
		return Result{Committed: committed}, nil
	}

	// Outcome 4, and it SPLITS the file-absent branch rather than replacing it.
	// With no memo row, or a memo whose audio is merely missing, the code below
	// runs exactly as it did and the client heals the gap by sending the bytes
	// — which is also the crash-recovery path CHRN-20 built. With the audio
	// PRUNED, the answer is the same as outcome 3: the memo exists, the
	// transcript that let it be pruned still exists, and nothing is transferred.
	//
	// Resurrecting instead cannot work: captured_at is immutable, so a memo
	// past its window would be re-pruned by the next sweep and the upload would
	// have bought nothing. See CHRN-22 §5.
	if pruned, err := s.ingest.AudioPrunedFor(ctx, in.AuthorID, in.ContentHash); err != nil {
		return Result{}, err
	} else if pruned {
		committed, err := s.commit(ctx, store.Upload{
			AuthorID:         in.AuthorID,
			IdempotencyKey:   in.IdempotencyKey,
			ContentHash:      in.ContentHash,
			ByteSize:         in.ByteSize,
			Retention:        in.Retention,
			OriginalFilename: in.OriginalFilename,
		}, "audio already pruned")
		if err != nil {
			return Result{}, err
		}
		s.logger.InfoContext(ctx, "re-delivery of a memo whose audio was pruned; nothing transferred",
			"memo", committed.Memo.ID)
		return Result{Committed: committed}, nil
	}

	// The cap is read here but ENFORCED after OpenUpload, and only against a
	// session that turns out to be new.
	//
	// Checked before, it refuses a RESUME — which is the one request that must
	// never be refused when an account is full of them. A phone that lost its
	// upload id (a reinstall, a crash) re-presents the key it still has; if that
	// answers 429 it cannot DELETE its way out either, because DELETE needs the
	// id it lost, and the account is stuck until the sessions expire a week
	// later. "Many stalled sessions" is exactly when resume matters most.
	open, err := s.sessions.CountOpenUploads(ctx, in.AuthorID)
	if err != nil {
		return Result{}, err
	}
	atCap := open >= s.maxOpen

	u, created, err := s.sessions.OpenUpload(ctx, store.Upload{
		AuthorID:         in.AuthorID,
		IdempotencyKey:   in.IdempotencyKey,
		ContentHash:      in.ContentHash,
		ByteSize:         in.ByteSize,
		Retention:        in.Retention,
		OriginalFilename: in.OriginalFilename,
	})
	if err != nil {
		return Result{}, err
	}

	if created && atCap {
		// A genuinely new session past the cap. Undo it: it holds no bytes yet,
		// so removing the row is the whole of the rollback. The count read
		// above is the honest one to report — it is what the account held when
		// the request arrived.
		if err := s.sessions.ClearUploadKey(ctx, in.AuthorID, in.IdempotencyKey); err != nil {
			return Result{}, err
		}
		return Result{}, fmt.Errorf("%w: %d open, limit is %d", ErrTooManyOpen, open, s.maxOpen)
	}
	if created {
		// Created here rather than at boot, and it is not the same call the
		// audio root gets. The root is an operator's path and a typo in it must
		// stop the service; this is a fixed name inside a directory that has
		// already been checked, so making it is bookkeeping rather than
		// invention.
		if err := os.MkdirAll(s.audio.StagingRoot(), 0o755); err != nil {
			return Result{}, fmt.Errorf("upload: create staging directory: %w", err)
		}
	}

	// Same lock Append and Status take, for the same reason: the branch below
	// can finalise.
	unlock := s.lock(u.ID)
	defer unlock()

	sess, done, err := s.status(u)
	if err != nil {
		return Result{}, err
	}
	if done {
		// A session whose bytes are all present but which never got as far as
		// a memo — a crash in the window between the two. Finish it rather
		// than telling the client it has nothing left to send and no memo.
		committed, err := s.finalise(ctx, u)
		if err != nil {
			return Result{}, err
		}
		return Result{Committed: committed}, nil
	}
	return Result{Session: &sess, Created: created}, nil
}

// Status reports how far a session has got, finalising it if the bytes are all
// there. Ownership is the caller's to check.
//
// It takes the session lock even though it writes nothing itself, because it can
// finalise — and a finalise moves a file and writes a memo. Two unlocked
// Statuses on one complete session both reach finalise, and the second finds the
// staging file the first has already dealt with. Read-only is a property of what
// a function does, not of the verb in its name.
func (s *Service) Status(ctx context.Context, u store.Upload) (Result, error) {
	unlock := s.lock(u.ID)
	defer unlock()

	sess, done, err := s.status(u)
	if err != nil {
		return Result{}, err
	}
	if done {
		committed, err := s.finalise(ctx, u)
		if err != nil {
			return Result{}, err
		}
		return Result{Committed: committed}, nil
	}
	return Result{Session: &sess}, nil
}

// Append writes the next chunk at offset.
//
// A mismatched offset is an *OffsetConflict carrying the server's own, which is
// how a client that lost track resumes rather than restarts.
//
// # Two kinds of failure, treated differently on purpose
//
// A transfer cut by the network KEEPS whatever landed. Those bytes are the
// right bytes — the connection died, the content did not — and discarding them
// would throw away exactly the progress this endpoint exists to preserve.
//
// A client that sends MORE than it declared has the whole chunk discarded and
// the file truncated back to where the request found it. That is not
// pedantry: a client whose byte count is wrong is a client whose bytes we have
// no reason to believe are the right ones, so the hash would reject them at the
// end anyway — after the rest of the transfer. Refusing at the point of the
// mistake costs one chunk instead of the remainder of the file.
func (s *Service) Append(ctx context.Context, u store.Upload, offset int64, body io.Reader) (Result, error) {
	unlock := s.lock(u.ID)
	defer unlock()

	path, err := s.audio.StagingPath(u.ID)
	if err != nil {
		return Result{}, err
	}
	at, done, err := s.offset(u)
	if err != nil {
		return Result{}, err
	}
	if offset != at {
		return Result{}, &OffsetConflict{Offset: at}
	}
	if done {
		// Everything is already here; an empty append at the full offset is
		// the legitimate way a client re-drives a finalise it did not see the
		// answer to.
		committed, err := s.finalise(ctx, u)
		if err != nil {
			return Result{}, err
		}
		return Result{Committed: committed}, nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return Result{}, fmt.Errorf("upload: create staging directory: %w", err)
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return Result{}, fmt.Errorf("upload: open staging file: %w", err)
	}
	defer func() { _ = f.Close() }()

	remaining := u.ByteSize - at
	written, copyErr := io.Copy(f, io.LimitReader(body, remaining))

	// Durable before the offset is reported. Without this the server can
	// promise a client it holds N bytes and hold fewer after a power cut, and
	// the client would resume from a point the file never reached — producing a
	// hole that the hash catches but only after the whole remaining transfer.
	if syncErr := f.Sync(); syncErr != nil {
		return Result{}, fmt.Errorf("upload: sync staging file: %w", syncErr)
	}

	if copyErr != nil {
		// Keep what landed and say so. The next request resumes from here.
		if touchErr := s.sessions.TouchUpload(ctx, u.ID); touchErr != nil {
			s.logger.Warn("could not record upload progress; the session may expire early",
				"upload_id", u.ID, "error", touchErr)
		}
		return Result{}, &TransferCut{Offset: at + written, Err: copyErr}
	}

	// Did the client have more to send than it declared? LimitReader stopped at
	// the boundary, so anything still readable is past it.
	//
	// This read cannot hang, and the reason is in the handler rather than here:
	// a PATCH without a Content-Length is refused, so the body reader ends at a
	// length the server knows and returns EOF rather than waiting on a client
	// that has stopped sending. The handler rejects the common case up front
	// from that same header; this is the backstop for a reader that lied.
	if written == remaining {
		var probe [1]byte
		if n, _ := io.ReadFull(body, probe[:]); n > 0 {
			if truncErr := f.Truncate(at); truncErr != nil {
				return Result{}, fmt.Errorf("upload: discarding an oversized chunk: %w", truncErr)
			}
			if syncErr := f.Sync(); syncErr != nil {
				return Result{}, fmt.Errorf("upload: sync after discarding a chunk: %w", syncErr)
			}
			return Result{}, fmt.Errorf("%w: declared %d bytes", ErrOversend, u.ByteSize)
		}
	}

	if err := s.sessions.TouchUpload(ctx, u.ID); err != nil {
		s.logger.Warn("could not record upload progress; the session may expire early",
			"upload_id", u.ID, "error", err)
	}

	if at+written >= u.ByteSize {
		committed, err := s.finalise(ctx, u)
		if err != nil {
			return Result{}, err
		}
		return Result{Committed: committed}, nil
	}
	// Expiry from the touch above rather than from the value loaded before it:
	// reporting the stale one would tell a client its session dies earlier than
	// it does, on every single chunk.
	return Result{Session: &Session{
		ID:        u.ID,
		ByteSize:  u.ByteSize,
		Offset:    at + written,
		ExpiresAt: s.now().Add(s.ttl),
	}}, nil
}

// Abandon drops a session and its bytes at the client's request.
func (s *Service) Abandon(ctx context.Context, u store.Upload) error {
	unlock := s.lock(u.ID)
	defer unlock()

	if err := s.removeStaging(u.ID); err != nil {
		return err
	}
	return s.sessions.DeleteUpload(ctx, u.ID)
}

// status is Session plus "are all the bytes here".
func (s *Service) status(u store.Upload) (Session, bool, error) {
	at, done, err := s.offset(u)
	if err != nil {
		return Session{}, false, err
	}
	return Session{
		ID:        u.ID,
		ByteSize:  u.ByteSize,
		Offset:    at,
		ExpiresAt: u.LastActivityAt.Add(s.ttl),
	}, done, nil
}

// offset reports how far this session's bytes have got, and whether they are
// all present.
//
// The staging file's size is the answer. When there is no staging file the
// answer is usually zero — but not always: a finalise that renamed the bytes
// into place and then failed before recording the memo leaves exactly this
// shape, and telling that client to send forty minutes of audio again would be
// wrong when the audio is already on the disk under it.
func (s *Service) offset(u store.Upload) (int64, bool, error) {
	path, err := s.audio.StagingPath(u.ID)
	if err != nil {
		return 0, false, err
	}
	info, err := os.Stat(path)
	switch {
	case err == nil:
		return info.Size(), info.Size() >= u.ByteSize, nil
	case !os.IsNotExist(err):
		return 0, false, fmt.Errorf("upload: stat staging file: %w", err)
	}

	held, err := s.alreadyHeld(audio.Ref{AuthorID: u.AuthorID, ContentHash: u.ContentHash}, u.ByteSize)
	if err != nil {
		return 0, false, err
	}
	if held {
		return u.ByteSize, true, nil
	}
	return 0, false, nil
}

// alreadyHeld reports whether the finished recording is on disk at the right
// size. Size, not content: re-hashing on every open would make the cheap path
// as expensive as the transfer it is avoiding, and the size disagreeing is
// itself enough to fall back to sending the bytes.
func (s *Service) alreadyHeld(ref audio.Ref, size int64) (bool, error) {
	final, err := s.audio.Path(ref)
	if err != nil {
		return false, err
	}
	info, err := os.Stat(final)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("upload: stat recording: %w", err)
	}
	return info.Size() == size, nil
}

// finalise turns a complete staging file into a memo.
//
// The write order is CHRN-19's, and for CHRN-19's reason: **file, then memo
// row, then bookkeeping.** Every crash window then fails in the recoverable
// direction — a file with no memo is an orphan, which the storage report
// detects and which a re-open collapses; the reverse order would leave a memo
// whose audio is not there.
func (s *Service) finalise(ctx context.Context, u store.Upload) (*Committed, error) {
	path, err := s.audio.StagingPath(u.ID)
	if err != nil {
		return nil, err
	}
	ref := audio.Ref{AuthorID: u.AuthorID, ContentHash: u.ContentHash}
	final, err := s.audio.Path(ref)
	if err != nil {
		return nil, err
	}

	if _, err := os.Stat(path); err == nil {
		// Hashed here rather than accumulated across requests. A session spans
		// minutes, several connections and possibly a restart, so there is no
		// hasher that survives it — and a re-read of a file the sizing puts in
		// the single-digit megabytes is cheaper than any scheme that would.
		sum, size, err := hashFile(path)
		if err != nil {
			return nil, err
		}
		if size != u.ByteSize || sum != u.ContentHash {
			// Not a memo. Destroy the session so the client starts cleanly
			// rather than resuming onto bytes that already failed.
			//
			// Neither hash is logged as a secret would be — they are not
			// secret — but the FILENAME is not logged either: it is authored
			// text (REVIEW.md 8).
			s.logger.Warn("upload did not match its declared content; discarding",
				"upload_id", u.ID, "declared_bytes", u.ByteSize, "received_bytes", size,
				"hash_matched", sum == u.ContentHash)
			if err := s.removeStaging(u.ID); err != nil {
				return nil, err
			}
			if err := s.sessions.DeleteUpload(ctx, u.ID); err != nil {
				return nil, err
			}
			return nil, ErrHashMismatch
		}
		if err := os.MkdirAll(filepath.Dir(final), 0o755); err != nil {
			return nil, fmt.Errorf("upload: create %s: %w", filepath.Dir(final), err)
		}
		// Atomic: staging is inside the audio root precisely so this is a
		// rename within one filesystem and never a copy. There is never a
		// partially written file at a recording's path.
		if err := os.Rename(path, final); err != nil {
			return nil, fmt.Errorf("upload: rename into %s: %w", final, err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("upload: stat staging file: %w", err)
	} else {
		// No staging file. offset() reaches this only for the
		// crash-between-rename-and-memo case, where the recording IS on disk —
		// but that was established by the caller, and the invariant is checked
		// again here because this is where it is depended on.
		//
		// It can be false by the time we arrive. Two overlapping requests both
		// see a complete staging file and both enter finalise; the first finds
		// a hash mismatch and removes it, and the second then stats ENOENT with
		// nothing ever renamed. Committing there would write a memo whose audio
		// is not on disk — CHRN-23's `missing`, the exact state the file-gate on
		// the already-held shortcut exists to prevent, reached by another door.
		// An Abandon landing between a Status's two stats does the same with no
		// second finalise at all.
		//
		// The lock in Status and Open closes the window; this closes the
		// invariant, and they are not the same fix. A comment asserting what a
		// caller did is not a check.
		held, err := s.alreadyHeld(ref, u.ByteSize)
		if err != nil {
			return nil, err
		}
		if !held {
			s.logger.Warn("an upload's bytes are gone before it could be recorded; "+
				"no memo was written and the client must send them again",
				"upload_id", u.ID, "declared_bytes", u.ByteSize)
			return nil, ErrStagingLost
		}
	}

	return s.commit(ctx, u, "uploaded")
}

// commit records the arrival and clears the session. Shared by finalise and by
// Open's already-held shortcut, which differ only in whether bytes moved.
func (s *Service) commit(ctx context.Context, u store.Upload, how string) (*Committed, error) {
	res, err := s.ingest.IngestMemo(ctx, store.Arrival{
		AuthorID:    u.AuthorID,
		ContentHash: u.ContentHash,
		ByteSize:    u.ByteSize,
		Source:      store.SourceUpload,
		// The key is this arrival's handle, and unlike the watcher's path it is
		// inside memo_arrivals_key — so a same-key retry writes no second row
		// and IngestMemo reports the collapse instead.
		IdempotencyKey:   u.IdempotencyKey,
		Retention:        u.Retention,
		OriginalFilename: u.OriginalFilename,
	})
	if err != nil {
		return nil, err
	}

	// Spent. Cleared after the memo exists, not before: the row is what a second
	// request in flight would find, and finding a live session for a memo that
	// already exists is harmless (it finalises again, idempotently) while
	// finding nothing would send the client back to a full transfer.
	//
	// By KEY rather than by id, because the already-held shortcut arrives here
	// with no id at all. That path is also the crash-recovery path — bytes
	// renamed into place, memo never written — and the session it is recovering
	// from is precisely the row that would otherwise sit holding this key until
	// the sweep collected it a week later.
	if err := s.sessions.ClearUploadKey(ctx, u.AuthorID, u.IdempotencyKey); err != nil {
		s.logger.Warn("could not clear a finished upload session; the sweep will",
			"upload_id", u.ID, "error", err)
	}

	// The same two lines the watcher emits, with source=upload. Driven by
	// Collapsed and never by Deliveries > 1.
	if res.Collapsed {
		s.logger.Info("duplicate arrival",
			"memo_id", res.Memo.ID,
			"source", store.SourceUpload,
			"arrival_count", res.Deliveries,
			"how", how)
	} else {
		s.logger.Info("memo captured",
			"memo_id", res.Memo.ID,
			"source", store.SourceUpload,
			"bytes", u.ByteSize)
	}

	// After the memo exists and its audio is durable, never before. Guarded on
	// DurationMS so a re-delivery of a described memo does no work — and so a
	// memo whose probe FAILED gets another attempt here, which is the whole of
	// the retry story for CHRN-21.
	//
	// The RESULT is taken, not discarded. The row this returns is the one the
	// probe just wrote, and it is what the response is built from below — an
	// earlier version described the memo and then answered from the copy it had
	// before, so a first upload reported `"duration_ms": null` for a memo whose
	// column said 3000. That is the only delivery on which the probe actually
	// runs, so it made CHRN-21 invisible over HTTP.
	memo := s.describe(ctx, res.Memo)

	// The warning that stood here — a memo whose audio was re-delivered after
	// being pruned, left as an orphan because clearing `audio_pruned_at` was
	// CHRN-22's policy to decide — is gone with the state it described. CHRN-22
	// Ruling 2 settled it: audio is delivered once, so `Open` answers such a
	// re-delivery as a duplicate and no bytes ever reach this function.
	return &Committed{Memo: memo, Collapsed: res.Collapsed, Deliveries: res.Deliveries}, nil
}

// hashFile reads a file once, returning its SHA-256 as lowercase hex and its
// length.
func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, fmt.Errorf("upload: open staging file: %w", err)
	}
	defer func() { _ = f.Close() }()

	sum := sha256.New()
	n, err := io.Copy(sum, f)
	if err != nil {
		return "", 0, fmt.Errorf("upload: hash staging file: %w", err)
	}
	return hex.EncodeToString(sum.Sum(nil)), n, nil
}

// removeStaging deletes one session's bytes. A missing file is success: the
// point is that it is gone.
func (s *Service) removeStaging(id uuid.UUID) error {
	path, err := s.audio.StagingPath(id)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("upload: remove staging file: %w", err)
	}
	return nil
}

// sessionLock is one session's mutex plus how many callers are holding or
// waiting for it. The count is what makes the map safe to prune: an entry is
// removed only when nobody can still be about to use it.
type sessionLock struct {
	mu   sync.Mutex
	refs int
}

// lock serialises work on one session and returns the release.
//
// The refcount is not incidental. Pruning the map on "is it currently
// unlocked?" looks equivalent and is not: a caller that has already read the
// mutex out of the map but has not yet locked it would have its entry deleted,
// the next caller would create a second mutex under the same id, and the two
// would then write to one file believing they were serialised. Incrementing
// under s.mu before releasing it is what closes that window.
func (s *Service) lock(id uuid.UUID) func() {
	s.mu.Lock()
	l, ok := s.locks[id]
	if !ok {
		l = &sessionLock{}
		s.locks[id] = l
	}
	l.refs++
	s.mu.Unlock()

	l.mu.Lock()
	return func() {
		l.mu.Unlock()
		// Dropped at zero so the map tracks sessions in flight rather than
		// every session this process has ever seen.
		s.mu.Lock()
		l.refs--
		if l.refs == 0 {
			delete(s.locks, id)
		}
		s.mu.Unlock()
	}
}

// Find resolves a session id for one author.
//
// Not-yours and not-there are ONE answer, and that is the point: returning
// ErrNotFound for a session belonging to somebody else means an id cannot be
// probed to learn whether it names a real upload on another account.
func (s *Service) Find(ctx context.Context, id, authorID uuid.UUID) (store.Upload, error) {
	u, err := s.sessions.GetUpload(ctx, id)
	if err != nil {
		return store.Upload{}, err
	}
	if u.AuthorID != authorID {
		return store.Upload{}, store.ErrNotFound
	}
	return u, nil
}

// describe records what the recording turned out to be (CHRN-21): duration,
// codec, sample rate, read from the Ogg/OpusHead headers rather than a decode.
//
// It never fails an upload. By the time this runs the bytes are durable and the
// memo exists; a file whose headers cannot be read is UNDESCRIBED, not broken,
// so the three columns stay NULL and a warning is logged. Refusing a recording
// because Chronicle could not parse its container would be Chronicle deciding
// somebody's memo does not count.
//
// Returns the memo to answer with: the described row on success, and the one it
// was given on every failure path. The caller must use the return value rather
// than its own copy, or the response reports NULL for columns that were just
// written.
func (s *Service) describe(ctx context.Context, m store.Memo) store.Memo {
	if m.DurationMS != nil {
		return m
	}
	path, err := s.audio.Path(audio.Ref{AuthorID: m.AuthorID, ContentHash: m.ContentHash})
	if err != nil {
		s.logger.Warn("could not derive the path of a memo's recording", "memo_id", m.ID, "error", err)
		return m
	}
	info, err := audio.Probe(path)
	if err != nil {
		// No filename — it is authored text (REVIEW.md §8). The memo id finds
		// the row and the row finds the file.
		s.logger.Warn("could not read a recording's headers; duration, codec and sample rate stay unset",
			"memo_id", m.ID, "source", store.SourceUpload, "error", err)
		return m
	}
	described, err := s.ingest.SetMemoAudioInfo(ctx, m.ID, store.AudioInfo{
		DurationMS:   info.DurationMS,
		Codec:        info.Codec,
		SampleRateHz: info.SampleRateHz,
	})
	if err != nil {
		s.logger.Warn("could not record a recording's metadata; it will be retried on the next delivery",
			"memo_id", m.ID, "error", err)
		return m
	}
	s.logger.Info("memo described",
		"memo_id", m.ID, "source", store.SourceUpload,
		"duration_ms", info.DurationMS, "codec", info.Codec, "channels", info.Channels)
	return described
}
