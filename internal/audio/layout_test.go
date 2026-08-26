package audio

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"
)

const (
	hashA = "0000000000000000000000000000000000000000000000000000000000000001"
	hashB = "ffeeddccbbaa99887766554433221100ffeeddccbbaa99887766554433221100"
)

func mustUUID(t *testing.T, s string) uuid.UUID {
	t.Helper()
	u, err := uuid.Parse(s)
	if err != nil {
		t.Fatalf("parse uuid: %v", err)
	}
	return u
}

func TestRelPathIsDerivableAndStable(t *testing.T) {
	author := mustUUID(t, "11111111-2222-3333-4444-555555555555")

	got, err := RelPath(Ref{AuthorID: author, ContentHash: hashB})
	if err != nil {
		t.Fatalf("RelPath: %v", err)
	}
	want := filepath.Join("11111111-2222-3333-4444-555555555555", "ff", hashB)
	if got != want {
		t.Errorf("RelPath = %q, want %q", got, want)
	}

	// The whole point of the layout: two immutable columns in, one path out,
	// every time. Nothing about state, retention, filename or codec is in
	// reach of it.
	again, _ := RelPath(Ref{AuthorID: author, ContentHash: hashB})
	if again != got {
		t.Errorf("RelPath is not a function: %q then %q", got, again)
	}
}

// The reason the author is in the path at all. memos_author_content is UNIQUE
// on (author_id, content_hash), so two authors holding identical bytes are two
// memos by design -- and under a hash-only layout they would share one file,
// making CHRN-22's unlink a refcount problem. It is not.
func TestSameBytesUnderTwoAuthorsAreTwoFiles(t *testing.T) {
	a := mustUUID(t, "11111111-2222-3333-4444-555555555555")
	b := mustUUID(t, "99999999-8888-7777-6666-555555555555")

	pa, err := RelPath(Ref{AuthorID: a, ContentHash: hashA})
	if err != nil {
		t.Fatal(err)
	}
	pb, err := RelPath(Ref{AuthorID: b, ContentHash: hashA})
	if err != nil {
		t.Fatal(err)
	}
	if pa == pb {
		t.Fatalf("identical bytes under two authors share a path (%q): pruning either would need a refcount", pa)
	}
}

func TestRelPathRefusesWhatCannotBeAPath(t *testing.T) {
	author := mustUUID(t, "11111111-2222-3333-4444-555555555555")

	for name, ref := range map[string]Ref{
		"no author":     {ContentHash: hashA},
		"empty hash":    {AuthorID: author},
		"short hash":    {AuthorID: author, ContentHash: "abc"},
		"uppercase":     {AuthorID: author, ContentHash: strings.ToUpper(hashB)},
		"not hex":       {AuthorID: author, ContentHash: strings.Repeat("g", 64)},
		"traversal":     {AuthorID: author, ContentHash: "../../../etc/passwd"},
		"hash with sep": {AuthorID: author, ContentHash: hashA[:32] + "/" + hashA[33:]},
	} {
		t.Run(name, func(t *testing.T) {
			if p, err := RelPath(ref); err == nil {
				t.Errorf("RelPath accepted %q and produced %q", ref.ContentHash, p)
			}
		})
	}
}

func TestNewRefusesRelativeRoots(t *testing.T) {
	if _, err := New("relative/path"); err == nil {
		t.Error("New accepted a relative root; the corpus would move with the working directory")
	}
	if _, err := New(""); err == nil {
		t.Error("New accepted an empty root")
	}
	s, err := New("/data/chronicle/audio/")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if s.Root() != "/data/chronicle/audio" {
		t.Errorf("Root = %q, want the cleaned path", s.Root())
	}
}

func TestRefFromRelRoundTrips(t *testing.T) {
	want := Ref{AuthorID: mustUUID(t, "11111111-2222-3333-4444-555555555555"), ContentHash: hashB}
	rel, err := RelPath(want)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := refFromRel(rel)
	if !ok || got != want {
		t.Fatalf("refFromRel(%q) = %+v, %v; want %+v, true", rel, got, ok, want)
	}
}

func TestRefFromRelRejectsAnythingElse(t *testing.T) {
	author := "11111111-2222-3333-4444-555555555555"
	for name, rel := range map[string]string{
		"loose file":          "notes.txt",
		"flat hash":           hashA,
		"wrong bucket":        filepath.Join(author, "aa", hashA),
		"missing author":      filepath.Join("ff", hashB),
		"author not a uuid":   filepath.Join("someone", "ff", hashB),
		"extra depth":         filepath.Join(author, "ff", "ff", hashB),
		"extension appended":  filepath.Join(author, "ff", hashB+".opus"),
		"nil author":          filepath.Join(uuid.Nil.String(), "00", hashA),
		"noncanonical author": filepath.Join(strings.ReplaceAll(author, "-", ""), "ff", hashB),
	} {
		t.Run(name, func(t *testing.T) {
			if ref, ok := refFromRel(rel); ok {
				t.Errorf("refFromRel(%q) claimed %+v", rel, ref)
			}
		})
	}
}
