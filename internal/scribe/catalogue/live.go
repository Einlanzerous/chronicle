package catalogue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

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
//
// NEITHER IS THE NOTE HALF, for a different reason. The page tree now exists,
// but a live note list is not a fetch — it is retrieval over the corpus for a
// given memo (CHRN-36 §7 points at CHRN-41), and a Fetch that returned every
// note would not fit a prompt. Notes come back empty and stage 2 treats that
// as the ordinary case too.
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

// minSummary is the shortest run this will accept as a first sentence, and it
// replaces an offset floor that was the wrong shape for the job — see summarise.
const minSummary = 12

// notASentenceEnd is the set of words that end in a full stop and are followed
// by a capital often enough to fool the sentence scan.
//
// SMALL ON PURPOSE. This is a list of things that turn up in project
// descriptions, not a general English abbreviation table: a bigger one would be
// more code defending against text this estate does not write.
var notASentenceEnd = map[string]bool{
	"inc": true, "ltd": true, "llc": true, "corp": true, "co": true,
	"etc": true, "eg": true, "e.g": true, "ie": true, "i.e": true,
	"vs": true, "approx": true, "cf": true, "no": true,
	"mr": true, "ms": true, "mrs": true, "dr": true, "st": true, "jr": true, "sr": true,
}

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

	s := &Snapshot{Version: 1, Pages: []string{}, Notes: []string{}}
	for _, p := range ps {
		// THE SAME INVARIANT Parse ENFORCES, and it is here because the two
		// constructors of one Snapshot were disagreeing about a rule only one
		// of them documented.
		//
		// Parse says why: "a lowercase key here would be a catalogue that can
		// only produce answers stage 1 rejects" — validate.go requires an
		// uppercase project_key, so a lowercase one renders into the prompt,
		// the model answers it, stage 1 rejects it, and the attempt burns to
		// MaxAttempts for that project alone.
		//
		// DROPPED RATHER THAN REFUSED, which is where the two constructors are
		// allowed to differ. A fixture is a file a person edits, so Parse
		// refuses and they fix it. A live list belongs to another service and
		// Chronicle cannot fix it, so refusing would stop ALL routing over one
		// project's data error. Dropping costs that project alone, and it is
		// not silent: with no such destination in the prompt the model answers
		// an empty project_key, its memos land needs_input, and they show up on
		// the triage screen asking for a project. If dropping leaves nothing,
		// the refusal above fires, so it cannot hide a wholesale error either.
		if p.Key != strings.ToUpper(p.Key) {
			continue
		}
		s.Projects = append(s.Projects, Project{
			Key: p.Key, Name: p.Name, Description: summarise(p.Description),
		})
	}
	if len(s.Projects) == 0 {
		return nil, fmt.Errorf("catalogue: Switchyard returned no live projects with a usable key, so every TICKET would answer with an empty project_key")
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
// there is one worth trusting, otherwise a word boundary, with an ellipsis so a
// reader can see it was cut.
//
// "WORTH TRUSTING" IS THE WHOLE OF THE DIFFICULTY, and the guard used to be an
// offset: the scan began at byte 40, so a description whose first sentence was
// shorter than that never matched and fell through to the hard 240-byte cut.
// EIDO's is `Thin-client Agent OS portal.` — twenty-eight characters, and
// exactly the summary maxDescription's comment claims this lands on. It got
// two-and-a-bit sentences instead, and the comment described behaviour the code
// did not have.
//
// An offset was never the right shape, because what it defends against is an
// ABBREVIATION — `Inc.`, `Est.`, `Dr.` — and those are short AND a single
// token, while a genuine short summary is short and has several words. So the
// guard now tests the candidate itself: long enough, more than one word, and
// not ending in a word from the small set that is usually not a sentence end.
// The existing "full stop, then a space, then a capital" rule already excludes
// version numbers, which carry no space.
//
// The residual failure is a summary cut SHORTER than it should be, never one
// that says something wrong.
func summarise(d string) string {
	d = strings.Join(strings.Fields(strings.ReplaceAll(d, "\n", " ")), " ")
	if d == "" {
		return ""
	}
	for i := minSummary; i < len(d)-2 && i < maxDescription; i++ {
		if d[i] != '.' || d[i+1] != ' ' || !unicode.IsUpper(rune(d[i+2])) {
			continue
		}
		if isSentence(d[:i]) {
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
	// ON A RUNE BOUNDARY. The word-boundary cut above usually removes a
	// straddling rune for free, but it does not fire on a long unbroken run —
	// and a partial rune here goes into the prompt, where nobody would trace it
	// back to this line.
	for len(cut) > 0 && !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return strings.TrimRight(cut, " ,;:—-") + "…"
}

// isSentence reports whether a run ending at a full stop reads as a whole
// sentence rather than as an abbreviation somebody happened to put a capital
// after.
func isSentence(candidate string) bool {
	if len(candidate) < minSummary {
		return false
	}
	i := strings.LastIndex(candidate, " ")
	if i < 0 {
		// One word. `Thin-client.` is not a summary; `Inc.` is not either.
		return false
	}
	return !notASentenceEnd[strings.ToLower(candidate[i+1:])]
}
