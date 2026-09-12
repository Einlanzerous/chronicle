package resolve

import (
	"errors"
	"strings"
	"testing"

	"github.com/Einlanzerous/chronicle/internal/switchyard"
)

// timeoutError is a net.Error that timed out, which is one of the two shapes a
// transport failure arrives in and the one with a different sentence.
type timeoutError struct{}

func (timeoutError) Error() string   { return "context deadline exceeded" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

const ticketBody = `{"key":"SWY-389","id":"u","title":"A real ticket",` +
	`"status":{"category":"in_progress","display_name":"In Progress"}}`

// TestClassifySwitchyard is the Switchyard half of the plan's classification
// contract, over raw bytes with no network.
func TestClassifySwitchyard(t *testing.T) {
	for _, tc := range []struct {
		name    string
		answer  Answer
		state   State
		trips   bool
		explain string // substring
	}{
		{"200 with a ticket", Answer{Status: 200, Body: []byte(ticketBody)}, StateResolved, false, ""},
		{"404 is about the referent", Answer{Status: 404}, StateBroken, false, "no ticket with this key"},
		{"a switchyard.Error 404 is still an answer",
			Answer{Err: &switchyard.Error{Status: 404}}, StateBroken, false, "no ticket with this key"},
		{"a dial failure", Answer{Err: errors.New("dial tcp: connection refused")}, StateUnreachable, true, "could not be reached"},
		{"a timeout", Answer{Err: timeoutError{}}, StateUnreachable, true, "in the time allowed"},
		{"500 may be about one reference", Answer{Status: 500}, StateUnreachable, false, ""},
		{"502 may be about one reference", Answer{Status: 502}, StateUnreachable, false, ""},
		{"a 2xx that will not decode", Answer{Status: 200, Body: []byte("<html>nope")}, StateUnreachable, false, "could not read"},
		{"a 2xx with no key", Answer{Status: 200, Body: []byte(`{"title":"x"}`)}, StateUnreachable, false, "could not read"},
		{"401 names the credential", Answer{Status: 401}, StateUnreachable, true, "refused Chronicle's credential (401)"},
		{"403 names the credential", Answer{Status: 403}, StateUnreachable, true, "refused Chronicle's credential (403)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := classifySwitchyard("SWY-389", tc.answer, func(k string) string { return "https://sy/tickets/" + k })
			if got.state != tc.state {
				t.Errorf("state = %q, want %q", got.state, tc.state)
			}
			if got.trips != tc.trips {
				t.Errorf("trips = %v, want %v (explain %q)", got.trips, tc.trips, got.explain)
			}
			if tc.explain != "" && !strings.Contains(got.explain, tc.explain) {
				t.Errorf("explain = %q, want it to contain %q", got.explain, tc.explain)
			}
			// A refused credential must never read as a generic outage: that is
			// what sends somebody to check a service that is fine.
			if tc.trips && (tc.answer.Status == 401 || tc.answer.Status == 403) {
				if strings.Contains(got.explain, "did not answer") {
					t.Errorf("a 401 reads as a generic outage: %q", got.explain)
				}
			}
		})
	}
}

// TestClassifySwitchyardResolvedCarriesTheStatus checks the fields a coral card
// is built from.
func TestClassifySwitchyardResolvedCarriesTheStatus(t *testing.T) {
	got := classifySwitchyard("SWY-389", Answer{Status: 200, Body: []byte(ticketBody)},
		func(k string) string { return "https://sy/tickets/" + k })
	if got.upstream == nil {
		t.Fatal("resolved with no Upstream")
	}
	if got.upstream.Outcome != "in_progress" || got.upstream.DisplayName != "In Progress" {
		t.Errorf("status = %q/%q, want in_progress/In Progress", got.upstream.Outcome, got.upstream.DisplayName)
	}
	if got.upstream.Title != "A real ticket" {
		t.Errorf("title = %q", got.upstream.Title)
	}
	if got.upstream.URL != "https://sy/tickets/SWY-389" {
		t.Errorf("url = %q", got.upstream.URL)
	}
}

// TestAMovedTicketIsShownUnderTheKeyThatAnswered.
//
// resolveTicket falls through to ticket_aliases after a move, so a request for
// IDEA-21 can answer 200 with key CHRN-7 -- and CHRN itself graduated from
// IDEA-21. Showing the written token beside that ticket's status, or building
// the deep link from the alias, is the identity form of the same failure a
// stale status is about age.
func TestAMovedTicketIsShownUnderTheKeyThatAnswered(t *testing.T) {
	body := `{"key":"CHRN-7","title":"E7 — References: link, never copy",` +
		`"status":{"category":"in_progress","display_name":"In Progress"}}`
	got := classifySwitchyard("IDEA-21", Answer{Status: 200, Body: []byte(body)},
		func(k string) string { return "https://sy/tickets/" + k })

	if got.upstream == nil {
		t.Fatal("resolved with no Upstream")
	}
	if got.upstream.Key != "CHRN-7" {
		t.Errorf("Upstream.Key = %q, want the key that ANSWERED (CHRN-7)", got.upstream.Key)
	}
	if got.upstream.URL != "https://sy/tickets/CHRN-7" {
		t.Errorf("url = %q, want it built from the answered key, not the alias", got.upstream.URL)
	}
}

// TestASwitchyardBrokenOffersNothingToClick: an outbound arrow onto a 404 is a
// worse link than none, and Switchyard publishes no vocabulary member meaning
// "deleted", so Chronicle does not mint one in its namespace.
func TestASwitchyardBrokenOffersNothingToClick(t *testing.T) {
	got := classifySwitchyard("SWY-99999", Answer{Status: 404},
		func(k string) string { return "https://sy/tickets/" + k })
	if got.upstream == nil {
		t.Fatal("broken with no Upstream: the upstream answered, so it must be present")
	}
	if got.upstream.URL != "" {
		t.Errorf("url = %q, want empty", got.upstream.URL)
	}
	if got.upstream.Outcome != "" {
		t.Errorf("outcome = %q, want empty: Switchyard has no member meaning deleted", got.upstream.Outcome)
	}
	if got.upstream.Key != "SWY-99999" {
		t.Errorf("key = %q, want the key as written", got.upstream.Key)
	}
}

func citeBody(outcome, explain string, recoverable bool) []byte {
	r := "false"
	if recoverable {
		r = "true"
	}
	return []byte(`{"ref":"amber1.a.b.0","outcome":"` + outcome + `","explain":"` + explain +
		`","recoverable":` + r + `,"objects_read":1}`)
}

// TestClassifyAmberFollowsTheStatedDecodeOrder walks every row of the order in
// the plan's contract.
//
// The reason the order exists at all: Amber answers its broken outcomes as
// non-2xx WITH the cite body (404/409/410), and answers {"error": …} with NO
// outcome for 400, 401 and 503. "Decide from the outcome, never from the
// status" is true of the first group and says nothing about the second.
func TestClassifyAmberFollowsTheStatedDecodeOrder(t *testing.T) {
	for _, tc := range []struct {
		name    string
		answer  Answer
		state   State
		outcome string
		trips   bool
		explain string
	}{
		{"1 transport failure", Answer{Err: errors.New("connection refused")}, StateUnreachable, "", true, "could not be reached"},
		{"2 not JSON", Answer{Status: 200, Body: []byte("<html>")}, StateUnreachable, "", false, "non-JSON"},
		{"3 400 is a malformed reference", Answer{Status: 400, Body: []byte(`{"error":"amber1: bad reference"}`)},
			StateBroken, amberOutcomeMalformed, false, "bad reference"},
		{"4 401 names the credential", Answer{Status: 401, Body: []byte(`{"error":"missing or invalid bearer token"}`)},
			StateUnreachable, "", true, "refused Chronicle's credential (401)"},
		{"4 403 names the credential", Answer{Status: 403, Body: []byte(`{"error":"forbidden"}`)},
			StateUnreachable, "", true, "refused Chronicle's credential (403)"},
		{"5 503 no archive open", Answer{Status: 503, Body: []byte(`{"error":"citation resolution unavailable: no archive is open"}`)},
			StateUnreachable, "", true, "no archive is open"},
		{"6 held at 200", Answer{Status: 200, Body: citeBody("held", "in the archive", false)},
			StateResolved, "held", false, "in the archive"},
		{"6 not_captured at 404", Answer{Status: 404, Body: citeBody("not_captured", "never held", false)},
			StateBroken, "not_captured", false, "never held"},
		{"6 block_out_of_range at 404", Answer{Status: 404, Body: citeBody("block_out_of_range", "no such block", false)},
			StateBroken, "block_out_of_range", false, "no such block"},
		{"6 prompt_only at 410", Answer{Status: 410, Body: citeBody("prompt_only", "prompt survived", false)},
			StateBroken, "prompt_only", false, "prompt survived"},
		{"6 not_in_capture at 410", Answer{Status: 410, Body: citeBody("not_in_capture", "sweep beat the ingest", true)},
			StateBroken, "not_in_capture", false, "sweep beat"},
		{"6 ambiguous at 409", Answer{Status: 409, Body: citeBody("ambiguous", "two candidates", true)},
			StateBroken, "ambiguous", false, "two candidates"},
		{"7 another error status keeps Amber's words",
			Answer{Status: 500, Body: []byte(`{"error":"cannot resolve citation"}`)},
			StateUnreachable, "", false, "cannot resolve citation"},
		{"8 JSON and neither shape", Answer{Status: 200, Body: []byte(`{"something":1}`)},
			StateUnreachable, "", false, "could not read"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyAmber("amber1.a.b.0", tc.answer, nil)
			if got.state != tc.state {
				t.Errorf("state = %q, want %q", got.state, tc.state)
			}
			if got.trips != tc.trips {
				t.Errorf("trips = %v, want %v", got.trips, tc.trips)
			}
			if tc.outcome == "" {
				if got.upstream != nil {
					t.Errorf("Upstream is present on a state where nobody answered about the referent")
				}
			} else {
				if got.upstream == nil {
					t.Fatalf("no Upstream, want outcome %q", tc.outcome)
				}
				if got.upstream.Outcome != tc.outcome {
					t.Errorf("outcome = %q, want %q", got.upstream.Outcome, tc.outcome)
				}
			}
			if tc.explain != "" && !strings.Contains(got.explain, tc.explain) {
				t.Errorf("explain = %q, want it to contain %q", got.explain, tc.explain)
			}
		})
	}
}

// TestAmberOutcomesDoNotCollapse: AMBR-11 published a vocabulary instead of a
// 404 so that "Amber never held this session" and "the transcript aged out but
// the typed prompt survived" could be told apart. Rendering them identically
// throws that away, and prompt_only is the COMMON case -- 111 of 200 sampled
// sessions -- not a corner.
func TestAmberOutcomesDoNotCollapse(t *testing.T) {
	outcomes := map[string]struct {
		status      int
		recoverable bool
	}{
		"held":               {200, false},
		"not_captured":       {404, false},
		"block_out_of_range": {404, false},
		"prompt_only":        {410, false},
		"not_in_capture":     {410, true},
		"ambiguous":          {409, true},
	}
	seen := map[string]string{}
	for outcome, spec := range outcomes {
		got := classifyAmber("amber1.a.b.0",
			Answer{Status: spec.status, Body: citeBody(outcome, "explain for "+outcome, spec.recoverable)}, nil)
		if got.upstream == nil {
			t.Fatalf("%s: no Upstream", outcome)
		}
		if got.upstream.Outcome != outcome {
			t.Errorf("%s: outcome relayed as %q", outcome, got.upstream.Outcome)
		}
		if got.upstream.Recoverable != spec.recoverable {
			t.Errorf("%s: recoverable = %v, want %v (relayed verbatim)", outcome, got.upstream.Recoverable, spec.recoverable)
		}
		if got.explain != "explain for "+outcome {
			t.Errorf("%s: explain = %q, want Amber's own sentence", outcome, got.explain)
		}
		fingerprint := string(got.state) + "|" + got.upstream.Outcome + "|" + got.explain
		if other, dup := seen[fingerprint]; dup {
			t.Errorf("%s and %s produce the same payload", outcome, other)
		}
		seen[fingerprint] = outcome
	}

	// recoverable is meaningful only on broken: held is a constant false, so a
	// card reading it on resolved learns nothing.
	held := classifyAmber("amber1.a.b.0", Answer{Status: 200, Body: citeBody("held", "x", false)}, nil)
	if held.upstream.Recoverable {
		t.Error("held came back recoverable; cite.Describe() makes it a constant false")
	}
}

// TestAnUnknownAmberOutcomeReadsAsBroken: relaying outcomes as opaque strings
// is what keeps a tenth member upstream from being a compile error here, and
// broken is the safe direction to guess in -- it says there is something to
// know rather than that all is well.
func TestAnUnknownAmberOutcomeReadsAsBroken(t *testing.T) {
	got := classifyAmber("amber1.a.b.0", Answer{Status: 200, Body: citeBody("held_partial", "a tenth member", false)}, nil)
	if got.state != StateBroken {
		t.Errorf("state = %q, want broken", got.state)
	}
	if got.upstream == nil || got.upstream.Outcome != "held_partial" {
		t.Error("an unknown outcome is not relayed verbatim")
	}
}

// TestTheBreakerTripsOnWhatCannotDiffer is the rule, named.
//
//	TRIP WHEN THE ANSWER CANNOT BE DIFFERENT FOR THE NEXT REFERENCE.
//
// A dial failure, a timeout, a refused credential and a missing archive are
// claims about the SERVICE. A 500, a 502 and a body that will not decode may be
// claims about ONE reference, so the rest of the page is still attempted.
func TestTheBreakerTripsOnWhatCannotDiffer(t *testing.T) {
	cannotDiffer := []struct {
		name string
		got  classified
	}{
		{"switchyard dial failure", classifySwitchyard("SWY-1", Answer{Err: errors.New("refused")}, nil)},
		{"switchyard timeout", classifySwitchyard("SWY-1", Answer{Err: timeoutError{}}, nil)},
		{"switchyard 401", classifySwitchyard("SWY-1", Answer{Status: 401}, nil)},
		{"switchyard 403", classifySwitchyard("SWY-1", Answer{Status: 403}, nil)},
		{"amber dial failure", classifyAmber("amber1.a.b.0", Answer{Err: errors.New("refused")}, nil)},
		{"amber 401", classifyAmber("amber1.a.b.0", Answer{Status: 401, Body: []byte(`{"error":"no token"}`)}, nil)},
		{"amber 503 no archive", classifyAmber("amber1.a.b.0",
			Answer{Status: 503, Body: []byte(`{"error":"no archive is open"}`)}, nil)},
	}
	for _, tc := range cannotDiffer {
		if !tc.got.trips {
			t.Errorf("%s: did not trip, but the answer is identical for every reference on the page", tc.name)
		}
	}

	couldDiffer := []struct {
		name string
		got  classified
	}{
		{"switchyard 500", classifySwitchyard("SWY-1", Answer{Status: 500}, nil)},
		{"switchyard 502", classifySwitchyard("SWY-1", Answer{Status: 502}, nil)},
		{"switchyard undecodable 2xx", classifySwitchyard("SWY-1", Answer{Status: 200, Body: []byte("{")}, nil)},
		{"switchyard 404", classifySwitchyard("SWY-1", Answer{Status: 404}, nil)},
		{"amber undecodable", classifyAmber("amber1.a.b.0", Answer{Status: 200, Body: []byte("{")}, nil)},
		{"amber 500 with a message", classifyAmber("amber1.a.b.0",
			Answer{Status: 500, Body: []byte(`{"error":"cannot resolve citation"}`)}, nil)},
		{"amber not_captured", classifyAmber("amber1.a.b.0",
			Answer{Status: 404, Body: citeBody("not_captured", "x", false)}, nil)},
	}
	for _, tc := range couldDiffer {
		if tc.got.trips {
			t.Errorf("%s: tripped, but it may be a claim about this one reference", tc.name)
		}
	}
}

// TestExplainNeverCarriesARelativeTime.
//
// "did not answer 4 s ago" is true at serialisation and false in the hands of a
// client holding the payload -- an age with no timestamp beside it, which is
// the defect this package refuses for values. The operator's instant lives in
// the log line instead.
func TestExplainNeverCarriesARelativeTime(t *testing.T) {
	banned := []string{" ago", "seconds ago", "just now"}
	all := []classified{
		classifySwitchyard("SWY-1", Answer{Err: errors.New("refused")}, nil),
		classifySwitchyard("SWY-1", Answer{Status: 404}, nil),
		classifySwitchyard("SWY-1", Answer{Status: 401}, nil),
		classifyAmber("amber1.a.b.0", Answer{Err: timeoutError{}}, nil),
		classifyAmber("amber1.a.b.0", Answer{Status: 503, Body: []byte(`{"error":"no archive is open"}`)}, nil),
	}
	for _, c := range all {
		for _, b := range banned {
			if strings.Contains(c.explain, b) {
				t.Errorf("explain %q carries a relative time", c.explain)
			}
		}
	}
}
