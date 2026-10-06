// Package mcp is Chronicle's MCP server (CHRN-65): one tool registry behind
// two transports that differ only in identity.
//
// IT IS A CLIENT OF CHRONICLE'S OWN HTTP API, and that is structural rather
// than a convention. This package imports internal/apiclient -- the Go client
// generated from openapi.yaml -- and may import neither internal/api nor
// internal/store; verify.sh's `mcp boundary` step and its mirror in ci.yml
// fail the build if it does. So every tool call reaches a handler, enforcement
// stays where it already is (the policy table and the database guards), and
// the processes this package runs hold no DSN and no database role.
//
// The two transports:
//
//   - stdio runs as the ONE account named by a token. This is the
//     agent-attributed path: an agent account, its own session token, and
//     whatever it writes is recorded as the agent's.
//   - hosted HTTP takes NO token. Each request carries a Cloudflare Access
//     assertion, verified here against this endpoint's own audience, and each
//     session exchanges it once for a short-lived session as that person.
//
// What a session may do is decided in one place, Identity.MayWrite, and
// applied in one place, BuildServer. A tool never tests the identity itself.
package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/apiclient"
)

// Transport is how a session reached this server.
type Transport string

const (
	TransportStdio  Transport = "stdio"
	TransportHosted Transport = "hosted"
)

// Account kinds, as the API reports them. Restated rather than imported: the
// constants live in internal/store, which this package may not reach.
const (
	KindPerson = "person"
	KindAgent  = "agent"
)

// Identity is who a session runs as and how it arrived. The transport builds
// one per session and hands it to BuildServer, which is how tool registration
// learns which kind of session it is registering for.
type Identity struct {
	Transport Transport
	// Kind is the account's kind as GET /auth/me reported it when the session
	// began. It is never taken from configuration or from anything the client
	// says about itself.
	Kind   string
	UserID uuid.UUID
	Name   string
}

// MayWrite reports whether this session may be given tools that change
// anything: a stdio session on an agent account, and nothing else.
//
// Both halves are rulings (CHRN-65). A hosted session acts as a person and
// would pass CH041's person check, so a hosted write would be indistinguishable
// from the owner's own -- hosted is read-only. And a person's token on stdio is
// the same authority arriving by another door, so it reads and does not write.
//
// This governs which tools a session is OFFERED. It is not the enforcement:
// the API authorizes every call on the credential it carries, whatever this
// package believed about it.
func (i Identity) MayWrite() bool {
	return i.Transport == TransportStdio && i.Kind == KindAgent
}

// Session is what a tool runs with: who it is, and the API as that account.
type Session struct {
	Identity Identity
	API      *apiclient.ClientWithResponses
}

// ErrUnauthorized means the API refused the session's credential.
var ErrUnauthorized = errors.New("the Chronicle API refused this credential")

// NewAPI builds an API client that presents token as the session credential
// on every request.
func NewAPI(baseURL, token string, httpClient *http.Client) (*apiclient.ClientWithResponses, error) {
	opts := []apiclient.ClientOption{
		apiclient.WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
			req.Header.Set("Authorization", "Bearer "+token)
			return nil
		}),
	}
	if httpClient != nil {
		opts = append(opts, apiclient.WithHTTPClient(httpClient))
	}
	return apiclient.NewClientWithResponses(baseURL, opts...)
}

// me asks the API who a client's credential belongs to.
func me(ctx context.Context, api *apiclient.ClientWithResponses) (*apiclient.User, error) {
	resp, err := api.GetMeWithResponse(ctx)
	if err != nil {
		return nil, fmt.Errorf("reach the Chronicle API: %w", err)
	}
	switch {
	case resp.JSON200 != nil:
		return resp.JSON200, nil
	case resp.StatusCode() == http.StatusUnauthorized:
		return nil, ErrUnauthorized
	default:
		return nil, fmt.Errorf("GET /auth/me answered %d", resp.StatusCode())
	}
}

// resolve builds the Identity for a session that arrived over transport, by
// asking the API whose credential the client carries.
func resolve(ctx context.Context, transport Transport, api *apiclient.ClientWithResponses) (Identity, error) {
	u, err := me(ctx, api)
	if err != nil {
		return Identity{}, err
	}
	if u.Kind != KindPerson && u.Kind != KindAgent {
		// An account kind this build does not know is not one to guess about:
		// MayWrite would answer false, but the session would still be offered
		// read tools as something it is not.
		return Identity{}, fmt.Errorf("account kind %q is not one this server knows", u.Kind)
	}
	return Identity{Transport: transport, Kind: u.Kind, UserID: u.Id, Name: u.DisplayName}, nil
}
