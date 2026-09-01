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

// IdempotencyKey is the key a create is deduplicated by, derived from the memo.
//
// DERIVED, NEVER RANDOM. A random key makes every retry a new ticket, which is
// precisely the failure this ticket exists to prevent — "a memo that creates
// SWY-231 and then gets replayed must return SWY-231, not create SWY-232".
// Keying on the memo also protects the likelier accident: the same memo
// submitted in two different batches, which a per-batch key would not catch.
func IdempotencyKey(memoID uuid.UUID) string {
	return "chronicle-memo-" + memoID.String()
}

// CreateTicket creates one ticket, idempotently on the memo.
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
		map[string]string{"Idempotency-Key": IdempotencyKey(in.MemoID)}, &t)
	if err != nil {
		return Ticket{}, err
	}
	if t.Key == "" {
		return Ticket{}, fmt.Errorf("switchyard: ticket created but no key came back, so nothing can link to it")
	}
	t.URL = c.base.String() + "/tickets/" + t.Key
	return t, nil
}
