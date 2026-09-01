package catalogue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"unicode"

	"github.com/Einlanzerous/chronicle/internal/switchyard"
)

// Live turns Switchyard's live project list into a Snapshot, which is CHRN-31's
// half of "read the live project list, do not hardcode it".
//
// A ROUTER WITH A STALE CATALOGUE routes today's memo into last month's
// structure, and the error is invisible because the destination it picked does
// exist — it is just the wrong one. That is what this prevents, and it is why
// there is no TTL and no package-level cache: the Snapshot IS the cache, it
// lives for one run, and nothing outlives the batch that fetched it.
//
// THE SPLIT WITH internal/switchyard IS BY SUBJECT. That package knows about
// HTTP, pagination and which projects are live; this one knows what a PROMPT
// needs — a budget, a stable order, and a hash the eval report can record.
//
// THE PAGE HALF IS NOT HERE. CHRN-31's second `Done when` — a page renamed is
// not offered under its old path — needs a page tree, and there is none until
// CHRN-37 in E5. Pages come back empty, which scribe.Reconcile already treats
// as the ordinary case rather than a special one.
type Live struct{ sw *switchyard.Client }

// maxDescription is the per-project prompt budget, and it is A NUMBER SOMEBODY
// CHOSE.
//
// The salvage cut at 150 characters, mid-word, with no ellipsis — not because
// 150 was right but because the prompt was assembled by string concatenation
// and nobody had a budget. Sixteen projects at this width is about a thousand
// tokens of a 16 384 window, which is affordable; the first sentence of a
// Switchyard description is almost always the summary, so the cut lands on a
// sentence where it can and a word where it cannot.
const maxDescription = 240

// NewLive wraps a Switchyard client.
func NewLive(sw *switchyard.Client) *Live { return &Live{sw: sw} }

// Fetch reads the live catalogue.
func (l *Live) Fetch(ctx context.Context) (*Snapshot, error) {
	ps, err := l.sw.Projects(ctx)
	if err != nil {
		return nil, err
	}
	if len(ps) == 0 {
		// Refused rather than returned. An empty catalogue is indistinguishable
		// from a working one until somebody reads the proposals, where every
		// TICKET has landed needs_input with its project cleared — which reads
		// as a router that cannot route rather than as a fetch that came back
		// empty.
		return nil, fmt.Errorf("catalogue: Switchyard returned no live projects, so every TICKET would answer with an empty project_key")
	}

	s := &Snapshot{Version: 1, Pages: []string{}}
	for _, p := range ps {
		s.Projects = append(s.Projects, Project{
			Key: p.Key, Name: p.Name, Description: summarise(p.Description),
		})
	}

	// ORDER IS FIXED HERE, not left to the endpoint. The salvage's list was
	// "unfiltered and unordered — whatever the endpoint returns, in whatever
	// order", which varies the prompt between runs for no reason and puts a
	// spurious difference in the catalogue hash the report records.
	sort.Slice(s.Projects, func(i, j int) bool { return s.Projects[i].Key < s.Projects[j].Key })
	s.sha = contentHash(s.Projects)
	return s, nil
}

// contentHash identifies a fetched catalogue, since there is no file to hash.
//
// Over the RENDERED CONTENT rather than the response bytes, so that a
// Switchyard release adding a field, or reordering the list, does not read as a
// catalogue change. What matters is what the model saw.
func contentHash(ps []Project) string {
	h := sha256.New()
	for _, p := range ps {
		_, _ = fmt.Fprintf(h, "%s\x00%s\x00%s\n", p.Key, p.Name, p.Description)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// summarise cuts a description to the prompt budget: the first sentence where
// there is one, otherwise a word boundary, with an ellipsis so a reader can see
// it was cut.
func summarise(d string) string {
	d = strings.Join(strings.Fields(strings.ReplaceAll(d, "\n", " ")), " ")
	if d == "" {
		return ""
	}
	// A sentence end, but not an abbreviation or a version number: require the
	// full stop to be followed by a space and a capital.
	for i := 40; i < len(d)-2 && i < maxDescription; i++ {
		if d[i] == '.' && d[i+1] == ' ' && unicode.IsUpper(rune(d[i+2])) {
			return d[:i+1]
		}
	}
	if len(d) <= maxDescription {
		return d
	}
	cut := d[:maxDescription]
	if i := strings.LastIndex(cut, " "); i > maxDescription/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,;:—-") + "…"
}
