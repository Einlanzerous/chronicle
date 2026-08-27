package store

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// tier1.memo_uploads (CHRN-20). These need a real Postgres and skip without
// one, like every other database test here.

const (
	uploadKeyA = "idem-key-aaaaaaaaaaaa"
	uploadKeyB = "idem-key-bbbbbbbbbbbb"
)

func newUpload(author uuid.UUID, key, content string, size int64) Upload {
	return Upload{
		AuthorID:         author,
		IdempotencyKey:   key,
		ContentHash:      hashOf(content),
		ByteSize:         size,
		OriginalFilename: "memo.opus",
	}
}

// The unique index on (author_id, idempotency_key) is what makes re-presenting
// a key a RESUME. Without it a phone that lost its upload id would start a
// second session beside the first and the bytes already sent would be stranded.
func TestOpenUploadResumesOnTheSameKey(t *testing.T) {
	s, ctx := newTestStore(t)
	author := newAuthor(t, s, ctx, "uploader@example.com")

	first, created, err := s.OpenUpload(ctx, newUpload(author, uploadKeyA, "hello", 5))
	if err != nil {
		t.Fatalf("OpenUpload: %v", err)
	}
	if !created {
		t.Fatal("the first open did not report as created")
	}

	again, created, err := s.OpenUpload(ctx, newUpload(author, uploadKeyA, "hello", 5))
	if err != nil {
		t.Fatalf("OpenUpload (resume): %v", err)
	}
	if created {
		t.Fatal("re-presenting a key created a second session")
	}
	if again.ID != first.ID {
		t.Fatalf("got session %s, want %s", again.ID, first.ID)
	}

	n, err := s.CountOpenUploads(ctx, author)
	if err != nil {
		t.Fatalf("CountOpenUploads: %v", err)
	}
	if n != 1 {
		t.Fatalf("%d open sessions, want 1", n)
	}
}

// A key presented with a different declaration is refused at the first request
// rather than resolved into a resume that would fail its hash check a whole
// transfer later.
func TestOpenUploadRefusesAKeyReusedForDifferentContent(t *testing.T) {
	s, ctx := newTestStore(t)
	author := newAuthor(t, s, ctx, "uploader@example.com")

	if _, _, err := s.OpenUpload(ctx, newUpload(author, uploadKeyA, "hello", 5)); err != nil {
		t.Fatalf("OpenUpload: %v", err)
	}

	_, _, err := s.OpenUpload(ctx, newUpload(author, uploadKeyA, "different", 9))
	if !errors.Is(err, ErrUploadKeyReused) {
		t.Fatalf("want ErrUploadKeyReused, got %v", err)
	}
	// The same bytes at a different length is the subtler half, and is caught
	// too: a client that recomputed its size is a client whose resume would
	// finalise short.
	_, _, err = s.OpenUpload(ctx, newUpload(author, uploadKeyA, "hello", 6))
	if !errors.Is(err, ErrUploadKeyReused) {
		t.Fatalf("a changed byte_size was accepted as a resume: %v", err)
	}
}

// Author-scoped, for the reason memo_arrivals_key is: a global unique on a
// client-chosen string lets one account's key deny another account an upload,
// through a namespace the two share for no reason.
func TestUploadKeysAreScopedToTheirAuthor(t *testing.T) {
	s, ctx := newTestStore(t)
	one := newAuthor(t, s, ctx, "one@example.com")
	two := newAuthor(t, s, ctx, "two@example.com")

	a, _, err := s.OpenUpload(ctx, newUpload(one, uploadKeyA, "hello", 5))
	if err != nil {
		t.Fatalf("first author: %v", err)
	}
	b, created, err := s.OpenUpload(ctx, newUpload(two, uploadKeyA, "goodbye", 7))
	if err != nil {
		t.Fatalf("second author with the same key: %v", err)
	}
	if !created || a.ID == b.ID {
		t.Fatal("two accounts using the same key collided into one session")
	}
}

// Retention is carried, and empty stays empty. It must NOT default to days_30
// here: CHRN-18's ratchet treats "no opinion" and "the default" differently,
// and a session that turns one into the other would let a re-upload undo a pin.
func TestUploadCarriesRetentionAndDistinguishesNoOpinion(t *testing.T) {
	s, ctx := newTestStore(t)
	author := newAuthor(t, s, ctx, "uploader@example.com")

	silent, _, err := s.OpenUpload(ctx, newUpload(author, uploadKeyA, "quiet", 5))
	if err != nil {
		t.Fatalf("OpenUpload: %v", err)
	}
	if silent.Retention != "" {
		t.Fatalf("an unstated retention came back as %q, not empty", silent.Retention)
	}

	pinned := newUpload(author, uploadKeyB, "pinned", 6)
	pinned.Retention = RetentionForever
	got, _, err := s.OpenUpload(ctx, pinned)
	if err != nil {
		t.Fatalf("OpenUpload: %v", err)
	}
	if got.Retention != RetentionForever {
		t.Fatalf("retention %q, want %q", got.Retention, RetentionForever)
	}

	// A resume does not redeclare. The stored choice stands, and the ratchet at
	// ingest is where the level is actually decided.
	lowered := newUpload(author, uploadKeyB, "pinned", 6)
	lowered.Retention = RetentionDays30
	back, _, err := s.OpenUpload(ctx, lowered)
	if err != nil {
		t.Fatalf("OpenUpload (resume): %v", err)
	}
	if back.Retention != RetentionForever {
		t.Fatalf("a resume lowered retention to %q", back.Retention)
	}
}

// Expiry runs from last activity, not from creation, so a slow upload that is
// still progressing is never stale.
func TestStaleUploadsMeasureIdlenessNotAge(t *testing.T) {
	s, ctx := newTestStore(t)
	author := newAuthor(t, s, ctx, "uploader@example.com")

	idle, _, err := s.OpenUpload(ctx, newUpload(author, uploadKeyA, "idle", 4))
	if err != nil {
		t.Fatalf("OpenUpload: %v", err)
	}
	slow, _, err := s.OpenUpload(ctx, newUpload(author, uploadKeyB, "slow", 4))
	if err != nil {
		t.Fatalf("OpenUpload: %v", err)
	}

	// Both were opened a month ago. Only one has done anything since.
	if _, err := s.pool.Exec(ctx,
		`UPDATE tier1.memo_uploads SET created_at = now() - interval '30 days',
		                               last_activity_at = now() - interval '30 days'`); err != nil {
		t.Fatalf("age the sessions: %v", err)
	}
	if err := s.TouchUpload(ctx, slow.ID); err != nil {
		t.Fatalf("TouchUpload: %v", err)
	}

	stale, err := s.StaleUploads(ctx, time.Now().Add(-7*24*time.Hour))
	if err != nil {
		t.Fatalf("StaleUploads: %v", err)
	}
	if len(stale) != 1 {
		t.Fatalf("%d stale sessions, want 1", len(stale))
	}
	if stale[0].ID != idle.ID {
		t.Fatalf("the wrong session is stale: got %s, want %s", stale[0].ID, idle.ID)
	}
}

func TestLiveUploadIDsAndDelete(t *testing.T) {
	s, ctx := newTestStore(t)
	author := newAuthor(t, s, ctx, "uploader@example.com")

	one, _, err := s.OpenUpload(ctx, newUpload(author, uploadKeyA, "one", 3))
	if err != nil {
		t.Fatalf("OpenUpload: %v", err)
	}
	two, _, err := s.OpenUpload(ctx, newUpload(author, uploadKeyB, "two", 3))
	if err != nil {
		t.Fatalf("OpenUpload: %v", err)
	}

	live, err := s.LiveUploadIDs(ctx)
	if err != nil {
		t.Fatalf("LiveUploadIDs: %v", err)
	}
	if len(live) != 2 {
		t.Fatalf("%d live ids, want 2", len(live))
	}

	if err := s.DeleteUpload(ctx, one.ID); err != nil {
		t.Fatalf("DeleteUpload: %v", err)
	}
	if _, err := s.GetUpload(ctx, one.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("a deleted session still resolves: %v", err)
	}
	if _, err := s.GetUpload(ctx, two.ID); err != nil {
		t.Fatalf("the other session was deleted too: %v", err)
	}

	// Deleting frees the key, so a client can start again cleanly after
	// abandoning — which is the remedy offered for a hash mismatch.
	if _, created, err := s.OpenUpload(ctx, newUpload(author, uploadKeyA, "one", 3)); err != nil || !created {
		t.Fatalf("reopening a released key: created=%v err=%v", created, err)
	}
}

// The declaration is validated before it reaches the column CHECKs, so a bad
// one is a caller's mistake with a message rather than a wrapped constraint
// violation the handler would answer 500 to.
func TestOpenUploadRejectsBadDeclarations(t *testing.T) {
	s, ctx := newTestStore(t)
	author := newAuthor(t, s, ctx, "uploader@example.com")

	bad := map[string]Upload{
		"no author":    {IdempotencyKey: uploadKeyA, ContentHash: hashOf("x"), ByteSize: 1},
		"short key":    {AuthorID: author, IdempotencyKey: "tooshort", ContentHash: hashOf("x"), ByteSize: 1},
		"bad hash":     {AuthorID: author, IdempotencyKey: uploadKeyA, ContentHash: "nope", ByteSize: 1},
		"zero size":    {AuthorID: author, IdempotencyKey: uploadKeyA, ContentHash: hashOf("x"), ByteSize: 0},
		"bad reten...": {AuthorID: author, IdempotencyKey: uploadKeyA, ContentHash: hashOf("x"), ByteSize: 1, Retention: "someday"},
	}
	for name, in := range bad {
		t.Run(name, func(t *testing.T) {
			if _, _, err := s.OpenUpload(ctx, in); !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("want ErrInvalidInput, got %v", err)
			}
		})
	}
}

// 0005 declares author_id with no foreign key into tier2.users, because a
// tier-1 table referencing tier 2 would be the cross-schema path the doctrine
// forbids (0004 established this). Asserted rather than trusted to review: the
// absence of a constraint is exactly the kind of thing a later "helpful" edit
// adds back.
func TestMemoUploadsHoldsNoForeignKeyIntoTier2(t *testing.T) {
	s, ctx := newTestStore(t)

	var refs []string
	rows, err := s.pool.Query(ctx, `
		SELECT confrelid::regclass::text
		  FROM pg_constraint
		 WHERE conrelid = 'tier1.memo_uploads'::regclass AND contype = 'f'`)
	if err != nil {
		t.Fatalf("read constraints: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var target string
		if err := rows.Scan(&target); err != nil {
			t.Fatalf("scan: %v", err)
		}
		refs = append(refs, target)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("read constraints: %v", err)
	}
	if len(refs) != 0 {
		t.Fatalf("tier1.memo_uploads references %v; a tier-1 table must hold no foreign key at all here", refs)
	}
}

// The other direction of the same rule: an upload session must be deletable
// without touching tier 2, and an account with a session open must still be
// removable through the tier-2 path alone. A cascade in either direction would
// be a write path crossing the boundary.
func TestDeletingAnAccountDoesNotCascadeIntoUploads(t *testing.T) {
	s, ctx := newTestStore(t)
	author := newAuthor(t, s, ctx, "leaving@example.com")

	u, _, err := s.OpenUpload(ctx, newUpload(author, uploadKeyA, "bytes", 5))
	if err != nil {
		t.Fatalf("OpenUpload: %v", err)
	}
	if err := s.DeleteUser(ctx, author); err != nil {
		t.Fatalf("DeleteUser: %v", err)
	}
	// The session outlives the account rather than cascading. It is unreachable
	// — no session can authenticate as that account any more — and the sweep
	// collects it, which is the whole cost of the tier rule here.
	got, err := s.GetUpload(ctx, u.ID)
	if err != nil {
		t.Fatalf("the session was cascaded away by a tier-2 delete: %v", err)
	}
	if got.AuthorID != author {
		t.Fatalf("author_id changed to %s", got.AuthorID)
	}

	stale, err := s.StaleUploads(ctx, time.Now().Add(time.Hour))
	if err != nil {
		t.Fatalf("StaleUploads: %v", err)
	}
	if len(stale) != 1 {
		t.Fatalf("the orphaned session is not collectable: %d stale", len(stale))
	}
}

// ClearUploadKey is how a finished upload releases its key, and it has to work
// without an id: the already-held shortcut answers before any session is looked
// up, and the row it clears is one an earlier crashed attempt left behind.
func TestClearUploadKeyReleasesTheKeyWithoutAnID(t *testing.T) {
	s, ctx := newTestStore(t)
	author := newAuthor(t, s, ctx, "uploader@example.com")
	other := newAuthor(t, s, ctx, "bystander@example.com")

	mine, _, err := s.OpenUpload(ctx, newUpload(author, uploadKeyA, "mine", 4))
	if err != nil {
		t.Fatalf("OpenUpload: %v", err)
	}
	// Same key on another account, which must be untouched — the delete is
	// author-scoped for the same reason the index is.
	theirs, _, err := s.OpenUpload(ctx, newUpload(other, uploadKeyA, "theirs", 6))
	if err != nil {
		t.Fatalf("OpenUpload: %v", err)
	}

	if err := s.ClearUploadKey(ctx, author, uploadKeyA); err != nil {
		t.Fatalf("ClearUploadKey: %v", err)
	}
	if _, err := s.GetUpload(ctx, mine.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the session survived: %v", err)
	}
	if _, err := s.GetUpload(ctx, theirs.ID); err != nil {
		t.Fatalf("another account's session was cleared: %v", err)
	}
	// Clearing a key nothing holds is not an error: the point is that it is free.
	if err := s.ClearUploadKey(ctx, author, uploadKeyA); err != nil {
		t.Fatalf("clearing an already-free key: %v", err)
	}
	if _, created, err := s.OpenUpload(ctx, newUpload(author, uploadKeyA, "again", 5)); err != nil || !created {
		t.Fatalf("the key was not released: created=%v err=%v", created, err)
	}
}
