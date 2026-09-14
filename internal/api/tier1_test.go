package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/api/wire"
	"github.com/Einlanzerous/chronicle/internal/estatewiki"
	"github.com/Einlanzerous/chronicle/internal/scribe"
	"github.com/Einlanzerous/chronicle/internal/triage"
)

// CHRN-100's surface against a corpus on disk, shaped the way construct-server
// generates one. What is proved here is the contract: every declared status
// driven through apitest.Conform, the marking on every tier-1 payload
// including the proposal that predates this group, a path the corpus could
// not hold refused before it touches disk, and a router with no corpus
// answering the documented 503.

func tier1Corpus(t *testing.T, withStamp bool) *estatewiki.Corpus {
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
	write("index.md", "---\ntitle: \"The Imperial Construct\"\n---\n# The Imperial Construct\n\nGenerated on every deploy.\n")
	write("services/chronicle.md", "---\ntitle: \"chronicle\"\n---\n::: info GENERATED PAGE\n:::\nTracked as CHRN-100 and SWY-389.\n")
	write(".vitepress/config.ts", "export default {}")
	if withStamp {
		write("build.json", `{"ref":"abc1234","generated_at":"2026-09-14T12:00:00Z"}`)
	}
	c, err := estatewiki.New(root)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func tier1Router(t *testing.T, corpus *estatewiki.Corpus) (http.Handler, string) {
	t.Helper()
	f := newFakeAccounts()
	f.sessions["tok"] = person("member@example.test", false)
	return NewRouter(Deps{
		DB: fakePinger{}, Accounts: f, Logger: discardLogger(), Version: "test",
		EstateWiki: corpus,
	}), "tok"
}

func tier1Get(h http.Handler, tok, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, withToken(httptest.NewRequest(http.MethodGet, target, nil), tok))
	return rec
}

func assertServMarking(t *testing.T, g wire.Generated, stamped bool) {
	t.Helper()
	if g.Tier != wire.GeneratedTierOne || g.Source != wire.GeneratedSourceServ || g.Regenerable != wire.GeneratedRegenerableTrue {
		t.Errorf("marking = %+v: not tier 1, serv, regenerable", g)
	}
	if g.Notice != noticeServ {
		t.Errorf("notice = %q", g.Notice)
	}
	if !stamped {
		if g.Ref != nil || g.GeneratedAt != nil {
			t.Errorf("an unstamped corpus invented a ref or a time: %+v", g)
		}
		return
	}
	if g.Ref == nil || *g.Ref != "abc1234" {
		t.Errorf("ref = %v, want abc1234", g.Ref)
	}
	if g.GeneratedAt == nil || !g.GeneratedAt.Equal(time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)) {
		t.Errorf("generated_at = %v", g.GeneratedAt)
	}
}

func TestTheCorpusIsListedAndMarked(t *testing.T) {
	h, tok := tier1Router(t, tier1Corpus(t, true))
	rec := tier1Get(h, tok, "/tier1/pages")
	mustStatus(t, rec, http.StatusOK, "listTier1Pages")
	got := decodeInto[wire.Tier1PageList](t, rec)
	if len(got.Items) != 2 || got.Items[0].Path != "index" || got.Items[0].Title != "The Imperial Construct" ||
		got.Items[1].Path != "services/chronicle" || got.Items[1].Title != "chronicle" {
		t.Errorf("items = %+v", got.Items)
	}
	assertServMarking(t, got.Generated, true)
}

func TestAPageIsReadRenderedAndMarked(t *testing.T) {
	h, tok := tier1Router(t, tier1Corpus(t, true))
	rec := tier1Get(h, tok, "/tier1/page?path=services/chronicle")
	mustStatus(t, rec, http.StatusOK, "getTier1Page")
	got := decodeInto[wire.Tier1Page](t, rec)
	if got.Path != "services/chronicle" || got.Title != "chronicle" {
		t.Errorf("page = %+v", got)
	}
	if got.Body != "::: info GENERATED PAGE\n:::\nTracked as CHRN-100 and SWY-389.\n" {
		t.Errorf("body kept the front matter or lost content: %q", got.Body)
	}
	if got.Html == "" || got.Html == got.Body {
		t.Errorf("html was not rendered: %q", got.Html)
	}
	assertServMarking(t, got.Generated, true)
}

// A corpus the generator wrote before SERV-189, or an empty mount, carries no
// stamp — and the payload then omits ref and generated_at rather than
// inventing them.
func TestAnUnstampedCorpusOmitsWhatItCannotKnow(t *testing.T) {
	h, tok := tier1Router(t, tier1Corpus(t, false))
	rec := tier1Get(h, tok, "/tier1/pages")
	mustStatus(t, rec, http.StatusOK, "listTier1Pages")
	assertServMarking(t, decodeInto[wire.Tier1PageList](t, rec).Generated, false)

	empty, err := estatewiki.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	h, tok = tier1Router(t, empty)
	rec = tier1Get(h, tok, "/tier1/pages")
	mustStatus(t, rec, http.StatusOK, "listTier1Pages")
	if got := decodeInto[wire.Tier1PageList](t, rec); len(got.Items) != 0 {
		t.Errorf("an empty mount listed %+v", got.Items)
	}
}

func TestAPathTheCorpusCouldNotHoldIsRefusedBeforeDisk(t *testing.T) {
	h, tok := tier1Router(t, tier1Corpus(t, true))
	for _, q := range []string{
		"path=../index", "path=/index", "path=.vitepress/config", "path=index.md",
		"path=services//chronicle", "path=services/.hidden", "path=",
	} {
		rec := tier1Get(h, tok, "/tier1/page?"+q)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s = %d, want 400: %s", q, rec.Code, rec.Body.String())
			continue
		}
		mustStatus(t, rec, http.StatusBadRequest, "getTier1Page")
	}
	// No parameter at all is the binder's 400, through the same envelope.
	rec := tier1Get(h, tok, "/tier1/page")
	mustStatus(t, rec, http.StatusBadRequest, "getTier1Page")
}

func TestAPageTheCorpusDoesNotHaveIs404(t *testing.T) {
	h, tok := tier1Router(t, tier1Corpus(t, true))
	for _, q := range []string{"path=nope", "path=services", "path=services/chronicle/deeper"} {
		rec := tier1Get(h, tok, "/tier1/page?"+q)
		mustStatus(t, rec, http.StatusNotFound, "getTier1Page")
	}
}

func TestARouterWithNoCorpusAnswersTheDocumented503(t *testing.T) {
	h, tok := tier1Router(t, nil)
	rec := tier1Get(h, tok, "/tier1/pages")
	mustStatus(t, rec, http.StatusServiceUnavailable, "listTier1Pages")
	rec = tier1Get(h, tok, "/tier1/page?path=index")
	mustStatus(t, rec, http.StatusServiceUnavailable, "getTier1Page")
}

// A corrupt stamp is a deploy that went wrong, and is a 500 naming it rather
// than a payload that quietly dropped the ref.
func TestACorruptStampIsAServerError(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "build.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	corpus, err := estatewiki.New(root)
	if err != nil {
		t.Fatal(err)
	}
	h, tok := tier1Router(t, corpus)
	rec := tier1Get(h, tok, "/tier1/pages")
	mustStatus(t, rec, http.StatusInternalServerError, "listTier1Pages")
}

// THE PROPOSAL WAS THE TIER-1 PAYLOAD ON THE WIRE BEFORE THIS GROUP, unmarked.
// It carries the same marking now, with the source that is true of it.
func TestAProposalCarriesTheChronicleMarking(t *testing.T) {
	nearest := "estate/conventions"
	gen := 1
	tr := &fakeTriage{items: []triage.BatchItem{{
		MemoID: uuid.New(), CapturedAt: time.Now(), Excerpt: "…", Proposer: "ollama/x",
		Generation: &gen, Status: "proposed",
		Proposal: &scribe.Proposal{
			Destination: scribe.Destination("NOTE"), Confidence: 0.9,
			Reason: "reads as doctrine", NearestPage: &nearest,
		},
	}}}
	h, tok := signedInTriage(t, false, tr)
	rec := tier1Get(h, tok, "/triage/batch")
	mustStatus(t, rec, http.StatusOK, "getTriageBatch")
	got := decodeInto[wire.TriageBatch](t, rec)
	if len(got.Items) != 1 || got.Items[0].Proposal == nil {
		t.Fatalf("batch = %+v", got)
	}
	g := got.Items[0].Proposal.Generated
	if g.Tier != wire.GeneratedTierOne || g.Source != wire.GeneratedSourceChronicle ||
		g.Regenerable != wire.GeneratedRegenerableTrue || g.Notice != noticeChronicle {
		t.Errorf("proposal marking = %+v", g)
	}
	if g.Ref != nil || g.GeneratedAt != nil {
		t.Errorf("a proposal invented a build ref or a time: %+v", g)
	}
}
