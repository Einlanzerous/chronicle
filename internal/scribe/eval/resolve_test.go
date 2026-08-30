package eval

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/scribe"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// fakeCorpus stands in for the tier-1 read path so scoring and resolution can
// be tested without a database. The real one is *store.Tier1Store.
type fakeCorpus struct {
	memos       map[string]store.Memo
	transcripts map[uuid.UUID]store.Transcript
}

func (c fakeCorpus) MemoByHash(_ context.Context, h string) (store.Memo, error) {
	m, ok := c.memos[h]
	if !ok {
		return store.Memo{}, store.ErrNotFound
	}
	return m, nil
}

func (c fakeCorpus) TranscriptForScribe(_ context.Context, id uuid.UUID) (store.Transcript, error) {
	t, ok := c.transcripts[id]
	if !ok {
		return store.Transcript{}, store.ErrNotFound
	}
	return t, nil
}

var _ Corpus = fakeCorpus{}

func corpusWith(h, model, text string) (fakeCorpus, uuid.UUID) {
	memoID, tID := uuid.New(), uuid.New()
	return fakeCorpus{
		memos:       map[string]store.Memo{h: {ID: memoID, ContentHash: h}},
		transcripts: map[uuid.UUID]store.Transcript{memoID: {ID: tID, MemoID: memoID, Model: model, Text: text}},
	}, memoID
}

func TestASyntheticLabelResolvesFromTheRepo(t *testing.T) {
	r := Resolver{Files: fstest.MapFS{
		"synthetic/x.md": &fstest.MapFile{Data: []byte("  so the thing is\n")},
	}}
	l := lbl("x", StratumSynthetic, scribe.DestNote, "", yes())

	it, err := r.Resolve(context.Background(), l)
	if err != nil {
		t.Fatal(err)
	}
	if it.Text != "so the thing is" {
		t.Errorf("text = %q", it.Text)
	}
	if it.PinMoved() {
		t.Error("a fixture has no pin to move")
	}
}

func TestAnEmptyFixtureIsRefused(t *testing.T) {
	r := Resolver{Files: fstest.MapFS{"synthetic/x.md": &fstest.MapFile{Data: []byte("  \n")}}}
	_, err := r.Resolve(context.Background(), lbl("x", StratumSynthetic, scribe.DestNote, "", yes()))
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("err = %v, want an empty-fixture complaint", err)
	}
}

// §8: the harness pulls through TranscriptForScribe rather than a query of its
// own, so it routes from the same text production routes from — the model
// ranking and CHRN-22's durable floor included.
func TestARealLabelResolvesThroughTranscriptForScribe(t *testing.T) {
	h := hash(1)
	c, memoID := corpusWith(h, "whisper.cpp/small.en", "what he said")
	r := Resolver{Corpus: c}

	it, err := r.Resolve(context.Background(), lbl(h, StratumReal, scribe.DestNote, "", yes()))
	if err != nil {
		t.Fatal(err)
	}
	if it.MemoID != memoID || it.Text != "what he said" || it.Model != "whisper.cpp/small.en" {
		t.Fatalf("item = %+v", it)
	}
	if it.PinMoved() {
		t.Error("the pin matched and was reported moved")
	}
}

// §1's [rev], and the failure 0007's transcript_id comment names by name: a
// large-v3 pass changes the words while every content hash still matches. Not
// an error — a re-transcription is a legitimate thing to do — but never silent.
func TestAMovedTranscriptPinIsVisibleAndIsNotAnError(t *testing.T) {
	h := hash(2)
	c, _ := corpusWith(h, "whisper.cpp/large-v3", "the same memo, different words")
	r := Resolver{Corpus: c}

	it, err := r.Resolve(context.Background(), lbl(h, StratumReal, scribe.DestNote, "", yes()))
	if err != nil {
		t.Fatalf("a moved pin must not be an error: %v", err)
	}
	if !it.PinMoved() {
		t.Fatal("the transcript is not the one the labeller read, and nothing said so")
	}
}

// §1's stated cost, as an error message rather than a surprise: the labels
// travel with the repo and the corpus does not.
func TestARealLabelWithoutACorpusSaysWhy(t *testing.T) {
	_, err := Resolver{}.Resolve(context.Background(), lbl(hash(3), StratumReal, scribe.DestNote, "", yes()))
	if !errors.Is(err, ErrNoCorpus) {
		t.Fatalf("err = %v, want ErrNoCorpus", err)
	}
}

func TestAMemoTheCorpusDoesNotHaveIsNamed(t *testing.T) {
	c, _ := corpusWith(hash(4), "whisper.cpp/small.en", "x")
	_, err := Resolver{Corpus: c}.Resolve(context.Background(),
		lbl(hash(5), StratumReal, scribe.DestNote, "", yes()))
	if err == nil || !strings.Contains(err.Error(), "no memo with this content_hash") {
		t.Fatalf("err = %v", err)
	}
}

// A memo whose only transcript is partial, or came from a model this
// deployment has never measured, is one there is nothing trustworthy to route
// from — which TranscriptForScribe reports as not-found.
func TestAMemoWithNoDurableTranscriptIsNamed(t *testing.T) {
	memoID := uuid.New()
	c := fakeCorpus{
		memos:       map[string]store.Memo{hash(6): {ID: memoID}},
		transcripts: map[uuid.UUID]store.Transcript{},
	}
	_, err := Resolver{Corpus: c}.Resolve(context.Background(),
		lbl(hash(6), StratumReal, scribe.DestNote, "", yes()))
	if err == nil || !strings.Contains(err.Error(), "durable floor") {
		t.Fatalf("err = %v", err)
	}
}

// One unresolvable label must not hide the other twenty: the check run exists
// to say which of them resolve.
func TestResolutionCarriesFailuresRatherThanStoppingAtTheFirst(t *testing.T) {
	c, _ := corpusWith(hash(7), "whisper.cpp/small.en", "x")
	r := Resolver{
		Corpus: c,
		Files:  fstest.MapFS{"synthetic/b.md": &fstest.MapFile{Data: []byte("y")}},
	}
	res := r.ResolveAll(context.Background(), []Label{
		lbl(hash(8), StratumReal, scribe.DestNote, "", yes()),  // missing
		lbl(hash(7), StratumReal, scribe.DestNote, "", yes()),  // present
		lbl("a", StratumSynthetic, scribe.DestNote, "", yes()), // missing
		lbl("b", StratumSynthetic, scribe.DestNote, "", yes()), // present
	})
	if len(res.Items) != 2 || len(res.Failures) != 2 {
		t.Fatalf("items=%d failures=%d, want 2 and 2", len(res.Items), len(res.Failures))
	}
}

// The whole committed set resolves against a corpus that has the memos it
// names — which is what `chronicle eval --dry-run` checks on the one machine
// that has them.
func TestTheCommittedSetResolvesAgainstACorpusThatHasIt(t *testing.T) {
	set, err := Load("../../../docs/eval/routing-v1.yaml")
	if err != nil {
		t.Fatal(err)
	}
	c := fakeCorpus{memos: map[string]store.Memo{}, transcripts: map[uuid.UUID]store.Transcript{}}
	for _, l := range set.Select(StratumReal) {
		id, tID := uuid.New(), uuid.New()
		c.memos[l.Hash] = store.Memo{ID: id, ContentHash: l.Hash}
		c.transcripts[id] = store.Transcript{
			ID: tID, MemoID: id, Model: l.LabelledAgainst, Text: "…",
		}
	}
	// os.DirFS via the real tree: the fixtures must actually be committed.
	res := Resolver{Corpus: c, Files: repoFS(t)}.ResolveAll(context.Background(), set.Labels)
	if len(res.Failures) > 0 {
		for _, f := range res.Failures {
			t.Errorf("%s: %v", f.Label.Short(), f.Err)
		}
		t.Fatalf("%d label(s) did not resolve", len(res.Failures))
	}
	if len(res.Moved()) > 0 {
		t.Errorf("pins moved against a corpus built from the pins themselves: %v", res.Moved())
	}
	if len(res.Items) != len(set.Labels) {
		t.Fatalf("resolved %d of %d", len(res.Items), len(set.Labels))
	}
}

func TestRunReportsEveryItemEvenWhenTheRouterFails(t *testing.T) {
	items := []Item{
		{Label: lbl("a", StratumSynthetic, scribe.DestNote, "", yes()), Text: "x"},
		{Label: lbl("b", StratumSynthetic, scribe.DestNote, "", yes()), Text: "y"},
	}
	results, err := Run(context.Background(), flakyRouter{failOn: "b"}, items)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if results[1].Err == nil {
		t.Error("the transport failure was swallowed")
	}
	if results[0].Proposal() == nil {
		t.Error("the item before the failure lost its proposal")
	}
}

type flakyRouter struct{ failOn string }

func (flakyRouter) Proposer() string { return "test/test@v1" }

func (r flakyRouter) Route(_ context.Context, it Item) (scribe.Outcome, error) {
	if strings.Contains(it.Label.File, r.failOn) {
		return scribe.Outcome{Status: scribe.StatusInvalid}, fmt.Errorf("ollama unreachable")
	}
	return scribe.Outcome{
		Proposal: &scribe.Proposal{Destination: scribe.DestNote, Confidence: 0.9, Reason: "r"},
		Status:   scribe.StatusValid,
	}, nil
}
