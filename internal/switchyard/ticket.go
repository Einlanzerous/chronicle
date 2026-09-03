package switchyard

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// NewTicket is what a memo becomes when it routes to TICKET.
//
// The fields are exactly CHRN-32's TICKET payload plus the memo it came from.
// Nothing else: this is not a general ticket-creation API and should not grow
// into one, because every field added here is a field Chronicle has an opinion
// about and Switchyard already owns.
type NewTicket struct {
	ProjectKey  string
	Type        string
	Title       string
	Description string

	// IdempotencyKey deduplicates a retry of THIS DECISION.
	//
	// SUPPLIED, NOT DERIVED, and the reason is a trap CHRN-33's plan found in
	// review. Switchyard caches every response below 500 that has a JSON body,
	// and it renders every error as JSON — so a 4xx is cached under whatever
	// key it was sent with, for 24 hours. A key derived from the memo would
	// therefore poison that memo: a create refused because its project was
	// archived would replay the cached 404 for every corrected decision the
	// operator made for the rest of the day, with nothing saying why.
	//
	// A key that belongs to the DECISION does not have that problem. A retry of
	// the same decision re-sends the same stored key and replays; a new
	// decision carries a new key and reaches Switchyard. The caller keeps it
	// beside its own durable record — for Chronicle that is CHRN-33's link row.
	//
	// It must still be STABLE ACROSS RETRIES rather than freshly random per
	// call, or every retry creates a ticket. That property now comes from being
	// stored rather than from being computed.
	IdempotencyKey string

	// MemoID is the provenance, and it is not optional.
	//
	// "The created ticket records where it came from (the memo, and Chronicle)
	// so the trail back to the original voice note is not lost the moment the
	// ticket is edited." A ticket that cannot be traced back to its recording
	// is a ticket whose context died with the triage screen.
	MemoID uuid.UUID
}

// Ticket is the handle a reference resolves FROM.
//
// A key and a URL, and deliberately nothing else. Returning the title or the
// status would put a copy of upstream state one assignment away from a column,
// and invariant 2's whole argument is that the copy goes stale silently.
type Ticket struct {
	Key string `json:"key"`
	ID  string `json:"id"`
	// URL is built here rather than parsed out, so a caller that wants to deep
	// link does not have to know Switchyard's routing.
	URL string `json:"-"`
}

// MetadataSource is the value Chronicle stamps so a person reading the ticket
// can tell where it came from without knowing the memo id means anything.
const MetadataSource = "chronicle"

// NewIdempotencyKey mints a key for one decision.
//
// A HELPER, NOT A DERIVATION. Callers store what this returns beside the
// decision it belongs to and re-send that stored value on every retry — see
// NewTicket.IdempotencyKey for why the key must not be a function of the memo.
func NewIdempotencyKey() string {
	return "chronicle-" + uuid.NewString()
}

// CreateTicket creates one ticket under the caller's idempotency key.
//
// THE HEADER REPLAYS A RESPONSE. IT DOES NOT SERIALISE A SIDE EFFECT, and a
// caller that mistakes the one for the other will create duplicate tickets.
//
// Switchyard's middleware (server/src/lib/idempotency.ts) is: look the key up,
// `await next()`, insert the cache row. There is no lock between the lookup and
// the handler, and the insert's own catch says so — "Concurrent same-key
// request landed first — that's fine, the other one wrote the cache entry."
// Fine for the cache; the handler has already run twice, so TWO TICKETS EXIST.
//
// So two overlapping requests for one memo duplicate, and that is the ordinary
// retry rather than an exotic race: a phone that gives up before this client's
// 15-second timeout and re-sends is exactly the case. The 24-hour TTL is a
// SEPARATE and weaker limit — outside it even a sequential replay re-creates.
//
// CALLERS MUST SERIALISE PER MEMO THEMSELVES. In Chronicle that is CHRN-33's
// pending-link row with UNIQUE (memo_id), taken before the outward call; this
// package cannot do it, because the only thing that can is a lock next to the
// durable record. Said here, in the function that would cause it, rather than
// only in a plan somebody has to find.
//
// The same record is where the idempotency key lives — see
// NewTicket.IdempotencyKey.
func (c *Client) CreateTicket(ctx context.Context, in NewTicket) (Ticket, error) {
	switch {
	case strings.TrimSpace(in.ProjectKey) == "":
		// The contract already refuses to guess a project, and a guessed
		// project_key is immutable after creation. Refusing here too means a
		// needs_input proposal cannot become a permanently misfiled ticket by
		// way of a caller that forgot to check.
		return Ticket{}, fmt.Errorf("switchyard: no project key — a ticket cannot be created for a proposal that needs input")
	case strings.TrimSpace(in.Title) == "":
		return Ticket{}, fmt.Errorf("switchyard: no title")
	case in.MemoID == uuid.Nil:
		return Ticket{}, fmt.Errorf("switchyard: no memo id — a ticket with no trail back to its recording is what this provenance exists to prevent")
	case strings.TrimSpace(in.IdempotencyKey) == "":
		// Refused rather than defaulted. A create with no key is one that
		// duplicates on every retry, and inventing one here would make it
		// fresh per call — the same failure with better manners.
		return Ticket{}, fmt.Errorf("switchyard: no idempotency key — the caller stores one beside its durable record and re-sends it (CHRN-33's link row)")
	}

	body := map[string]any{
		"project_key": in.ProjectKey,
		"type":        in.Type,
		"title":       in.Title,
		"metadata": map[string]any{
			"source":              MetadataSource,
			"chronicle_memo_id":   in.MemoID.String(),
			"chronicle_routed_by": "scribe",
		},
	}
	if in.Description != "" {
		body["description"] = in.Description
	}

	var t Ticket
	err := c.do(ctx, "POST", "/v1/tickets", body,
		map[string]string{"Idempotency-Key": in.IdempotencyKey}, &t)
	if err != nil {
		return Ticket{}, err
	}
	if t.Key == "" {
		return Ticket{}, fmt.Errorf("switchyard: ticket created but no key came back, so nothing can link to it")
	}
	t.URL = c.base.String() + "/tickets/" + t.Key
	return t, nil
}
