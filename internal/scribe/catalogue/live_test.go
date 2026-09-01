package catalogue

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/Einlanzerous/chronicle/internal/switchyard"
)

// live builds a Live over a stub Switchyard returning these projects. The
// transport itself — pagination, archived filtering, auth — is tested in
// internal/switchyard; what is tested here is what a PROMPT needs.
func live(t *testing.T, projects ...map[string]any) *Live {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": projects,
			"page":  map[string]any{"next_cursor": "", "has_more": false},
		})
	}))
	t.Cleanup(s.Close)
	c, err := switchyard.New(s.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}
	return NewLive(c)
}

func p(key, name, desc string) map[string]any {
	return map[string]any{"key": key, "name": name, "description": desc,
		"archived_at": nil, "deleted_at": nil}
}

func fetch(t *testing.T, l *Live) *Snapshot {
	t.Helper()
	s, err := l.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// The first Done-when: a project created an hour ago is a valid destination.
// There is no TTL and no package cache — the Snapshot IS the cache, and it
// lives for one run.
func TestAProjectAddedSinceTheLastRunIsADestination(t *testing.T) {
	first := true
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		items := []map[string]any{p("ARGY", "Argosy", "media")}
		if !first {
			items = append(items, p("CANT", "Catenary", "chat"))
		}
		first = false
		_ = json.NewEncoder(w).Encode(map[string]any{
			"items": items, "page": map[string]any{"has_more": false},
		})
	}))
	t.Cleanup(s.Close)
	c, err := switchyard.New(s.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}
	l := NewLive(c)

	if fetch(t, l).HasProject("CANT") {
		t.Fatal("the new project existed before it was created")
	}
	if !fetch(t, l).HasProject("CANT") {
		t.Fatal("a project created between runs is not a destination — the catalogue is cached across runs")
	}
}

// Order is fixed here rather than left to the endpoint: an unordered list
// varies the prompt between runs for no reason and puts a spurious difference
// in the catalogue hash the report records.
func TestTheListIsOrderedSoTheHashIsStable(t *testing.T) {
	a := fetch(t, live(t, p("ZZZ", "Z", ""), p("AAA", "A", "")))
	b := fetch(t, live(t, p("AAA", "A", ""), p("ZZZ", "Z", "")))

	if a.Projects[0].Key != "AAA" {
		t.Errorf("order = %v, want sorted by key", a.Projects)
	}
	if a.SHA256() != b.SHA256() {
		t.Error("the same projects in a different order produced different hashes")
	}
}

// An empty catalogue is indistinguishable from a working one until somebody
// reads the proposals, where every TICKET has landed needs_input.
func TestAnEmptyProjectListIsRefusedRatherThanReturned(t *testing.T) {
	_, err := live(t).Fetch(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no live projects") {
		t.Fatalf("err = %v, want a refusal naming the empty list", err)
	}
}

// The page half of CHRN-31's Done-when needs CHRN-37. Until then the tree is
// empty, which scribe.Reconcile treats as the ordinary case.
func TestThePageTreeIsEmptyUntilChrn37(t *testing.T) {
	s := fetch(t, live(t, p("A", "A", "")))
	if len(s.Pages) != 0 || s.HasPage("anything") {
		t.Fatal("a page tree appeared before CHRN-37")
	}
	if !strings.Contains(s.RenderPages(), "none") {
		t.Error("the empty tree does not say so to the model")
	}
}

// The budget is a number somebody chose. The salvage cut at 150 mid-word with
// no ellipsis because nobody had one.
func TestDescriptionsAreCutToADeliberateBudget(t *testing.T) {
	long := "Purser is the cross-service provisioning and invite service for the Construct. " +
		"One command invites a person into multiple services at once, mints starter credentials, " +
		"grants SSO, and returns a copy-pasteable credential block that can also be emailed."
	got := fetch(t, live(t, p("PRSR", "Purser", long))).Projects[0].Description

	if len(got) > maxDescription {
		t.Fatalf("description is %d chars, over the %d budget: %q", len(got), maxDescription, got)
	}
	if got != "Purser is the cross-service provisioning and invite service for the Construct." {
		t.Errorf("description = %q, want the first sentence", got)
	}

	d := fetch(t, live(t, p("X", "X", strings.Repeat("wibble ", 60)))).Projects[0].Description
	if len(d) > maxDescription || !strings.HasSuffix(d, "…") || strings.HasSuffix(d, "wibb…") {
		t.Errorf("description = %q, want a word-boundary cut with an ellipsis", d)
	}
}

// The one test that talks to the real Switchyard. Skips without config, the way
// the database tests do.
func TestAgainstTheLiveSwitchyard(t *testing.T) {
	base, token := os.Getenv("CHRONICLE_SWITCHYARD_URL"), os.Getenv("CHRONICLE_SWITCHYARD_TOKEN")
	if base == "" || token == "" {
		t.Skip("CHRONICLE_SWITCHYARD_URL / _TOKEN unset")
	}
	c, err := switchyard.New(base, token)
	if err != nil {
		t.Fatal(err)
	}
	s, err := NewLive(c).Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !s.HasProject("CHRN") {
		t.Errorf("the live catalogue has no CHRN: %v", s.Projects)
	}
	for _, pr := range s.Projects {
		if len(pr.Description) > maxDescription {
			t.Errorf("%s: description over budget (%d)", pr.Key, len(pr.Description))
		}
	}
	t.Logf("%d live projects, catalogue %s", len(s.Projects), s.SHA256()[:12])
	fmt.Fprintln(os.Stderr, s.RenderProjects())
}
