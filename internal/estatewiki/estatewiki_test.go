package estatewiki

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A corpus shaped the way construct-server's generator writes one: front
// matter with a quoted title, nested directories, the renderer's own
// `.vitepress` beside the pages, a non-markdown asset, and the SERV-189 stamp.
func corpus(t *testing.T) *Corpus {
	t.Helper()
	root := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		abs := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("index.md", "---\ntitle: \"The Imperial Construct\"\n---\n# The Imperial Construct\n\nA home.\n")
	write("versions.md", "---\ntitle: \"Deployed versions\"\n---\n::: info GENERATED PAGE\n:::\n")
	write("services/chronicle.md", "# chronicle — voice notes\n\nBody.\n")
	write("services/untitled.md", "just prose\n")
	write("public/architecture/chronicle.html", "<html></html>")
	write(".vitepress/config.ts", "export default {}")
	write(".vitepress/sidebar.json", "[]")
	write("build.json", `{"ref":"abc1234","generated_at":"2026-09-14T12:00:00Z"}`)
	c, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestNewRefusesAMissingOrNonDirectoryRoot(t *testing.T) {
	if _, err := New(filepath.Join(t.TempDir(), "absent")); err == nil {
		t.Fatal("a missing root was accepted")
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(file); err == nil {
		t.Fatal("a regular file was accepted as the corpus root")
	}
}

func TestAnEmptyMountIsACorpusWithNoPages(t *testing.T) {
	c, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	pages, err := c.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 0 {
		t.Fatalf("pages = %v, want none", pages)
	}
	if _, ok, err := c.Build(); err != nil || ok {
		t.Fatalf("build = ok %v err %v, want absent and no error", ok, err)
	}
}

func TestListWalksPagesAndNothingElse(t *testing.T) {
	pages, err := corpus(t).List()
	if err != nil {
		t.Fatal(err)
	}
	want := []Summary{
		{Path: "index", Title: "The Imperial Construct"},
		{Path: "services/chronicle", Title: "chronicle — voice notes"},
		{Path: "services/untitled", Title: "untitled"},
		{Path: "versions", Title: "Deployed versions"},
	}
	if len(pages) != len(want) {
		t.Fatalf("pages = %+v, want %+v", pages, want)
	}
	for i := range want {
		if pages[i] != want[i] {
			t.Errorf("page %d = %+v, want %+v", i, pages[i], want[i])
		}
	}
}

func TestReadLiftsTheTitleAndDropsTheFrontMatter(t *testing.T) {
	p, err := corpus(t).Read("index")
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "The Imperial Construct" {
		t.Errorf("title = %q", p.Title)
	}
	if p.Body != "# The Imperial Construct\n\nA home.\n" {
		t.Errorf("body = %q: front matter not removed", p.Body)
	}
	// No front matter: the file is returned whole and the heading names it.
	p, err = corpus(t).Read("services/chronicle")
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "chronicle — voice notes" || p.Body != "# chronicle — voice notes\n\nBody.\n" {
		t.Errorf("page = %+v", p)
	}
}

func TestReadRefusesWhatTheCorpusCannotHold(t *testing.T) {
	c := corpus(t)
	for _, bad := range []string{
		"", "/index", "../index", "services/../index", "services//chronicle",
		".vitepress/config", "index.md", "services/.hidden", "./index",
	} {
		if _, err := c.Read(bad); !errors.Is(err, ErrInvalidPath) {
			t.Errorf("Read(%q) = %v, want ErrInvalidPath", bad, err)
		}
	}
	for _, missing := range []string{"nope", "services", "public/architecture/chronicle"} {
		if _, err := c.Read(missing); !errors.Is(err, ErrNotFound) {
			t.Errorf("Read(%q) = %v, want ErrNotFound", missing, err)
		}
	}
}

func TestBuildReadsTheStamp(t *testing.T) {
	b, ok, err := corpus(t).Build()
	if err != nil || !ok {
		t.Fatalf("build = ok %v err %v", ok, err)
	}
	if b.Ref != "abc1234" || !b.GeneratedAt.Equal(time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("build = %+v", b)
	}
}

func TestAMalformedStampIsAnError(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "build.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.Build(); err == nil {
		t.Fatal("a malformed build.json was read as absent")
	}
}
