// Package estatewiki reads SERV-101's generated estate wiki — tier 1, the
// estate's account of what exists — from a read-only mount of the markdown
// corpus construct-server's generator emits (CHRN-100).
//
// ============================================================================
// A SECOND VIEW OF THE SAME ARTEFACT, NOT A COPY.
// ============================================================================
//
// The corpus is not in any database. construct-server's `wiki/generate` writes
// it into `wiki/docs` on every deploy, VitePress renders it into a site, and
// SERV-189 publishes the markdown itself to the deploy root and bind-mounts it
// into this container read-only. This package reads those files and nothing
// else: no table, no poller, no ingest. The kernel enforces read-only; the
// marking every payload carries (api.Generated) says who regenerates it.
//
// Nothing here touches either database pool. Tier 1's own rows — proposals,
// jobs, extracted links — are the tier-1 store's; this is the OTHER half of
// tier 1, the half CLAUDE.md calls "the estate's account of what exists".
//
// ============================================================================
// PATHS ARE THE CORPUS'S OWN, AND THEY ARE VALIDATED BEFORE THEY TOUCH DISK.
// ============================================================================
//
// A page is addressed the way the generator wrote it — `services/chronicle`,
// `versions` — without the `.md`. A path is refused rather than cleaned when
// it is not a plain relative path of dot-free segments: `..`, a leading slash,
// an empty segment, or any segment starting with `.` (which is how
// `.vitepress`, the renderer's own directory, stays out of reach). The read
// goes through an fs.FS rooted at the mount, so even a path that slipped the
// check could not leave it.
package estatewiki

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Errors a caller maps onto a status.
var (
	// ErrNotFound: the corpus has no page at that path.
	ErrNotFound = errors.New("estatewiki: no such page")
	// ErrInvalidPath: the path is not one this corpus could hold, and was
	// refused before touching disk.
	ErrInvalidPath = errors.New("estatewiki: invalid page path")
)

// buildFile is what the generator writes beside the pages (SERV-189): which
// deploy generated the corpus, and when.
const buildFile = "build.json"

// maxPage bounds a single page read. The corpus is generated and trusted, but
// a bound turns "somebody pointed the mount at the wrong directory" into an
// error naming the file rather than a multi-gigabyte response.
const maxPage = 8 << 20

// Corpus is the mounted corpus.
type Corpus struct {
	root string
	fsys fs.FS
}

// New opens the corpus at root, which must exist and be a directory. Empty is
// fine — "not generated yet" is a legitimate state on a fresh host — but
// missing is not: `serve` refuses to boot rather than serve an empty tier 1
// from a typo'd path, for the reason CHRONICLE_AUDIO_DIR does.
func New(root string) (*Corpus, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, fmt.Errorf("estate wiki %s: %w (mount the corpus, or create the directory)", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("estate wiki %s is not a directory", root)
	}
	return &Corpus{root: root, fsys: os.DirFS(root)}, nil
}

// Root is the directory the corpus was opened at.
func (c *Corpus) Root() string { return c.root }

// Build is the generator's stamp: which construct-server commit the corpus was
// generated from, and when. Absent on a corpus the generator wrote before
// SERV-189, and absent on an empty mount.
type Build struct {
	Ref         string    `json:"ref"`
	GeneratedAt time.Time `json:"generated_at"`
}

// Build reads the stamp. ok is false when the corpus carries none, which is
// not an error: the payload then omits the fields rather than inventing them.
func (c *Corpus) Build() (b Build, ok bool, err error) {
	raw, err := fs.ReadFile(c.fsys, buildFile)
	if errors.Is(err, fs.ErrNotExist) {
		return Build{}, false, nil
	}
	if err != nil {
		return Build{}, false, fmt.Errorf("estatewiki: read %s: %w", buildFile, err)
	}
	if err := json.Unmarshal(raw, &b); err != nil {
		return Build{}, false, fmt.Errorf("estatewiki: parse %s: %w", buildFile, err)
	}
	return b, true, nil
}

// Summary is a page as the list carries it.
type Summary struct {
	Path  string
	Title string
}

// Page is a page as a read returns it. Body is the markdown with the
// generator's front matter removed: `title` is lifted out of it, and the rest
// of it is VitePress furniture, not content.
type Page struct {
	Path  string
	Title string
	Body  string
}

// List walks the corpus and returns every page, sorted by path. Dot-prefixed
// entries — `.vitepress` above all — are never descended into; anything that
// is not a `.md` file is not a page.
func (c *Corpus) List() ([]Summary, error) {
	var out []Summary
	err := fs.WalkDir(c.fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p == "." {
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() || !strings.HasSuffix(p, ".md") {
			return nil
		}
		raw, err := fs.ReadFile(c.fsys, p)
		if err != nil {
			return fmt.Errorf("estatewiki: read %s: %w", p, err)
		}
		pagePath := strings.TrimSuffix(p, ".md")
		title, _ := splitFrontMatter(pagePath, string(raw))
		out = append(out, Summary{Path: pagePath, Title: title})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	if out == nil {
		out = []Summary{}
	}
	return out, nil
}

// Read returns one page. ErrInvalidPath for a path this corpus could not hold;
// ErrNotFound for one it merely does not.
func (c *Corpus) Read(pagePath string) (Page, error) {
	if !ValidPath(pagePath) {
		return Page{}, ErrInvalidPath
	}
	info, err := fs.Stat(c.fsys, pagePath+".md")
	if errors.Is(err, fs.ErrNotExist) {
		return Page{}, ErrNotFound
	}
	if err != nil {
		return Page{}, fmt.Errorf("estatewiki: stat %s: %w", pagePath, err)
	}
	if info.IsDir() {
		return Page{}, ErrNotFound
	}
	if info.Size() > maxPage {
		return Page{}, fmt.Errorf("estatewiki: %s is %d bytes, over the %d-byte bound", pagePath, info.Size(), maxPage)
	}
	raw, err := fs.ReadFile(c.fsys, pagePath+".md")
	if err != nil {
		return Page{}, fmt.Errorf("estatewiki: read %s: %w", pagePath, err)
	}
	title, body := splitFrontMatter(pagePath, string(raw))
	return Page{Path: pagePath, Title: title, Body: body}, nil
}

// ValidPath reports whether p is a path this corpus could hold: relative,
// non-empty, slash-separated segments that are neither empty nor dot-prefixed,
// with no `.md` suffix (the address is the page, not the file).
func ValidPath(p string) bool {
	if p == "" || strings.HasSuffix(p, ".md") || !fs.ValidPath(p) {
		return false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == "" || strings.HasPrefix(seg, ".") {
			return false
		}
	}
	return true
}

// splitFrontMatter lifts the title out of the generator's YAML front matter and
// returns the body without it. The front matter is the one shape the generator
// writes — `---`, `key: value` lines, `---` — and nothing more general is
// parsed: a file that does not start with it is returned whole, and the title
// then comes from the first `# ` heading, then from the last path segment.
func splitFrontMatter(pagePath, raw string) (title, body string) {
	body = raw
	if rest, ok := strings.CutPrefix(raw, "---\n"); ok {
		if fm, after, found := strings.Cut(rest, "\n---\n"); found {
			body = after
			for _, line := range strings.Split(fm, "\n") {
				k, v, ok := strings.Cut(line, ":")
				if !ok || strings.TrimSpace(k) != "title" {
					continue
				}
				v = strings.TrimSpace(v)
				if u, err := strconv.Unquote(v); err == nil {
					v = u
				}
				title = v
			}
		}
	}
	if title == "" {
		for _, line := range strings.Split(body, "\n") {
			if h, ok := strings.CutPrefix(line, "# "); ok {
				title = strings.TrimSpace(h)
				break
			}
		}
	}
	if title == "" {
		title = path.Base(pagePath)
	}
	return title, body
}
