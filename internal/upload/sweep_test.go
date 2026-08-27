package upload

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
)

// The sweep collects abandoned partial uploads. It is not CHRN-22 and these
// tests are deliberately about that boundary as much as about the mechanics: an
// author's directory is never walked, a memo row is never read, and the only
// two things it can name are the session table and StagingDir.

func TestSweepExpiresIdleSessionsAndLeavesActiveOnes(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()

	idle := memoBytes(900)
	active := memoBytes(950)

	abandoned := r.open(t, keyA, idle, "")
	if _, err := r.svc.Append(ctx, r.session(t, abandoned.Session.ID), 0, bytes.NewReader(idle[:300])); err != nil {
		t.Fatalf("partial chunk: %v", err)
	}
	live := r.open(t, keyB, active, "")
	if _, err := r.svc.Append(ctx, r.session(t, live.Session.ID), 0, bytes.NewReader(active[:100])); err != nil {
		t.Fatalf("partial chunk: %v", err)
	}

	// The abandoned one stopped a fortnight ago; the live one is current. Both
	// are pushed back through the fake rather than by sleeping.
	r.sessions.mu.Lock()
	u := r.sessions.byID[abandoned.Session.ID]
	u.LastActivityAt = r.now.Add(-14 * 24 * time.Hour)
	r.sessions.byID[abandoned.Session.ID] = u
	u = r.sessions.byID[live.Session.ID]
	u.LastActivityAt = r.now.Add(-time.Minute)
	r.sessions.byID[live.Session.ID] = u
	r.sessions.mu.Unlock()

	res, err := r.svc.Sweep(ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if res.Expired != 1 {
		t.Fatalf("expired %d sessions, want 1", res.Expired)
	}
	if res.Bytes != 300 {
		t.Fatalf("reported %d bytes reclaimed, want 300", res.Bytes)
	}

	if _, err := r.sessions.GetUpload(ctx, abandoned.Session.ID); err == nil {
		t.Fatal("the abandoned session survived the sweep")
	}
	if _, err := r.sessions.GetUpload(ctx, live.Session.ID); err != nil {
		t.Fatalf("the live session was swept: %v", err)
	}
	// And the live session's bytes are untouched, which is the failure that
	// would actually hurt: a sweep that eats an upload in progress.
	st, err := r.svc.Status(ctx, r.session(t, live.Session.ID))
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if st.Session.Offset != 100 {
		t.Fatalf("the live session is at offset %d, want 100", st.Session.Offset)
	}
}

// A slow upload that is still making progress is never stale, however long it
// has been running. Expiry is measured from activity, not from creation.
func TestASlowButProgressingUploadIsNotSwept(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	content := memoBytes(1000)

	res := r.open(t, keyA, content, "")
	r.sessions.mu.Lock()
	u := r.sessions.byID[res.Session.ID]
	u.CreatedAt = r.now.Add(-30 * 24 * time.Hour) // opened a month ago
	u.LastActivityAt = r.now.Add(-time.Minute)    // and still going
	r.sessions.byID[res.Session.ID] = u
	r.sessions.mu.Unlock()

	swept, err := r.svc.Sweep(ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if swept.Expired != 0 {
		t.Fatalf("a month-old upload that is still progressing was expired")
	}
}

func TestSweepRemovesUnclaimedStagingFilesOnlyAfterTheGrace(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()

	if err := os.MkdirAll(r.audio.StagingRoot(), 0o755); err != nil {
		t.Fatalf("mkdir staging: %v", err)
	}
	old := filepath.Join(r.audio.StagingRoot(), uuid.NewString())
	fresh := filepath.Join(r.audio.StagingRoot(), uuid.NewString())
	for _, p := range []string{old, fresh} {
		if err := os.WriteFile(p, memoBytes(64), 0o644); err != nil {
			t.Fatalf("write %s: %v", p, err)
		}
	}
	// The fresh one is what a session opened seconds ago looks like from the
	// sweep's side, before its row is visible. It must survive.
	if err := os.Chtimes(old, r.now.Add(-2*time.Hour), r.now.Add(-2*time.Hour)); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	if err := os.Chtimes(fresh, r.now.Add(-time.Minute), r.now.Add(-time.Minute)); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	res, err := r.svc.Sweep(ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if res.Unclaimed != 1 {
		t.Fatalf("removed %d unclaimed files, want 1", res.Unclaimed)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatal("the stale unclaimed file was left behind")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("a staging file inside the grace window was deleted; " +
			"that is an upload that had only just started")
	}
}

// A staging file whose session IS live is never a candidate, whatever its age.
func TestSweepNeverTouchesAClaimedStagingFile(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	content := memoBytes(600)

	res := r.open(t, keyA, content, "")
	if _, err := r.svc.Append(ctx, r.session(t, res.Session.ID), 0, bytes.NewReader(content[:200])); err != nil {
		t.Fatalf("chunk: %v", err)
	}
	path, err := r.audio.StagingPath(res.Session.ID)
	if err != nil {
		t.Fatalf("StagingPath: %v", err)
	}
	if err := os.Chtimes(path, r.now.Add(-72*time.Hour), r.now.Add(-72*time.Hour)); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	// The row is current even though the file looks old — a client that paused
	// and resumed. Only the row decides.
	r.sessions.mu.Lock()
	u := r.sessions.byID[res.Session.ID]
	u.LastActivityAt = r.now
	r.sessions.byID[res.Session.ID] = u
	r.sessions.mu.Unlock()

	if _, err := r.svc.Sweep(ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("a claimed staging file was removed: %v", err)
	}
}

// "Delete what you do not understand" is the wrong default anywhere under the
// audio root. A file that is not named like a session is reported and left.
func TestSweepLeavesFilesItCannotName(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()

	if err := os.MkdirAll(r.audio.StagingRoot(), 0o755); err != nil {
		t.Fatalf("mkdir staging: %v", err)
	}
	odd := filepath.Join(r.audio.StagingRoot(), "notes-from-somewhere.txt")
	if err := os.WriteFile(odd, []byte("not ours"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := os.Chtimes(odd, r.now.Add(-90*24*time.Hour), r.now.Add(-90*24*time.Hour)); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	res, err := r.svc.Sweep(ctx)
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if res.Unclaimed != 0 {
		t.Fatalf("the sweep claimed %d files it could not name", res.Unclaimed)
	}
	if _, err := os.Stat(odd); err != nil {
		t.Fatalf("a file the sweep does not understand was deleted: %v", err)
	}
}

// The boundary, asserted rather than only documented: a finished recording
// under an author's directory is invisible to the sweep. Deleting one of those
// is CHRN-22's job, gated on a durable transcript, and nothing here may reach
// it by any route.
func TestSweepNeverReachesAFinishedRecording(t *testing.T) {
	r := newRig(t)
	ctx := context.Background()
	content := memoBytes(700)

	res := r.open(t, keyA, content, "")
	if _, err := r.svc.Append(ctx, r.session(t, res.Session.ID), 0, bytes.NewReader(content)); err != nil {
		t.Fatalf("upload: %v", err)
	}
	recording, err := r.audio.Path(refFor(r.author, hashOf(content)))
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	// Aged well past every threshold the sweep knows about.
	if err := os.Chtimes(recording, r.now.Add(-365*24*time.Hour), r.now.Add(-365*24*time.Hour)); err != nil {
		t.Fatalf("chtimes: %v", err)
	}

	r.now = r.now.Add(365 * 24 * time.Hour)
	if _, err := r.svc.Sweep(ctx); err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	if _, err := os.Stat(recording); err != nil {
		t.Fatalf("the sweep deleted a finished recording: %v", err)
	}
	if got, err := os.ReadFile(recording); err != nil || !bytes.Equal(got, content) {
		t.Fatal("the finished recording was modified")
	}
}
