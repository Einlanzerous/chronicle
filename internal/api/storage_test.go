package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/audio"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// fakeCorpus stands in for the database half of the storage report, so the
// interesting cases — a file the database does not expect, and a file the
// database expects and cannot find — are testable without Postgres.
type fakeCorpus struct {
	inv   []store.AudioRef
	stats store.CorpusStats
}

func (f *fakeCorpus) AudioInventory(context.Context) ([]store.AudioRef, error) {
	return f.inv, nil
}

func (f *fakeCorpus) CorpusStats(context.Context, time.Duration) (store.CorpusStats, error) {
	return f.stats, nil
}

const testHash = "abcdef0123456789abcdef0123456789abcdef0123456789abcdef0123456789"

func storageRouter(t *testing.T, root string, c Corpus) (http.Handler, *fakeAccounts, store.User) {
	t.Helper()
	f := newFakeAccounts()
	owner := person("owner@example.com", true)
	f.byEmail[owner.Email] = owner
	f.sessions["chr_owner"] = owner

	d := Deps{
		DB: fakePinger{}, Accounts: f, Logger: discardLogger(),
		Version: "test", SecureCookies: true, Corpus: c,
	}
	if root != "" {
		s, err := audio.New(root)
		if err != nil {
			t.Fatalf("audio.New: %v", err)
		}
		d.Audio = s
	}
	return NewRouter(d), f, owner
}

func getStorage(t *testing.T, h http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/admin/storage", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "chr_owner"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// "Not configured" and "no audio yet" are different facts. Reporting a corpus
// of zero for the first would be a report that looks reassuring and is not.
func TestStorageReportSaysWhenItIsNotConfigured(t *testing.T) {
	h, _, _ := storageRouter(t, "", &fakeCorpus{})
	rec := getStorage(t, h)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "CHRONICLE_AUDIO_DIR") {
		t.Errorf("body = %q, want it to name the variable to set", body)
	}
}

func TestStorageReportIsOwnerOnly(t *testing.T) {
	h, f, _ := storageRouter(t, t.TempDir(), &fakeCorpus{})
	f.sessions["chr_member"] = person("member@example.com", false)

	req := httptest.NewRequest(http.MethodGet, "/admin/storage", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "chr_member"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("member got %d, want 403", rec.Code)
	}

	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/storage", nil))
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous got %d, want 401", rec.Code)
	}
}

func TestStorageReportReconcilesDiskAgainstTheDatabase(t *testing.T) {
	root := t.TempDir()
	author := uuid.New()

	// On disk and expected — agreement.
	writeRecording(t, root, author, testHash, 100)
	// On disk, expected by nobody — an orphan, which is what a prune whose
	// unlink did not happen looks like.
	orphanHash := "1111111111111111111111111111111111111111111111111111111111111111"
	writeRecording(t, root, author, orphanHash, 64)
	// Expected and absent — the direction that matters.
	missingHash := "2222222222222222222222222222222222222222222222222222222222222222"
	// Something this layout did not write.
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), make([]byte, 9), 0o644); err != nil {
		t.Fatal(err)
	}

	c := &fakeCorpus{
		inv: []store.AudioRef{
			{AuthorID: author, ContentHash: testHash, ByteSize: 100},
			{AuthorID: author, ContentHash: missingHash, ByteSize: 4096},
		},
		stats: store.CorpusStats{
			Memos: 3, AudioPresent: 2, AudioPruned: 1,
			RecordedBytes: 4196, EverBytes: 4260,
			WindowMemos: 2, WindowBytes: 4196,
		},
	}
	h, _, _ := storageRouter(t, root, c)

	rec := getStorage(t, h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var got storageReport
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.Disk.Files != 2 || got.Disk.Bytes != 164 {
		t.Errorf("disk = %d files / %d bytes, want 2 / 164", got.Disk.Files, got.Disk.Bytes)
	}
	if got.Disk.Strays != 1 || got.Disk.StrayBytes != 9 {
		t.Errorf("strays = %d / %d bytes, want 1 / 9", got.Disk.Strays, got.Disk.StrayBytes)
	}
	if got.Reconciliation.Orphans != 1 || got.Reconciliation.OrphanBytes != 64 {
		t.Errorf("orphans = %d / %d bytes, want 1 / 64",
			got.Reconciliation.Orphans, got.Reconciliation.OrphanBytes)
	}
	if got.Reconciliation.Missing != 1 || got.Reconciliation.MissingBytes != 4096 {
		t.Errorf("missing = %d / %d bytes, want 1 / 4096",
			got.Reconciliation.Missing, got.Reconciliation.MissingBytes)
	}
	// The sample names the ref so somebody can go and look, and it is the
	// author/hash pair the path is derived from.
	if len(got.Reconciliation.MissingSample) != 1 ||
		got.Reconciliation.MissingSample[0] != author.String()+"/"+missingHash {
		t.Errorf("missing sample = %v, want the author/hash of the absent file",
			got.Reconciliation.MissingSample)
	}
	// A stray is not an orphan. Offering it as one would invite deleting a
	// file nobody understands.
	for _, s := range got.Reconciliation.OrphanSample {
		if s == "notes.txt" {
			t.Error("a stray was reported as an orphan")
		}
	}

	if got.Corpus.Memos != 3 || got.Corpus.AudioPruned != 1 {
		t.Errorf("corpus = %+v, want the stats the store reported", got.Corpus)
	}
	if got.Window.Days != 30 || got.Window.ProjectedBytes != audio.ProjectedWindowBytes {
		t.Errorf("window = %+v, want 30 days against the 340 MB projection", got.Window)
	}
	if got.Window.Bytes != 4196 {
		t.Errorf("window bytes = %d, want 4196", got.Window.Bytes)
	}
	// The measured figure against the projection is the whole point of the
	// window block: a wrong prediction should show as a number.
	if got.Window.PctOfProjected <= 0 {
		t.Errorf("pct_of_projected = %v, want the measured share of the projection",
			got.Window.PctOfProjected)
	}
	if got.Root != root {
		t.Errorf("root = %q, want %q", got.Root, root)
	}
}

// A fresh install: nothing on disk, nothing in the database, and a report
// rather than an error. This is the state until CHRN-19 or CHRN-20 lands.
func TestStorageReportOnAnEmptyCorpus(t *testing.T) {
	h, _, _ := storageRouter(t, t.TempDir(), &fakeCorpus{})
	rec := getStorage(t, h)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body)
	}
	var got storageReport
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Disk.Files != 0 || got.Reconciliation.Orphans != 0 || got.Reconciliation.Missing != 0 {
		t.Errorf("empty corpus reported %+v", got)
	}
}

func writeRecording(t *testing.T, root string, author uuid.UUID, hash string, n int) {
	t.Helper()
	rel, err := audio.RelPath(audio.Ref{AuthorID: author, ContentHash: hash})
	if err != nil {
		t.Fatalf("RelPath: %v", err)
	}
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
}
