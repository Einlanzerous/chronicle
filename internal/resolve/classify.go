package resolve

import (
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"

	"github.com/Einlanzerous/chronicle/internal/switchyard"
)

// ============================================================================
// CLASSIFICATION LIVES HERE. TRANSPORT LIVES IN CHRN-49 AND CHRN-50.
// ============================================================================
//
// Broken-versus-unreachable is this ticket's doctrine, and these two functions
// are the whole of it: pure, no network, no clock, table-tested against
// fabricated bytes. Deciding it inside the two transport tickets would mean two
// independently invented mappings of the one thing CHRN-51 exists to decide.
//
// ============================================================================
// WHAT TRIPS THE BREAKER, AS A RULE RATHER THAN A LIST OF CODES.
// ============================================================================
//
//	TRIP WHEN THE ANSWER CANNOT BE DIFFERENT FOR THE NEXT REFERENCE.
//
// A dial failure, a refused credential and a missing archive are claims about
// the SERVICE: identical for all thirty references on the page, so paying for
// them thirty times is pure noise. A 500, a 502 and a body that will not decode
// may be claims about ONE reference -- Switchyard's own comment, on a 500 for a
// single unreadable archive object: it "costs milliseconds and says nothing
// about the next reference" -- so they do not trip, and the cost of being wrong
// about that is bounded by the budget and the cap.
//
// This widens Switchyard's rule, where only a transport failure trips. The
// credential and no-archive cases are added because both are permanent until
// somebody redeploys and neither is discoverable without dialling.

// classified is one classifier's verdict.
type classified struct {
	state    State
	upstream *Upstream
	explain  string

	// trips says the answer was about the service rather than about this
	// reference, so the rest of the page should not pay for it again.
	trips bool
}

// classifySwitchyard decides one ticket answer.
//
// urlFor may be nil. token is the key AS WRITTEN, which is what a Broken
// carries: on a 404 nothing answered with a key of its own.
func classifySwitchyard(token string, a Answer, urlFor func(string) string) classified {
	status, body, transport := unwrap(a)
	if transport != nil {
		return classified{state: StateUnreachable, explain: transportExplain("the ticket tracker", transport), trips: true}
	}

	switch {
	case status == http.StatusNotFound:
		// A fact about the referent: deleted, or a key that never existed.
		// Switchyard soft-deletes and its lookups exclude deleted rows, so a
		// deletion arrives here with no special case.
		//
		// NO URL. An outbound arrow onto a 404 is a worse link than none.
		return classified{
			state:    StateBroken,
			upstream: &Upstream{Key: token},
			explain:  "the ticket tracker has no ticket with this key — it was deleted, or it never existed",
		}

	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		// PERMANENT UNTIL A REDEPLOY, and the remedy belongs on the card: a
		// generic "did not answer" sends somebody to check a service that is
		// fine, which is the same mistake Unconfigured exists to avoid.
		return classified{
			state:   StateUnreachable,
			explain: "the ticket tracker refused Chronicle's credential (" + strconv.Itoa(status) + ")",
			trips:   true,
		}

	case status >= 300:
		return classified{
			state:   StateUnreachable,
			explain: upstreamSentence("the ticket tracker", body, status),
		}
	}

	var t struct {
		Key    string `json:"key"`
		Title  string `json:"title"`
		Status struct {
			Category    string `json:"category"`
			DisplayName string `json:"display_name"`
		} `json:"status"`
	}
	if err := json.Unmarshal(body, &t); err != nil || t.Key == "" {
		// It answered -- badly, but it answered, and in milliseconds. Nothing
		// about the next reference is predicted by this one, so it does not
		// trip.
		return classified{
			state:   StateUnreachable,
			explain: "the ticket tracker returned an answer Chronicle could not read",
		}
	}

	// THE KEY AS ANSWERED, NOT AS WRITTEN, and the URL is built from it.
	// resolveTicket falls through to ticket_aliases after a move, so
	// GET /v1/tickets/IDEA-21 answers 200 with key CHRN-7. Showing the written
	// token beside that ticket's status, or deep-linking the alias, is the same
	// doctrine failing about identity that a stale status fails about age.
	u := &Upstream{
		Key:         t.Key,
		Outcome:     t.Status.Category,
		DisplayName: t.Status.DisplayName,
		Title:       t.Title,
	}
	if urlFor != nil {
		u.URL = urlFor(t.Key)
	}
	return classified{state: StateResolved, upstream: u}
}

// amberOutcomeHeld is the one cite outcome that means the evidence is there.
const amberOutcomeHeld = "held"

// amberOutcomeMalformed is Switchyard's name for Amber's 400, borrowed rather
// than reinvented: CitationOutcome's ninth member, and a fact about the
// reference rather than about the archive.
const amberOutcomeMalformed = "malformed"

// classifyAmber decides one citation answer, IN A STATED ORDER.
//
// ============================================================================
// AMBER CANNOT BE CLASSIFIED BY STATUS, AND IT CANNOT BE CLASSIFIED BY BODY
// ALONE EITHER.
// ============================================================================
//
// Amber answers its broken outcomes as non-2xx WITH the cite body --
// cite.Outcome.Status() maps held to 200, not_captured and block_out_of_range
// to 404, prompt_only and not_in_capture to 410, ambiguous to 409, and
// handleCite writes the whole citeResponse at that status. A client mirroring
// internal/switchyard, which discards the body on anything >= 300, would call
// all five a transport error and never see an outcome.
//
// But Amber ALSO answers {"error": …} with no outcome at all -- writeError, for
// an unparseable reference (400), a missing or invalid bearer token (401) and
// "citation resolution unavailable: no archive is open" (503). So "decide from
// the outcome, never from the status" is too simple: those three decode as
// nothing, and a rule that said both "400 is broken" and "a body that does not
// decode is unreachable" would disagree with itself about every one of them.
//
// The order below is Switchyard's resolveTraced, stated rather than
// reimplemented from memory.
func classifyAmber(token string, a Answer, urlFor func(string) string) classified {
	status, body, transport := unwrap(a)

	// 1. No usable response at all.
	if transport != nil {
		return classified{state: StateUnreachable, explain: transportExplain("the capture archive", transport), trips: true}
	}

	// 2. Not JSON. "Amber answered -- badly, but it answered, and in
	//    milliseconds", so it says nothing about the next reference.
	var probe struct {
		Error       string `json:"error"`
		Outcome     string `json:"outcome"`
		Explain     string `json:"explain"`
		Recoverable bool   `json:"recoverable"`
	}
	if err := json.Unmarshal(body, &probe); err != nil {
		return classified{state: StateUnreachable, explain: "the capture archive returned a non-JSON response"}
	}

	if probe.Outcome == "" && probe.Error != "" {
		switch status {
		// 3. Amber refusing to parse the reference. A fact about the referent.
		case http.StatusBadRequest:
			return classified{
				state:    StateBroken,
				upstream: &Upstream{Outcome: amberOutcomeMalformed},
				explain:  probe.Error,
			}

		// 4. A refused credential, symmetric with the Switchyard side. Amber
		//    gates all of /v1 behind one shared token, which Chronicle is the
		//    third consumer of.
		case http.StatusUnauthorized, http.StatusForbidden:
			return classified{
				state:   StateUnreachable,
				explain: "the capture archive refused Chronicle's credential (" + strconv.Itoa(status) + ")",
				trips:   true,
			}

		// 5. No archive open. A claim about the service, identical for every
		//    reference on the page, so it trips even though it is a 5xx.
		case http.StatusServiceUnavailable:
			return classified{state: StateUnreachable, explain: probe.Error, trips: true}

		// 7. Anything else that named itself. Amber's own sentence beats
		//    "unexpected shape", and it does not trip: this may be about one
		//    reference.
		default:
			return classified{state: StateUnreachable, explain: probe.Error}
		}
	}

	// 6. A cite answer, decided by its outcome whatever the status.
	if probe.Outcome != "" {
		u := &Upstream{Outcome: probe.Outcome, Recoverable: probe.Recoverable}
		if urlFor != nil {
			u.URL = urlFor(token)
		}
		state := StateBroken
		if probe.Outcome == amberOutcomeHeld {
			state = StateResolved
		}
		// AN OUTCOME CHRONICLE DOES NOT KNOW READS AS BROKEN, and carries its
		// own name and sentence verbatim. Relaying them as opaque strings is
		// what keeps a tenth member upstream from being a compile error here;
		// treating an unknown one as Resolved would be the confident direction
		// to guess in.
		return classified{state: state, upstream: u, explain: probe.Explain}
	}

	// 8. JSON, and neither shape.
	return classified{state: StateUnreachable, explain: "the capture archive returned an answer Chronicle could not read"}
}

// unwrap separates a genuine transport failure from an answer that happens to
// carry a non-2xx status.
//
// A *switchyard.Error IS AN ANSWER. The client turns every status >= 300 into
// one, so a caller handing it straight through would report a 404 -- a fact
// about the referent -- as an outage.
func unwrap(a Answer) (status int, body []byte, transport error) {
	if a.Err == nil {
		return a.Status, a.Body, nil
	}
	var swErr *switchyard.Error
	if errors.As(a.Err, &swErr) {
		return swErr.Status, a.Body, nil
	}
	return 0, nil, a.Err
}

// transportExplain says which kind of not-answering it was, because the two
// have different remedies and only the log will ever carry the detail.
func transportExplain(name string, err error) string {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return name + " did not answer in the time allowed for one reference"
	}
	return name + " could not be reached"
}

// upstreamSentence prefers the upstream's own words for an error status.
func upstreamSentence(name string, body []byte, status int) string {
	var e struct {
		Error   string `json:"error"`
		Message string `json:"message"`
		Detail  string `json:"detail"`
	}
	if json.Unmarshal(body, &e) == nil {
		for _, s := range []string{e.Message, e.Error, e.Detail} {
			if s != "" {
				return s
			}
		}
	}
	return name + " answered " + strconv.Itoa(status)
}
