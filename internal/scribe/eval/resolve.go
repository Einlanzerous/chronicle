package eval

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/store"
)

// Item is one thing to route: a label, and the text Scribe would actually see
// for it.
//
// The provenance fields are not decoration. §2 requires every scoring run
// against the held-out stratum to record THE TRANSCRIPT EACH MEMO WAS SCORED
// FROM, model and id, because a ranking-driven input change would otherwise
// read as prompt drift. They travel from resolution all the way into the
// report for that reason.
type Item struct {
	Label Label

	// MemoID and TranscriptID are zero for a synthetic fixture: nobody said it,
	// so there is no memo and no ASR run.
	MemoID       uuid.UUID
	TranscriptID uuid.UUID

	// Model is tier2.transcripts.model as served, or empty for a fixture.
	Model string

	Text string
}

// PinMoved reports that the transcript actually served is not the one the
// labeller read (§1's [rev]).
//
// IT IS NOT AN ERROR AND IT IS NOT NOTHING. A large-v3 pass over the corpus is
// a legitimate thing to do, and the run that follows it is legitimate too — it
// is simply NOT COMPARABLE to the runs before it, and the label's reason was
// written about different words. So the harness records it, the report shouts
// about it, and the command refuses to score without being told to go ahead.
func (i Item) PinMoved() bool {
	return i.Label.Stratum == StratumReal && i.Model != i.Label.LabelledAgainst
}

// Corpus is the read side of tier 1, and it is an interface here so the scoring
// tests need no database. *store.Tier1Store satisfies it.
//
// TIER-1 READS ONLY. There is no method on this interface that writes anything,
// which is the same inner wall store.Tier1Store draws for the same reason: the
// harness that grades derived work should not be able to author tier 2 by
// accident.
type Corpus interface {
	MemoByHash(ctx context.Context, contentHash string) (store.Memo, error)
	TranscriptForScribe(ctx context.Context, memoID uuid.UUID) (store.Transcript, error)
}

// Resolver turns labels back into routable text.
//
// TWO SOURCES, ONE PER STRATUM, and which one is used is decided by the label
// rather than by a flag: a real memo comes out of the corpus through exactly
// the query production uses, and a fixture comes off disk. Anything else would
// score a path that does not exist.
type Resolver struct {
	// Corpus resolves the real stratum. Nil is legal and means real labels
	// cannot be resolved on this machine — which is the ordinary state of any
	// machine that is not this one (§1's stated cost).
	Corpus Corpus
	// Files resolves the synthetic stratum, rooted at the label file's own
	// directory.
	Files fs.FS
}

// ErrNoCorpus is what a real label resolves to with no corpus attached.
var ErrNoCorpus = errors.New("eval: no corpus: real labels resolve only where tier2.memos lives (§1)")

// Resolve produces the Item for one label.
func (r Resolver) Resolve(ctx context.Context, l Label) (Item, error) {
	switch l.Stratum {
	case StratumSynthetic:
		if r.Files == nil {
			return Item{}, fmt.Errorf("eval: %s: no fixture directory", l.Short())
		}
		b, err := fs.ReadFile(r.Files, l.File)
		if err != nil {
			return Item{}, fmt.Errorf("eval: %s: %w", l.Short(), err)
		}
		text := strings.TrimSpace(string(b))
		if text == "" {
			return Item{}, fmt.Errorf("eval: %s: fixture is empty", l.File)
		}
		return Item{Label: l, Text: text}, nil

	case StratumReal:
		if r.Corpus == nil {
			return Item{}, fmt.Errorf("eval: %s: %w", l.Short(), ErrNoCorpus)
		}
		m, err := r.Corpus.MemoByHash(ctx, l.Hash)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return Item{}, fmt.Errorf("eval: %s: no memo with this content_hash — the corpus this set was labelled against is not here", l.Short())
			}
			return Item{}, fmt.Errorf("eval: %s: %w", l.Short(), err)
		}
		// THROUGH TranscriptForScribe AND NOT A QUERY OF ITS OWN. §8 says so,
		// and the reason is that the eval must route from the same text
		// production routes from — including the model ranking and CHRN-22's
		// durable floor. A harness with its own idea of which transcript to
		// use grades a router nobody runs.
		t, err := r.Corpus.TranscriptForScribe(ctx, m.ID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				return Item{}, fmt.Errorf("eval: %s: no transcript clears the durable floor, so there is nothing trustworthy to route from", l.Short())
			}
			return Item{}, fmt.Errorf("eval: %s: %w", l.Short(), err)
		}
		return Item{Label: l, MemoID: m.ID, TranscriptID: t.ID, Model: t.Model, Text: t.Text}, nil

	default:
		return Item{}, fmt.Errorf("eval: %s: unknown stratum %q", l.Short(), l.Stratum)
	}
}

// Resolution is what one pass over a label set produced, successes and failures
// together.
//
// FAILURES ARE CARRIED RATHER THAN RETURNED. One unresolvable label should not
// hide the other twenty: the check run exists to say which of them resolve, and
// stopping at the first missing memo answers that question one label at a time.
type Resolution struct {
	Items    []Item
	Failures []Failure
}

// Failure is one label that could not be resolved.
type Failure struct {
	Label Label
	Err   error
}

// Moved returns the resolved items whose transcript pin has moved.
func (r Resolution) Moved() []Item {
	var out []Item
	for _, it := range r.Items {
		if it.PinMoved() {
			out = append(out, it)
		}
	}
	return out
}

// ResolveAll resolves every label, keeping order.
func (r Resolver) ResolveAll(ctx context.Context, labels []Label) Resolution {
	var res Resolution
	for _, l := range labels {
		it, err := r.Resolve(ctx, l)
		if err != nil {
			res.Failures = append(res.Failures, Failure{Label: l, Err: err})
			continue
		}
		res.Items = append(res.Items, it)
	}
	return res
}
