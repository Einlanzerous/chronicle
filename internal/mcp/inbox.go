package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/Einlanzerous/chronicle/internal/apiclient"
)

// InboxInput is read_inbox's arguments.
type InboxInput struct {
	Limit int `json:"limit,omitempty" jsonschema:"how many memos to return; default 10, at most 25. A larger number is lowered to 25 and one of 0 or less becomes the default; the result says what was used"`
}

// InboxMemo is one memo waiting for a triage decision: what was said, when,
// and where the Scribe's proposal for it stands. The proposal itself is left
// out -- it is derived, disposable text, and a model that wants it is better
// served by the memo than by the Scribe's guess at it.
type InboxMemo struct {
	MemoID         string    `json:"memo_id" jsonschema:"the memo's id"`
	CapturedAt     time.Time `json:"captured_at" jsonschema:"when the recording was captured"`
	DurationMs     int       `json:"duration_ms,omitempty" jsonschema:"how long the recording is, when anything has measured it"`
	Excerpt        string    `json:"excerpt" jsonschema:"the start of the transcript: what was said"`
	ProposalStatus string    `json:"proposal_status" jsonschema:"where the Scribe's routing proposal stands: absent, proposed, needs_input, or an error state"`
	ProposalError  string    `json:"proposal_error,omitempty" jsonschema:"why there is no usable proposal, when there is none"`
	PreAcceptable  bool      `json:"pre_acceptable" jsonschema:"whether the proposal clears the confidence threshold a person's screen pre-fills from"`
}

// Inbox is read_inbox's answer.
type Inbox struct {
	Memos        []InboxMemo `json:"memos" jsonschema:"the memos waiting for a decision, oldest first"`
	Limit        int         `json:"limit" jsonschema:"the limit this call was made with"`
	LimitClamped bool        `json:"limit_clamped" jsonschema:"whether that differs from the limit asked for"`
	Unconfigured bool        `json:"unconfigured,omitempty" jsonschema:"true when triage is not turned on in this deployment; that says nothing about whether any memo is waiting"`
	Note         string      `json:"note,omitempty" jsonschema:"what to make of an empty or unavailable answer"`
}

// codeTriageUnconfigured is the code internal/api answers with when no triage
// service is wired (apierr.go). Restated here because internal/mcp may not
// import internal/api; TestReadInboxSaysWhenTriageIsOff drives the real handler,
// so a change to the code fails there.
const codeTriageUnconfigured = "triage_unconfigured"

// emptyForAgent is said when an agent account's inbox comes back empty, because
// that is the answer an agent will most often get and the one it is likeliest
// to misread as "nothing is waiting".
const emptyForAgent = "Nothing is waiting for this agent account. Triage scopes the inbox to the memos an account captured itself, " +
	"so this says nothing about the owner's inbox, which this session cannot read."

// readInbox is CHRN-66 rulings 0 and 1: the memos awaiting triage and nothing
// else, for the account the session acts as, exactly as the API scopes it. It
// asks GET /triage/batch and no other route -- not /triage/deferred, not a
// thread list -- and the harness test pins that by the recorded path.
var readInbox = define("read_inbox",
	"List the voice memos waiting for a triage decision -- transcribed, not yet routed into a note, a ticket or a discussion -- "+
		"oldest first, each with an excerpt of its transcript, its length, and where the Scribe's proposal for it stands. "+
		"WHOSE INBOX: the account this session acts as, exactly as Chronicle scopes it, never anyone else's. "+
		"On the hosted endpoint that is a person, and which person depends on the deployment's Access policy: "+
		"the owner sees every author's waiting memos, any other person only their own. "+
		"A stdio agent account sees only memos it captured itself, and nothing captures voice memos as an agent today, "+
		"so an empty answer for an agent means nothing is waiting for it, NOT that the owner's inbox is empty. "+
		"Returns at most 25 memos a call (default 10; a limit above 25 is lowered to 25 and one of 0 or less becomes the default, "+
		"and the result says which), so a full list may not be the whole inbox. "+
		"This puts transcript text in front of you: expect a few hundred characters per memo. "+
		"Reads only -- it does not accept, defer or route anything. "+
		"To find something already filed as a note, search notes (search_notes) instead.",
	false,
	func(ctx context.Context, sess Session, in InboxInput) (Inbox, error) {
		limit, clamped := ClampLimit(in.Limit, DefaultInboxLimit, APITriageCap)
		resp, err := sess.API.GetTriageBatchWithResponse(ctx, &apiclient.GetTriageBatchParams{Limit: &limit})
		if err != nil {
			return Inbox{}, fmt.Errorf("reach the Chronicle API: %w", err)
		}
		out := Inbox{Memos: []InboxMemo{}, Limit: limit, LimitClamped: clamped}
		// A 503 is "triage is off in this deployment" ONLY when Chronicle says
		// so in its own words. A proxy's plain-text 503 during a redeploy, or
		// the router's accounts_unconfigured, is a failure to retry, and
		// reporting it as a feature that is off would tell an agent to stop
		// asking about something that is merely restarting.
		if resp.StatusCode() == http.StatusServiceUnavailable {
			if resp.JSON503 != nil && resp.JSON503.Code == codeTriageUnconfigured {
				out.Unconfigured = true
				out.Note = FeatureOff("Triage")
				return out, nil
			}
			return Inbox{}, errors.New("GET /triage/batch answered 503 without Chronicle's triage_unconfigured code: " +
				"the API or a proxy in front of it is unavailable; try again shortly")
		}
		av, err := Classify("GET /triage/batch", resp.StatusCode())
		if err != nil {
			return Inbox{}, err
		}
		if av != Available {
			return Inbox{}, fmt.Errorf("GET /triage/batch answered %s", av)
		}
		if resp.JSON200 == nil {
			return Inbox{}, fmt.Errorf("GET /triage/batch answered %d without a batch", resp.StatusCode())
		}
		for _, it := range resp.JSON200.Items {
			m := InboxMemo{
				MemoID:         it.MemoId.String(),
				CapturedAt:     it.CapturedAt,
				Excerpt:        it.Excerpt,
				ProposalStatus: it.Status,
				PreAcceptable:  it.PreAcceptable,
			}
			if it.DurationMs != nil {
				m.DurationMs = int(*it.DurationMs)
			}
			if it.Error != nil {
				m.ProposalError = *it.Error
			}
			out.Memos = append(out.Memos, m)
		}
		if len(out.Memos) == 0 && sess.Identity.Kind == KindAgent {
			out.Note = emptyForAgent
		}
		return out, nil
	})
