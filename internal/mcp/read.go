package mcp

import (
	"fmt"
	"net/http"
	"unicode/utf8"
)

// The plumbing the read tools share (CHRN-66). No tool lives here: a tool is
// attached in Tools(), and this file adds none. What it holds is the part every
// read tool would otherwise get slightly differently -- how a status the API
// answers becomes a result an agent can act on, and where the limits are.

// Limits are the TOOLS' limits, tighter than the API's (CHRN-66 ruling 3): a
// model's context wants less than a client's screen does. Every figure is a
// guess, because nothing has measured one; the evidence row records the sizes
// it saw and whether a cap cut, so a later change is made on a number.
const (
	// DefaultSearchLimit and DefaultInboxLimit are what a call gets when it
	// names no limit, or names one that is not positive.
	DefaultSearchLimit = 10
	DefaultInboxLimit  = 10
	DefaultListLimit   = 10

	// APISearchCap, APITriageCap and APIListCap restate what the API clamps
	// to, so a limit over them is clamped here, echoed, and never sent to be
	// refused or silently cut a second time. Triage's default and cap are
	// both 25 in the service: read_inbox can never offer more than that.
	APISearchCap = 100
	APITriageCap = 25
	APIListCap   = 200

	// MaxNoteBodyChars cuts a note's body, with a flag and the revision, never
	// silently. The API caps nothing on a body at all.
	MaxNoteBodyChars = 20000

	// MaxThreadTurns is how many turns of a thread a call returns -- the
	// newest are kept -- with the number left out. The API does not paginate
	// a thread.
	MaxThreadTurns = 100
)

// ClampLimit is the limit a call is actually made with, and whether that
// differs from what was asked for: a limit that is not positive becomes def --
// the API would answer it with a 400, and an agent should not have to learn
// that -- and one over max becomes max. It never refuses; the caller echoes
// the result so the agent can see what it got.
func ClampLimit(asked, def, max int) (used int, clamped bool) {
	switch {
	case asked <= 0:
		return def, asked != 0
	case asked > max:
		return max, true
	default:
		return asked, false
	}
}

// Truncate cuts s to at most max characters (runes, never mid-character) and
// reports whether it cut. The caller sets a `truncated` flag from it; a cut
// that is not flagged is the failure this exists to prevent.
func Truncate(s string, max int) (string, bool) {
	if max < 0 || utf8.RuneCountInString(s) <= max {
		return s, false
	}
	runes := []rune(s)
	return string(runes[:max]), true
}

// Availability is what an API answer means to a tool, in a form that can sit in
// a result. 404, 410 and 503 are results and not errors: "this ref names
// nothing", "this was withdrawn" and "this feature is off in this deployment"
// are three different facts an agent can act on, and folding them into one
// tool error makes it guess.
type Availability string

const (
	// Available is a 2xx: there is a body to read.
	Available Availability = "ok"
	// NotFound is a 404: the ref names nothing.
	NotFound Availability = "not_found"
	// Withdrawn is a 410: it existed and was withdrawn; the body says who and when.
	Withdrawn Availability = "withdrawn"
	// Unconfigured is a 503: the store behind it is not wired in this deployment.
	Unconfigured Availability = "unconfigured"
)

// Classify maps the status the API answered to an Availability, or to an error
// when it is neither: 401 is ErrUnauthorized (the credential was refused, which
// is the session's problem and not an answer about the ref), and anything else
// is a failure of the call. op names the operation for the error text.
func Classify(op string, status int) (Availability, error) {
	switch {
	case status >= 200 && status < 300:
		return Available, nil
	case status == http.StatusNotFound:
		return NotFound, nil
	case status == http.StatusGone:
		return Withdrawn, nil
	case status == http.StatusServiceUnavailable:
		return Unconfigured, nil
	case status == http.StatusUnauthorized:
		return "", ErrUnauthorized
	default:
		return "", fmt.Errorf("%s answered %d", op, status)
	}
}

// FeatureOff is what a tool says when Classify answered Unconfigured, so the
// sentence is the same everywhere: the feature is off in this deployment, which
// is not the same as nothing being there.
func FeatureOff(feature string) string {
	return feature + " is not turned on in this Chronicle deployment; that says nothing about whether anything exists"
}
