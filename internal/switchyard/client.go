// Package switchyard is Chronicle's client for the estate's ticket tracker.
//
// TWO CALLERS, ONE CLIENT. CHRN-31 reads the live project list to render into
// the routing prompt; CHRN-35 creates a ticket when a memo routes to TICKET.
// They were briefly two clients with the same auth header, the same base-URL
// parsing and two spellings of the same error, which is how a service ends up
// with three ideas about what a 401 means.
//
// NOTHING FROM SWITCHYARD IS STORED. Invariant 2: a ticket resolves at render
// time and is never written into Chronicle's tables. This package returns a key
// and a URL — the handles a reference is resolved FROM — and deliberately does
// not return, or offer any way to fetch, a title or a status. There is no
// method here whose result could be cached into a column and go stale.
package switchyard

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultTimeout bounds one call. Switchyard is on the same docker network and
// answers in milliseconds; a slow answer is a problem rather than something to
// wait patiently through, and a triage batch should not stall on it.
const DefaultTimeout = 15 * time.Second

// Client talks to one Switchyard.
type Client struct {
	base  *url.URL
	token string
	http  *http.Client
}

// New validates the configuration without calling anything.
func New(baseURL, token string) (*Client, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, fmt.Errorf("switchyard: CHRONICLE_SWITCHYARD_URL is not set")
	}
	u, err := url.Parse(strings.TrimRight(strings.TrimSpace(baseURL), "/"))
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("switchyard: CHRONICLE_SWITCHYARD_URL %q is not an absolute http(s) URL", baseURL)
	}
	if strings.TrimSpace(token) == "" {
		return nil, fmt.Errorf("switchyard: CHRONICLE_SWITCHYARD_TOKEN is not set — every /v1 route answers 401 without it")
	}
	return &Client{base: u, token: token, http: &http.Client{Timeout: DefaultTimeout}}, nil
}

// BaseURL is where this client points, for building a link back.
func (c *Client) BaseURL() string { return c.base.String() }

// do sends one request and decodes the body into out.
//
// The token is never in an error, here or anywhere: a credential in an error is
// a credential in every aggregator that scrapes the logs.
func (c *Client) do(ctx context.Context, method, path string, body any, hdr map[string]string, out any) error {
	var rdr *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base.String()+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range hdr {
		req.Header.Set(k, v)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("switchyard: %s %s: %w", method, path, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 300 {
		return &Error{Status: resp.StatusCode, Method: method, Path: path}
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("switchyard: %s %s: decode: %w", method, path, err)
	}
	return nil
}

// Error is a non-2xx answer, carrying the status so a caller can tell a
// permanent refusal from something worth retrying.
type Error struct {
	Status int
	Method string
	Path   string
}

func (e *Error) Error() string {
	return fmt.Sprintf("switchyard: %s %s: %s", e.Method, e.Path, http.StatusText(e.Status))
}

// Retryable reports whether the call is worth repeating. 5xx and 429 are; a
// 4xx is the request being wrong, and repeating it wastes a batch's time to
// arrive at the same refusal.
func (e *Error) Retryable() bool {
	return e.Status >= 500 || e.Status == http.StatusTooManyRequests
}
