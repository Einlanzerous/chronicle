// Package discuss is the agent reply path — CHRN-47, the ticket that makes
// E6's threading decision pay off.
//
// WHAT THIS PACKAGE DOES NOT DO IS THE INTERESTING HALF. It does not stop an
// agent replying to itself: CHRN-43's ruling 3 put that in the store as CH091,
// deliberately, because "a rule that lives only in the reply handler is a rule
// CHRN-67's MCP reply tool does not know about" — and CHRN-67 is the second
// caller by design. This package HANDLES that refusal; it does not duplicate
// it. The same is true of CH093 on a resolved thread.
//
// What it owns is the part a schema cannot express: that the trigger is
// EXPLICIT, that the rate is bounded, and that every attempt is logged.
package discuss

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/markdown"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// MentionHandle is what the Scribe answers to, written `@scribe`.
//
// It is derived from store.ScribeEmail's local part rather than from the
// display name, because a display name is something an operator can change
// through PATCH /auth/me and a trigger that stops working when somebody renames
// an account is a trigger nobody trusts. The two must stay in step — see the
// test that asserts it.
const MentionHandle = "scribe"

// ErrNotMentioned is returned when nothing asked for a reply.
//
// THIS IS THE TICKET'S FIRST SENTENCE MADE MECHANICAL: "keep the trigger
// explicit rather than ambient. An agent that replies to everything turns
// discussions into noise and burns the trust that makes the surface useful."
// An ambient agent is one that decides for itself when to speak; this one
// cannot be invoked at all unless somebody wrote its name.
var ErrNotMentioned = errors.New("discuss: nothing in that thread asked the scribe to reply")

// ErrRateLimited is returned when a thread has had its allowance of agent
// replies for the window. Transient by construction.
var ErrRateLimited = errors.New("discuss: too many agent replies in this thread")

// ErrAlreadyAnswered is returned when the last turn is already the agent's.
//
// CH091 refuses this in the store and would refuse it here too — this exists so
// the ordinary case does not reach the database as an error, and so the log
// line says "already answered" rather than reporting a guard violation for
// something entirely expected. The store remains the enforcement.
var ErrAlreadyAnswered = errors.New("discuss: the scribe already has the last word in that thread")

// replyWindow and replyBurst bound how often one thread can draw an agent
// reply.
//
// PER THREAD, not globally and not per agent. The failure the ticket names is
// "an agent in a reply loop with itself", which is a property of one
// conversation running away — a global limit would let one thread starve every
// other, and a per-agent limit is meaningless while there is one agent.
//
// The numbers are deliberately loose. CH091 already makes the loop structurally
// impossible, so this is not the defence; it is a bound on cost and noise if
// some future caller retries in a tight loop, and three replies to one thread
// in a minute is already more conversation than a person can read.
const (
	replyWindow = time.Minute
	replyBurst  = 3
)

// Store is the slice of the store this package uses.
type Store interface {
	Scribe(ctx context.Context) (store.User, error)
	Turns(ctx context.Context, discussionID uuid.UUID) ([]store.DiscussionTurn, error)
	AppendTurn(ctx context.Context, in store.NewTurn) (store.DiscussionTurn, error)
}

// Replier posts agent replies into discussions.
type Replier struct {
	store   Store
	logger  *slog.Logger
	limiter *limiter
}

// New builds a Replier. The logger is required — "rate-limit and log it" is
// half of this ticket's body, and a nil logger would make the second half
// silently optional.
func New(s Store, logger *slog.Logger) *Replier {
	return &Replier{
		store:   s,
		logger:  logger,
		limiter: newLimiter(replyWindow, replyBurst),
	}
}

// Reply posts body into the thread as the Scribe.
//
// THE BODY IS THE CALLER'S. Generating what the Scribe says is not this
// ticket's — the description routes agents through "E10's MCP write tools",
// and CHRN-67 owns that surface. What this owns is whether the reply is
// allowed to happen at all, and that it is attributed and recorded correctly.
//
// THE ORDER OF THE CHECKS IS THE POINT. The trigger is tested first, before the
// rate limiter, so that a thread nobody mentioned the Scribe in never consumes
// allowance — otherwise a caller looping on an untriggered thread would exhaust
// the budget of a thread that had legitimately asked.
func (r *Replier) Reply(ctx context.Context, discussionID uuid.UUID, body string) (store.DiscussionTurn, error) {
	if strings.TrimSpace(body) == "" {
		// 0015's discussion_turns_body_not_blank would refuse this anyway. It
		// is caught here so the caller is told which of its arguments was
		// wrong rather than being handed a constraint name.
		return store.DiscussionTurn{}, fmt.Errorf("%w: an agent reply needs a body", store.ErrInvalidInput)
	}

	scribe, err := r.store.Scribe(ctx)
	if err != nil {
		// Includes ErrNotAnAgent, which is CHRN-44's guard against the state
		// where scribe@localhost belongs to a person. Refusing here is what
		// makes boot's "the discussion surface degrades to human-only" true.
		return store.DiscussionTurn{}, fmt.Errorf("discuss: reply: %w", err)
	}

	turns, err := r.store.Turns(ctx, discussionID)
	if err != nil {
		return store.DiscussionTurn{}, fmt.Errorf("discuss: reply: %w", err)
	}
	if len(turns) == 0 {
		return store.DiscussionTurn{}, store.ErrNotFound
	}
	last := turns[len(turns)-1]

	// ALREADY ANSWERED, checked before the mention: a thread whose last turn is
	// the Scribe's is one it has already replied to, whatever the turn before
	// said. Without this, a caller re-invoked on an unchanged thread would read
	// the person's mention again and try to answer twice.
	if last.ByAgent() {
		r.refused(ctx, discussionID, scribe, "already answered", last.Seq)
		return store.DiscussionTurn{}, fmt.Errorf("%w: seq %d", ErrAlreadyAnswered, last.Seq)
	}

	// THE TRIGGER. Only the last turn is read: a mention four turns ago was
	// answered then, or was deliberately not, and re-reading the whole thread
	// would make every later turn re-trigger a reply to an old question.
	if !mentionsScribe(last.Body) {
		r.refused(ctx, discussionID, scribe, "not mentioned", last.Seq)
		return store.DiscussionTurn{}, fmt.Errorf("%w: seq %d", ErrNotMentioned, last.Seq)
	}

	if !r.limiter.allow(discussionID.String()) {
		r.refused(ctx, discussionID, scribe, "rate limited", last.Seq)
		return store.DiscussionTurn{}, fmt.Errorf("%w: at most %d per %s",
			ErrRateLimited, replyBurst, replyWindow)
	}

	turn, err := r.store.AppendTurn(ctx, store.NewTurn{
		DiscussionID: discussionID,
		AuthorID:     scribe.ID,
		Body:         body,
	})
	if err != nil {
		// CH091 and CH093 arrive here as store sentinels. They are REPORTED,
		// not prevented: the store is the enforcement, and CHRN-43's plan
		// specifically warns that an agent which began composing before a
		// thread resolved will arrive after it did.
		r.refused(ctx, discussionID, scribe, refusalReason(err), last.Seq)
		return store.DiscussionTurn{}, fmt.Errorf("discuss: reply: %w", err)
	}

	r.logger.Info("scribe replied to a discussion",
		"discussion_id", discussionID, "seq", turn.Seq,
		"author_id", scribe.ID, "author_kind", turn.AuthorKind,
		"in_reply_to_seq", last.Seq, "body_bytes", len(body))
	return turn, nil
}

// refused logs an attempt that did not produce a turn.
//
// AT INFO AND NOT AT WARN. Most of these are the system working: a thread
// nobody mentioned the Scribe in is the overwhelmingly common case, and a
// warning for the normal path is a warning nobody reads. The one that would
// matter — an agent turn refused by CH091 — cannot happen often enough to be
// noise, because the store makes it impossible to happen at all.
func (r *Replier) refused(_ context.Context, discussionID uuid.UUID, scribe store.User, reason string, lastSeq int) {
	r.logger.Info("scribe did not reply",
		"discussion_id", discussionID, "reason", reason,
		"author_id", scribe.ID, "last_seq", lastSeq)
}

// refusalReason names a store refusal for the log, without leaking a SQLSTATE
// into an operator's line.
func refusalReason(err error) string {
	switch {
	case errors.Is(err, store.ErrAgentMayNotFollowAgent):
		return "would follow another agent turn (CH091)"
	case errors.Is(err, store.ErrDiscussionResolved):
		return "thread resolved while composing (CH093)"
	case errors.Is(err, store.ErrLinkLocked):
		return "thread busy"
	default:
		return "store refused"
	}
}

// mentionsScribe reports whether body asks the Scribe for a reply.
//
// markdown.Mentions skips code, so a person explaining the feature — "it
// replies when you write `@scribe`" — does not summon it by describing it.
func mentionsScribe(body string) bool {
	for _, h := range markdown.Mentions([]byte(body)) {
		if h == MentionHandle {
			return true
		}
	}
	return false
}

// limiter is a fixed-window per-key limiter.
//
// NOT internal/api's, deliberately. That one keys on a client IP and carries
// the trusted-proxy reasoning CHRN-75 fixed; it belongs to the HTTP surface and
// is unexported there. This keys on a discussion id, has no notion of a
// request, and is reachable from CHRN-67's MCP tools which never see an IP.
// Sharing them would mean exporting an HTTP concept to a caller that has none.
type limiter struct {
	mu     sync.Mutex
	window time.Duration
	burst  int
	hits   map[string]*bucket
	now    func() time.Time // injectable so tests do not sleep
}

type bucket struct {
	count   int
	resetAt time.Time
}

// maxKeys bounds the map, on internal/api's reasoning: a limiter that grows
// without bound is itself the exhaustion it exists to prevent. Threads are
// few, so this is never reached in practice.
const maxKeys = 1024

func newLimiter(window time.Duration, burst int) *limiter {
	return &limiter{window: window, burst: burst, hits: map[string]*bucket{}, now: time.Now}
}

func (l *limiter) allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()

	b, ok := l.hits[key]
	if !ok || now.After(b.resetAt) {
		if len(l.hits) >= maxKeys {
			l.evictLocked(now)
		}
		l.hits[key] = &bucket{count: 1, resetAt: now.Add(l.window)}
		return true
	}
	if b.count >= l.burst {
		return false
	}
	b.count++
	return true
}

// evictLocked drops the window closest to expiring, and any already expired.
func (l *limiter) evictLocked(now time.Time) {
	var oldestKey string
	var oldest time.Time
	for k, b := range l.hits {
		if now.After(b.resetAt) {
			delete(l.hits, k)
			continue
		}
		if oldestKey == "" || b.resetAt.Before(oldest) {
			oldestKey, oldest = k, b.resetAt
		}
	}
	if len(l.hits) >= maxKeys && oldestKey != "" {
		delete(l.hits, oldestKey)
	}
}
