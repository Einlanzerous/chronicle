package resolve

import (
	"context"

	"github.com/Einlanzerous/chronicle/internal/markdown"
	"github.com/Einlanzerous/chronicle/internal/switchyard"
)

// ============================================================================
// THE TICKET TRACKER'S TRANSPORT. ONE CALL, NO OPINIONS.
// ============================================================================
//
// CHRN-51 split the seam here on purpose: "splitting it here is what stops two
// evidence-mode tickets from independently inventing the broken-versus-
// unreachable mapping". So everything below is plumbing, and every judgement
// about what an answer MEANS is classifySwitchyard's — including the one this
// ticket's Done-when turns on, that a deleted ticket is a fact about the
// referent and renders broken rather than vanishing.
//
// THE ESTATE COLOUR RULE IS NOT ENFORCED HERE, AND THAT IS NOT AN OVERSIGHT.
// Coral is a property of markdown.SystemSwitchyard, which the marker already
// carries as data-ref-system and the card's stylesheet keys off (CHRN-58). A
// colour chosen anywhere in this file would be a second place the estate rule
// lives, and internal/markdown states why that is the wrong shape: with fifteen
// live project keys, colour belongs to the system and never to the project.

// switchyardTransport is a Transport over a *switchyard.Client.
//
// UNEXPORTED, AND THE CONSTRUCTOR RETURNS THE INTERFACE. There is nothing here
// for a caller to configure — the deadline is the resolver's, the cache is the
// resolver's, and the classification is classifySwitchyard's — so a struct with
// exported fields would only offer places to disagree with CHRN-51.
type switchyardTransport struct{ c *switchyard.Client }

// NewSwitchyard is the ticket tracker's Transport, for Options.Transports under
// the key markdown.SystemSwitchyard.
//
// It takes a live client rather than a URL and a token: `serve` already builds
// one (cmd/chronicle/main.go), CHRN-33's triager already holds one, and a second
// construction would be a second place for the base URL to be parsed and the
// credential to be read.
//
// NOT REGISTERING IT AT ALL IS THE CONFIGURED-OFF CASE, and the only one. A
// Chronicle with no CHRONICLE_SWITCHYARD_URL leaves this system out of the map,
// and every ticket reference then answers StateUnconfigured — "this deployment
// has no credential for that upstream" — rather than the outage StateUnreachable
// would claim.
//
// So THE CLIENT MUST BE A REAL ONE. switchyard.New returns an error rather than
// a nil client for every configuration it refuses, so a caller that checked it
// cannot arrive here with nothing; a caller that passes nil anyway has asked for
// a third state that has no meaning, and would get a nil dereference inside a
// render rather than at boot.
func NewSwitchyard(c *switchyard.Client) Transport { return switchyardTransport{c: c} }

// Fetch makes exactly one GET /v1/tickets/{token}.
//
// THE TOKEN AS WRITTEN, NEVER A KEY REBUILT FROM Key AND Number. Switchyard's
// resolveTicket falls through to ticket_aliases after a move, so the written
// token is the only string that can find a ticket whose key has changed —
// GET /v1/tickets/IDEA-21 answers 200 with key CHRN-7, which is the very
// migration CHRN itself made. Sending a normalised spelling would break exactly
// the references most worth keeping alive.
//
// The error travels as Answer.Err and the status as Answer.Status, which is the
// distinction FetchTicket exists to preserve: a 404 is an answer.
func (t switchyardTransport) Fetch(ctx context.Context, ref markdown.Reference) Answer {
	status, body, err := t.c.FetchTicket(ctx, ref.Token)
	return Answer{Err: err, Status: status, Body: body}
}

// URLFor is the outbound arrow's target.
//
// BUILT FROM THE KEY THAT ANSWERED, because that is what classifySwitchyard
// passes: deep-linking the alias a reader wrote would send them to a redirect
// at best, and the card's job is to show the ticket that actually exists.
func (t switchyardTransport) URLFor(key string) string { return t.c.TicketURL(key) }

// SwitchyardProjectKeys adapts Projects to KeysOptions.Fetch.
//
// KeysOptions.Fetch is `func(ctx) ([]string, error)` so that internal/resolve
// ships no transport of its own; Client.Projects answers []Project, because the
// router (CHRN-31) needs each project's name and description to choose between
// them. The key set needs only the keys, and taking the wider type here would
// put a project's prose one field away from a render that has no use for it.
//
// A FETCH THAT FAILS IS THE CALLER'S TO ABSORB, not this function's: Keys.refresh
// keeps the last good set precisely so a blip cannot silently un-link every
// ticket reference in the corpus. Returning an empty slice on error here would
// defeat that by reporting success.
func SwitchyardProjectKeys(c *switchyard.Client) func(context.Context) ([]string, error) {
	return func(ctx context.Context) ([]string, error) {
		projects, err := c.Projects(ctx)
		if err != nil {
			return nil, err
		}
		keys := make([]string, 0, len(projects))
		for _, p := range projects {
			keys = append(keys, p.Key)
		}
		return keys, nil
	}
}
