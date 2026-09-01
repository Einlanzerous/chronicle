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
)

type page struct {
	Items []map[string]any `json:"items"`
	Page  struct {
		NextCursor string `json:"next_cursor"`
		HasMore    bool   `json:"has_more"`
	} `json:"page"`
}

func project(key, name, desc string, extra map[string]any) map[string]any {
	m := map[string]any{"key": key, "name": name, "description": desc,
		"archived_at": nil, "deleted_at": nil}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

// switchyard serves /v1/projects a page at a time and records what it was asked.
func switchyard(t *testing.T, pages []page) (*httptest.Server, *[]string) {
	t.Helper()
	var asked []string
	i := 0
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		asked = append(asked, r.URL.RequestURI())
		p := pages[i]
		if i < len(pages)-1 {
			i++
		}
		_ = json.NewEncoder(w).Encode(p)
	}))
	t.Cleanup(s.Close)
	return s, &asked
}

func onePage(items ...map[string]any) []page { return []page{{Items: items}} }

func fetch(t *testing.T, pages []page) (*Snapshot, *[]string) {
	t.Helper()
	srv, asked := switchyard(t, pages)
	l, err := NewLive(srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}
	s, err := l.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return s, asked
}

// The first Done-when: a project created an hour ago is a valid destination.
// There is no TTL and no package cache — the snapshot IS the cache, and it
// lives for one run.
func TestAProjectAddedSinceTheLastRunIsADestination(t *testing.T) {
	srv, _ := switchyard(t, []page{
		{Items: []map[string]any{project("ARGY", "Argosy", "media", nil)}},
		{Items: []map[string]any{
			project("ARGY", "Argosy", "media", nil),
			project("CANT", "Catenary", "notifications", nil),
		}},
	})
	l, err := NewLive(srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}
	first, err := l.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if first.HasProject("CANT") {
		t.Fatal("the new project existed before it was created")
	}
	second, err := l.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !second.HasProject("CANT") {
		t.Fatal("a project created between runs is not a destination — the catalogue is cached across runs")
	}
}

// A catalogue that stopped at the first page is a router that cannot reach the
// projects at the end of the list: the same invisible error a stale catalogue
// causes, through a different door.
func TestPaginationIsFollowed(t *testing.T) {
	p1 := page{Items: []map[string]any{project("AAA", "A", "", nil)}}
	p1.Page.HasMore, p1.Page.NextCursor = true, "cur2"
	p2 := page{Items: []map[string]any{project("BBB", "B", "", nil)}}

	s, asked := fetch(t, []page{p1, p2})
	if len(s.Projects) != 2 || !s.HasProject("BBB") {
		t.Fatalf("projects = %+v, want both pages", s.Projects)
	}
	if len(*asked) != 2 || !strings.Contains((*asked)[1], "cursor=cur2") {
		t.Fatalf("requests = %v, want the second to carry the cursor", *asked)
	}
}

// Archived projects are excluded, and belt-and-braces: the endpoint omits them
// by default AND anything carrying archived_at is dropped here. A project
// nobody wants new tickets in must not be offered as a destination — the eval
// set has a fixture about exactly this bug happening elsewhere.
func TestArchivedAndDeletedProjectsAreNotDestinations(t *testing.T) {
	s, asked := fetch(t, onePage(
		project("LIVE", "Live", "", nil),
		project("OLD", "Archived", "", map[string]any{"archived_at": "2026-01-01T00:00:00Z"}),
		project("GONE", "Deleted", "", map[string]any{"deleted_at": "2026-01-01T00:00:00Z"}),
	))
	if s.HasProject("OLD") || s.HasProject("GONE") {
		t.Fatalf("projects = %+v, want only the live one", s.Projects)
	}
	// And the default is asked for, rather than include_archived being passed.
	for _, a := range *asked {
		if strings.Contains(a, "include_archived") {
			t.Errorf("asked for archived projects: %s", a)
		}
	}
}

// Order is fixed here rather than left to the endpoint: the salvage's list was
// "unfiltered and unordered", which makes the prompt vary between runs for no
// reason and puts a spurious difference in the catalogue hash.
func TestTheListIsOrderedSoTheHashIsStable(t *testing.T) {
	a, _ := fetch(t, onePage(project("ZZZ", "Z", "", nil), project("AAA", "A", "", nil)))
	b, _ := fetch(t, onePage(project("AAA", "A", "", nil), project("ZZZ", "Z", "", nil)))

	if a.Projects[0].Key != "AAA" {
		t.Errorf("order = %v, want sorted by key", a.Projects)
	}
	if a.SHA256() != b.SHA256() {
		t.Error("the same projects in a different order produced different hashes")
	}
}

// An empty catalogue is indistinguishable from a working one until you read the
// proposals: every TICKET lands needs_input with its project cleared, which
// reads as a router that cannot route rather than as a fetch that came back
// empty.
func TestAnEmptyProjectListIsRefusedRatherThanReturned(t *testing.T) {
	srv, _ := switchyard(t, onePage())
	l, err := NewLive(srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := l.Fetch(context.Background()); err == nil ||
		!strings.Contains(err.Error(), "no live projects") {
		t.Fatalf("err = %v, want a refusal naming the empty list", err)
	}
}

// A credential in an error is a credential in every aggregator that scrapes the
// logs.
func TestTheTokenIsNeverInAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	l, err := NewLive(srv.URL, "super-secret-token")
	if err != nil {
		t.Fatal(err)
	}
	_, err = l.Fetch(context.Background())
	if err == nil {
		t.Fatal("a 401 was accepted")
	}
	if strings.Contains(err.Error(), "super-secret-token") {
		t.Fatalf("the token is in the error: %v", err)
	}
}

func TestNewLiveRefusesAHalfConfiguration(t *testing.T) {
	for _, tc := range []struct{ name, url, token, want string }{
		{"no url", "", "t", "CHRONICLE_SWITCHYARD_URL"},
		{"bad url", "nope", "t", "absolute http(s) URL"},
		{"no token", "http://x:1", "", "CHRONICLE_SWITCHYARD_TOKEN"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := NewLive(tc.url, tc.token); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}

// The page half of CHRN-31's Done-when needs CHRN-37. Until then the tree is
// empty, which scribe.Reconcile treats as the ordinary case.
func TestThePageTreeIsEmptyUntilChrn37(t *testing.T) {
	s, _ := fetch(t, onePage(project("A", "A", "", nil)))
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
	s, _ := fetch(t, onePage(project("PRSR", "Purser", long, nil)))
	got := s.Projects[0].Description

	if len(got) > maxDescription {
		t.Fatalf("description is %d chars, over the %d budget: %q", len(got), maxDescription, got)
	}
	// The first sentence is the summary, so the cut lands there when it can.
	if got != "Purser is the cross-service provisioning and invite service for the Construct." {
		t.Errorf("description = %q, want the first sentence", got)
	}

	// With no sentence end, cut on a word boundary and say it was cut.
	nosentence := strings.Repeat("wibble ", 60)
	s2, _ := fetch(t, onePage(project("X", "X", nosentence, nil)))
	d := s2.Projects[0].Description
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
	l, err := NewLive(base, token)
	if err != nil {
		t.Fatal(err)
	}
	s, err := l.Fetch(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !s.HasProject("CHRN") {
		t.Errorf("the live catalogue has no CHRN: %v", s.Projects)
	}
	for _, p := range s.Projects {
		if len(p.Description) > maxDescription {
			t.Errorf("%s: description over budget (%d)", p.Key, len(p.Description))
		}
	}
	t.Logf("%d live projects, catalogue %s", len(s.Projects), s.SHA256()[:12])
	fmt.Fprintln(os.Stderr, s.RenderProjects())
}
