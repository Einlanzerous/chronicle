package switchyard

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func proj(key, name, desc string, extra map[string]any) map[string]any {
	m := map[string]any{"key": key, "name": name, "description": desc,
		"archived_at": nil, "deleted_at": nil}
	for k, v := range extra {
		m[k] = v
	}
	return m
}

// server answers /v1/projects a page at a time and records what it was asked.
func server(t *testing.T, pages []page) (*Client, *[]string) {
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
	c, err := New(s.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}
	return c, &asked
}

func onePage(items ...map[string]any) []page { return []page{{Items: items}} }

// A caller that stopped at the first page is a router that cannot reach the
// projects at the end of the list.
func TestProjectsFollowsPagination(t *testing.T) {
	p1 := page{Items: []map[string]any{proj("AAA", "A", "", nil)}}
	p1.Page.HasMore, p1.Page.NextCursor = true, "cur2"
	p2 := page{Items: []map[string]any{proj("BBB", "B", "", nil)}}

	c, asked := server(t, []page{p1, p2})
	ps, err := c.Projects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 2 {
		t.Fatalf("projects = %+v, want both pages", ps)
	}
	if len(*asked) != 2 || !strings.Contains((*asked)[1], "cursor=cur2") {
		t.Fatalf("requests = %v, want the second to carry the cursor", *asked)
	}
}

// A project nobody wants new tickets in must not be offered as a destination.
// The endpoint omits them by default AND they are dropped here.
func TestProjectsExcludesArchivedAndDeleted(t *testing.T) {
	c, asked := server(t, onePage(
		proj("LIVE", "Live", "", nil),
		proj("OLD", "Archived", "", map[string]any{"archived_at": "2026-01-01T00:00:00Z"}),
		proj("GONE", "Deleted", "", map[string]any{"deleted_at": "2026-01-01T00:00:00Z"}),
	))
	ps, err := c.Projects(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 1 || ps[0].Key != "LIVE" {
		t.Fatalf("projects = %+v, want only the live one", ps)
	}
	for _, a := range *asked {
		if strings.Contains(a, "include_archived") {
			t.Errorf("asked for archived projects: %s", a)
		}
	}
}

// A credential in an error is a credential in every aggregator that scrapes the
// logs.
func TestTheTokenIsNeverInAnError(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(s.Close)
	c, err := New(s.URL, "super-secret-token")
	if err != nil {
		t.Fatal(err)
	}
	_, err = c.Projects(context.Background())
	if err == nil {
		t.Fatal("a 401 was accepted")
	}
	if strings.Contains(err.Error(), "super-secret-token") {
		t.Fatalf("the token is in the error: %v", err)
	}
}

// A 4xx is the request being wrong; repeating it wastes a batch's time to
// arrive at the same refusal.
func TestOnlyServerFailuresAreWorthRetrying(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   bool
	}{
		{http.StatusBadRequest, false},
		{http.StatusUnauthorized, false},
		{http.StatusConflict, false},
		{http.StatusTooManyRequests, true},
		{http.StatusInternalServerError, true},
		{http.StatusBadGateway, true},
	} {
		e := &Error{Status: tc.status, Method: "POST", Path: "/v1/tickets"}
		if got := e.Retryable(); got != tc.want {
			t.Errorf("%d retryable = %v, want %v", tc.status, got, tc.want)
		}
	}
}

func TestNewRefusesAHalfConfiguration(t *testing.T) {
	for _, tc := range []struct{ name, url, token, want string }{
		{"no url", "", "t", "CHRONICLE_SWITCHYARD_URL"},
		{"bad url", "nope", "t", "absolute http(s) URL"},
		{"wrong scheme", "ftp://x", "t", "absolute http(s) URL"},
		{"no token", "http://x:1", "", "CHRONICLE_SWITCHYARD_TOKEN"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := New(tc.url, tc.token); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want %q", err, tc.want)
			}
		})
	}
}
