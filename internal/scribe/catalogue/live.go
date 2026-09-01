package catalogue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Live reads the catalogue from Switchyard, which is CHRN-31's half of "read
// the live project list, do not hardcode it".
//
// A ROUTER WITH A STALE CATALOGUE routes today's memo into last month's
// structure, and the error is invisible because the destination it picked does
// exist — it is just the wrong one. That is the failure this type exists to
// prevent, and it is why there is no TTL here and no package-level cache: the
// Snapshot is the cache, it lives for one run, and nothing outlives the batch
// that fetched it.
//
// THE PAGE HALF IS NOT HERE. CHRN-31's second `Done when` — a page renamed in
// Chronicle is not offered under its old path — needs a page tree, and there is
// none until CHRN-37 in E5. Pages therefore come back empty, which
// scribe.Reconcile already treats as the ordinary case rather than a special
// one. Recorded on the ticket as a stop-and-ask rather than quietly reread.
type Live struct {
	base  *url.URL
	token string
	http  *http.Client
}

// DefaultTimeout bounds one catalogue fetch. Short on purpose: this runs before
// any routing, and a Switchyard that is slow to answer should fail the run
// quickly rather than half an hour into a batch.
const DefaultTimeout = 15 * time.Second

// maxDescription is the per-project prompt budget, and it is a NUMBER SOMEBODY
// CHOSE.
//
// The salvage cut descriptions at 150 characters, mid-word, with no ellipsis —
// not because 150 was right but because the prompt was assembled by string
// concatenation and nobody had a budget. Sixteen projects at this width is
// about 1 000 tokens of a 16 384 window, which is affordable; the first
// sentence of a Switchyard description is almost always the summary, so the cut
// lands on a sentence where it can and a word where it cannot.
const maxDescription = 240

// NewLive builds the client. It does not call Switchyard.
func NewLive(baseURL, token string) (*Live, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, fmt.Errorf("catalogue: CHRONICLE_SWITCHYARD_URL is not set, so the project list cannot be read")
	}
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("catalogue: CHRONICLE_SWITCHYARD_URL %q is not an absolute http(s) URL", baseURL)
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("catalogue: CHRONICLE_SWITCHYARD_TOKEN is not set — /v1/projects answers 401 without it")
	}
	return &Live{base: u, token: token, http: &http.Client{Timeout: DefaultTimeout}}, nil
}

type projectsPage struct {
	Items []struct {
		Key         string  `json:"key"`
		Name        string  `json:"name"`
		Description string  `json:"description"`
		ArchivedAt  *string `json:"archived_at"`
		DeletedAt   *string `json:"deleted_at"`
	} `json:"items"`
	Page struct {
		NextCursor string `json:"next_cursor"`
		HasMore    bool   `json:"has_more"`
	} `json:"page"`
}

// Fetch reads every live project, following pagination.
//
// PAGINATION IS FOLLOWED RATHER THAN ASSUMED AWAY. The endpoint answers a page
// at a time, and a catalogue that silently stopped at the first page would be a
// router that cannot route to the projects at the end of the list — the exact
// invisible error this type exists to prevent, arriving through the back door.
//
// ARCHIVED PROJECTS ARE EXCLUDED. The endpoint already omits them unless asked,
// and this asks for the default and then drops anything carrying archived_at or
// deleted_at anyway. Belt and braces, because a project that is archived is one
// nobody wants a new ticket in, and `switchyard-archived-project-picker.md` is a
// fixture in the eval set about precisely that bug happening elsewhere.
func (l *Live) Fetch(ctx context.Context) (*Snapshot, error) {
	s := &Snapshot{Version: 1, Pages: []string{}}
	seen := map[string]bool{}
	cursor := ""

	for page := 0; ; page++ {
		if page > 100 {
			return nil, fmt.Errorf("catalogue: /v1/projects did not stop paginating")
		}
		q := url.Values{}
		if cursor != "" {
			q.Set("cursor", cursor)
		}
		endpoint := l.base.String() + "/v1/projects"
		if len(q) > 0 {
			endpoint += "?" + q.Encode()
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+l.token)

		resp, err := l.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("catalogue: /v1/projects: %w", err)
		}
		var body projectsPage
		err = json.NewDecoder(resp.Body).Decode(&body)
		status := resp.StatusCode
		_ = resp.Body.Close()
		if status != http.StatusOK {
			// The token is never echoed, here or anywhere: a credential in an
			// error is a credential in every aggregator that scrapes the logs.
			return nil, fmt.Errorf("catalogue: /v1/projects: %s", http.StatusText(status))
		}
		if err != nil {
			return nil, fmt.Errorf("catalogue: /v1/projects: decode: %w", err)
		}

		for _, p := range body.Items {
			switch {
			case p.ArchivedAt != nil, p.DeletedAt != nil:
				continue
			case p.Key == "" || p.Name == "":
				continue
			case seen[p.Key]:
				continue
			}
			seen[p.Key] = true
			s.Projects = append(s.Projects, Project{
				Key: p.Key, Name: p.Name, Description: summarise(p.Description),
			})
		}
		if !body.Page.HasMore || body.Page.NextCursor == "" {
			break
		}
		cursor = body.Page.NextCursor
	}

	if len(s.Projects) == 0 {
		// Refused rather than returned. An empty catalogue is indistinguishable
		// from a working one until you look at the proposals: every TICKET
		// lands needs_input with its project cleared, which reads as a router
		// that cannot route rather than as a fetch that came back empty.
		return nil, fmt.Errorf("catalogue: /v1/projects returned no live projects, so every TICKET would answer with an empty project_key")
	}

	// ORDER IS FIXED HERE, not left to the endpoint. The salvage's list was
	// "unfiltered and unordered — whatever the endpoint returns, in whatever
	// order", which makes the prompt text vary between runs for no reason and
	// puts a spurious difference in the catalogue hash.
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
