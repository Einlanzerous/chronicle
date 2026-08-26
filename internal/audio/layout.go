// Package audio owns where a memo's recording lives on disk and what the
// corpus costs.
//
// The layout is a pure function of immutable columns and nothing else. CHRN-23
// asks that "every audio file's path is derivable from its memo row alone", and
// CHRN-18 answered it by declining to add an audio_path column: the identity
// columns cannot change, so the path is derivable and storing it too would be a
// second source of truth for one fact.
//
// That constraint is what rules out the obvious alternatives. A path built from
// the original filename, the state, the retention class or the author's display
// name is a path that MOVES when a memo is renamed or reclassified — and a file
// the pruner cannot find is a file the pruner cannot delete and the player
// cannot play.
package audio

import (
	"fmt"
	"path/filepath"
	"regexp"

	"github.com/google/uuid"
)

// hashPattern is 0003's CHECK constraint, restated where a path is built from
// the value. The database is the authority; this is here because a filesystem
// path assembled from unvalidated data is a directory traversal, and "the
// column has a CHECK on it" is not a defence when the value arrives from a
// caller rather than from a row.
var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// fanout is how many leading hash characters become a subdirectory.
//
// Two hex characters, so 256 buckets under each author. The sizing this is
// built against (CHRN-22: 4.1 GB over 812 memos, settling near 340 MB at 30
// days rolling) puts a handful of files in each bucket, and a second level
// would be 65,536 mostly-empty directories to solve a problem three orders of
// magnitude away.
const fanout = 2

// Ref identifies one recording on disk. Both fields are immutable in
// tier2.memos — 0003's trigger rejects an UPDATE that moves either — which is
// what makes a path built from them stable for the life of the memo.
type Ref struct {
	AuthorID    uuid.UUID
	ContentHash string
}

// Store is a rooted directory of recordings.
//
// It does not read or write audio: CHRN-19 and CHRN-20 put files here, CHRN-22
// removes them, CHRN-21 reads them. This type owns the naming and the
// arithmetic, which is the part all four have to agree on.
type Store struct {
	root string
}

// New returns a Store rooted at dir, which must be an absolute path.
//
// Relative is refused rather than resolved: the daemon's working directory is
// not a property anyone deploying this thinks about, and a corpus that lands
// somewhere different depending on how the process was started is a corpus that
// gets half-pruned. It does NOT create the directory — a typo'd path silently
// springing into existence is how audio ends up on the container's ephemeral
// layer instead of on the NVMe, which looks like it works until a redeploy.
func New(dir string) (*Store, error) {
	if dir == "" {
		return nil, fmt.Errorf("audio: root directory is required")
	}
	if !filepath.IsAbs(dir) {
		return nil, fmt.Errorf("audio: root %q must be an absolute path", dir)
	}
	return &Store{root: filepath.Clean(dir)}, nil
}

// Root is the directory this Store owns.
func (s *Store) Root() string { return s.root }

// RelPath is the path of a recording relative to the root:
//
//	<author_id>/<first two hash characters>/<hash>
//
// **The author scope is load-bearing, and it is the one thing here the ticket
// did not settle.** Content-addressing alone would be the conventional choice,
// and it is wrong for this corpus for a reason that only shows up in CHRN-22.
// `memos_author_content` is UNIQUE on (author_id, content_hash), not on
// content_hash — so the same bytes delivered by two different authors are two
// memo rows by design (0003 says so: "a second memo under the second author
// rather than silently re-attributing the first"). Under a hash-only layout
// those two rows share one file, and pruning either one has to refcount the
// other or delete audio a live memo still needs.
//
// A refcount inside the pruner is precisely where an irreversible-deletion bug
// would live, and CLAUDE.md calls pruning audio that should have been kept the
// single worst thing this system can do. Scoping the path by author makes each
// memo's file its own, so CHRN-22 is an unlink and never an arithmetic problem.
// The cost is duplicate bytes when two authors hold byte-identical audio, which
// for a personal corpus is a case that does not arise — and if it ever does, it
// costs disk that the sizing has three orders of magnitude of headroom for.
//
// There is deliberately no extension. The obvious one — .opus, .wav — would be
// derived from `codec`, which is NULL until CHRN-21 has run and is rewritten
// when it does. A path that only becomes knowable after normalisation is not
// derivable from the memo row alone, which is the one property this layout
// exists to have. The bytes are self-describing and the row records the codec.
func RelPath(r Ref) (string, error) {
	if r.AuthorID == uuid.Nil {
		return "", fmt.Errorf("audio: author id is required")
	}
	if !hashPattern.MatchString(r.ContentHash) {
		return "", fmt.Errorf("audio: %q is not a lowercase 64-character hex digest", r.ContentHash)
	}
	return filepath.Join(r.AuthorID.String(), r.ContentHash[:fanout], r.ContentHash), nil
}

// Path is the absolute path of a recording in this Store.
func (s *Store) Path(r Ref) (string, error) {
	rel, err := RelPath(r)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.root, rel), nil
}

// refFromRel recovers a Ref from a path relative to the root, reporting false
// for anything this layout did not write. The scan uses it to tell a recording
// from a stray rather than assuming everything under the root is ours — a file
// dropped here by hand must not be counted as corpus, and must not be reported
// as an orphan that CHRN-22 could then be pointed at.
func refFromRel(rel string) (Ref, bool) {
	parts := splitAll(filepath.Clean(rel))
	if len(parts) != 3 {
		return Ref{}, false
	}
	author, err := uuid.Parse(parts[0])
	if err != nil || author == uuid.Nil {
		return Ref{}, false
	}
	if !hashPattern.MatchString(parts[2]) || parts[1] != parts[2][:fanout] {
		return Ref{}, false
	}
	// uuid.Parse accepts forms the layout never writes (braced, urn:, no
	// hyphens), so a directory named any of those would read back as a Ref
	// whose own Path() points somewhere else. Require the canonical spelling.
	if parts[0] != author.String() {
		return Ref{}, false
	}
	return Ref{AuthorID: author, ContentHash: parts[2]}, true
}

func splitAll(p string) []string {
	var out []string
	for {
		dir, name := filepath.Split(p)
		if name != "" {
			out = append([]string{name}, out...)
		}
		if dir == "" || dir == p {
			return out
		}
		p = filepath.Clean(dir)
		if p == "." || p == string(filepath.Separator) {
			return out
		}
	}
}
