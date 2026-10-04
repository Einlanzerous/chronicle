package api

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/Einlanzerous/chronicle/internal/api/wire"
	"github.com/Einlanzerous/chronicle/internal/store"
)

// CHRN-128: raiseMemoRetention, the pin. The rules are store.RaiseRetention's
// and are proved against Postgres there; these drive the HANDLER — who may
// ask, and what status and code each refusal becomes — through the document,
// since mustStatus runs every response past apitest.Conform.

func (rig *memoRig) raise(id, token, retention string) *httptest.ResponseRecorder {
	return rig.put("/audio/"+id+"/retention", token, `{"retention":"`+retention+`"}`, "application/json")
}

func (rig *memoRig) put(path, token, body, contentType string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, path, strings.NewReader(body))
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	if contentType != "" {
		r.Header.Set("Content-Type", contentType)
	}
	rig.h.ServeHTTP(rec, r)
	return rec
}

func (rig *memoRig) retentionOf(id uuid.UUID) string {
	rig.memos.mu.Lock()
	defer rig.memos.mu.Unlock()
	return rig.memos.memos[id].Retention
}

func withRetention(r string) memoOpt { return func(m *store.Memo) { m.Retention = r } }
func withState(s string) memoOpt     { return func(m *store.Memo) { m.State = s } }

// THE FIRST DONE-WHEN: one action moves a memo from PRUNES <date> to PINNED.
func TestAPinMovesAMemoFromScheduledToPinned(t *testing.T) {
	rig := newMemoRig(t)
	m := rig.memo(t, rig.author, "a voice worth keeping")
	rig.transcribe(m, "a voice", false, "whisper.cpp/small.en", m.CapturedAt.Add(time.Minute))
	n := rig.note(t, rig.author, &m)

	before := provenanceOf(t, rig, n.Ref(), "author-token").Items[0]
	if before.RetentionStatus != wire.Scheduled || before.PrunesAt == nil {
		t.Fatalf("before the pin: status = %q, prunes_at = %v; want scheduled with a date",
			before.RetentionStatus, before.PrunesAt)
	}

	rec := rig.raise(m.ID.String(), "author-token", "forever")
	mustStatus(t, rec, http.StatusOK, "raiseMemoRetention")
	got := decodeInto[wire.RetentionState](t, rec)
	if got.MemoId != m.ID || got.Retention != wire.RetentionStateRetentionForever ||
		got.RetentionStatus != store.RetentionStatusPinned || got.PrunesAt != nil {
		t.Errorf("answer = %+v; want this memo, forever, pinned, and no prune date", got)
	}

	// And the block a client renders from says the same thing on the next read.
	after := provenanceOf(t, rig, n.Ref(), "author-token").Items[0]
	if after.RetentionStatus != wire.Pinned || after.PrunesAt != nil {
		t.Errorf("after the pin: status = %q, prunes_at = %v; want pinned and null",
			after.RetentionStatus, after.PrunesAt)
	}

	// A RETRIED PIN ANSWERS LIKE THE FIRST. The same level is a 200 that
	// changes nothing, so a client unsure its request landed can send it again.
	mustStatus(t, rig.raise(m.ID.String(), "author-token", "forever"), http.StatusOK, "raiseMemoRetention")
}

// The owner may keep anybody's recording: mayReadMemo's rule, unchanged.
func TestTheOwnerMayPinAnotherAccountsMemo(t *testing.T) {
	rig := newMemoRig(t)
	m := rig.memo(t, rig.author, "kept by the owner")
	mustStatus(t, rig.raise(m.ID.String(), "owner-token", "forever"), http.StatusOK, "raiseMemoRetention")
	if got := rig.retentionOf(m.ID); got != store.RetentionForever {
		t.Errorf("retention = %q, want forever", got)
	}
}

// discard_now may be lifted to the default as well as to the pin.
func TestAnUndecidedDiscardCanBeRaisedToThirtyDays(t *testing.T) {
	rig := newMemoRig(t)
	m := rig.memo(t, rig.author, "almost thrown away", withRetention(store.RetentionDiscardNow))
	rec := rig.raise(m.ID.String(), "author-token", "days_30")
	mustStatus(t, rec, http.StatusOK, "raiseMemoRetention")
	if got := decodeInto[wire.RetentionState](t, rec).Retention; got != wire.RetentionStateRetentionDays30 {
		t.Errorf("retention = %q, want days_30", got)
	}
}

// THE SECOND DONE-WHEN, first half: a request to lower is REFUSED, with a
// reason, and nothing moves.
func TestARequestToLowerRetentionIsRefusedNotIgnored(t *testing.T) {
	rig := newMemoRig(t)
	pinned := rig.memo(t, rig.author, "already pinned", withRetention(store.RetentionForever))
	ordinary := rig.memo(t, rig.author, "thirty days")

	for _, c := range []struct {
		name string
		m    store.Memo
		to   string
	}{
		{"forever → days_30", pinned, "days_30"},
		{"forever → discard_now", pinned, "discard_now"},
		{"days_30 → discard_now", ordinary, "discard_now"},
	} {
		rec := rig.raise(c.m.ID.String(), "author-token", c.to)
		mustStatus(t, rec, http.StatusConflict, "raiseMemoRetention")
		body := decodeInto[wire.Error](t, rec)
		if body.Code != codeRetentionLowered {
			t.Errorf("%s: code = %q, want %q", c.name, body.Code, codeRetentionLowered)
		}
		if !strings.Contains(body.Message, "never lowered") {
			t.Errorf("%s: message %q does not say why", c.name, body.Message)
		}
		if got := rig.retentionOf(c.m.ID); got != c.m.Retention {
			t.Errorf("%s: retention moved to %q", c.name, got)
		}
	}
}

// THE SECOND DONE-WHEN, second half: a pruned memo cannot be pinned, and the
// answer says the audio is gone rather than pretending to keep it.
func TestAPrunedMemoCannotBePinned(t *testing.T) {
	rig := newMemoRig(t)
	m := rig.memo(t, rig.author, "went at 03:00")
	m = rig.prune(t, m)

	rec := rig.raise(m.ID.String(), "author-token", "forever")
	mustStatus(t, rec, http.StatusGone, "raiseMemoRetention")
	body := decodeInto[wire.Error](t, rec)
	if body.Code != codeAudioPruned {
		t.Errorf("code = %q, want %q — the same fact getMemoAudio reports", body.Code, codeAudioPruned)
	}
	if !strings.Contains(body.Message, "nothing left to keep") {
		t.Errorf("message %q does not say why", body.Message)
	}
	if got := rig.retentionOf(m.ID); got != store.RetentionDays30 {
		t.Errorf("retention = %q: a refused pin wrote", got)
	}
}

func TestADiscardedMemoCannotBePinned(t *testing.T) {
	rig := newMemoRig(t)
	m := rig.memo(t, rig.author, "the cable in the shed", withState(store.StateDiscarded))
	rec := rig.raise(m.ID.String(), "author-token", "forever")
	mustStatus(t, rec, http.StatusConflict, "raiseMemoRetention")
	if code := decodeInto[wire.Error](t, rec).Code; code != codeMemoDiscarded {
		t.Errorf("code = %q, want %q", code, codeMemoDiscarded)
	}
}

// THE THIRD DONE-WHEN, server side: a caller who cannot read the audio cannot
// pin it — and is told exactly what a nonexistent id is told, whatever state
// the memo is in, so the refusal says nothing about another account's
// recording. (`audio_readable: false` is what hides the control; this is what
// holds when a client ignores it.)
func TestOnlyTheAuthorAndTheOwnerMayPin(t *testing.T) {
	rig := newMemoRig(t)
	ordinary := rig.memo(t, rig.author, "ordinary")
	prunedMemo := rig.prune(t, rig.memo(t, rig.author, "pruned"))
	pinned := rig.memo(t, rig.author, "pinned", withRetention(store.RetentionForever))
	n := rig.note(t, rig.author, &ordinary)

	for _, caller := range []struct{ who, token string }{
		{"another member", "other-token"},
		{"an agent session", "agent-token"},
	} {
		if provenanceOf(t, rig, n.Ref(), caller.token).Items[0].AudioReadable {
			t.Fatalf("%s is told audio_readable: true", caller.who)
		}
		baseline := rig.raise(uuid.NewString(), caller.token, "forever")
		mustStatus(t, baseline, http.StatusNotFound, "raiseMemoRetention")

		for name, c := range map[string]struct {
			m  store.Memo
			to string
		}{
			"ordinary":         {ordinary, "forever"},
			"pruned":           {prunedMemo, "forever"},
			"pinned, lowering": {pinned, "days_30"},
		} {
			rec := rig.raise(c.m.ID.String(), caller.token, c.to)
			mustStatus(t, rec, http.StatusNotFound, "raiseMemoRetention")
			if rec.Body.String() != baseline.Body.String() {
				t.Errorf("%s pinning a %s memo: body = %s, want byte-identical to a nonexistent id (%s)",
					caller.who, name, rec.Body.String(), baseline.Body.String())
			}
			if got := rig.retentionOf(c.m.ID); got != c.m.Retention {
				t.Errorf("%s moved a %s memo's retention to %q", caller.who, name, got)
			}
		}
	}
}

// Every other declared refusal, driven so Conform judges a real response.
func TestRaiseMemoRetentionRefusesAMalformedRequest(t *testing.T) {
	rig := newMemoRig(t)
	m := rig.memo(t, rig.author, "well-formed memo, malformed asks")
	path := "/audio/" + m.ID.String() + "/retention"

	for _, c := range []struct {
		name, path, token, body, contentType string
		status                               int
		code                                 string
	}{
		{"a level that does not exist", path, "author-token", `{"retention":"a_year"}`, "application/json",
			http.StatusBadRequest, codeInvalidBody},
		{"no level at all", path, "author-token", `{}`, "application/json",
			http.StatusBadRequest, codeInvalidBody},
		{"an unknown field", path, "author-token", `{"retention":"forever","until":"2030"}`, "application/json",
			http.StatusBadRequest, codeInvalidBody},
		{"an id that is not one", "/audio/not-a-uuid/retention", "author-token", `{"retention":"forever"}`, "application/json",
			http.StatusBadRequest, codeInvalidParameter},
		{"a form post", path, "author-token", `retention=forever`, "application/x-www-form-urlencoded",
			http.StatusUnsupportedMediaType, codeUnsupportedMedia},
		{"a body past the cap", path, "author-token", `{"retention":"` + strings.Repeat("x", int(maxBodyBytes)) + `"}`, "application/json",
			http.StatusRequestEntityTooLarge, codeBodyTooLarge},
		{"nobody signed in", path, "", `{"retention":"forever"}`, "application/json",
			http.StatusUnauthorized, codeUnauthorized},
	} {
		rec := rig.put(c.path, c.token, c.body, c.contentType)
		mustStatus(t, rec, c.status, "raiseMemoRetention")
		if code := decodeInto[wire.Error](t, rec).Code; code != c.code {
			t.Errorf("%s: code = %q, want %q", c.name, code, c.code)
		}
	}
	if got := rig.retentionOf(m.ID); got != store.RetentionDays30 {
		t.Errorf("retention = %q: a refused request wrote", got)
	}
}

func TestRaiseMemoRetentionAnswersTheDocumented500And503(t *testing.T) {
	rig := newMemoRig(t)
	m := rig.memo(t, rig.author, "the store will fail under this")
	rig.memos.mu.Lock()
	rig.memos.err = errors.New("connection reset by peer")
	rig.memos.mu.Unlock()

	rec := rig.raise(m.ID.String(), "author-token", "forever")
	mustStatus(t, rec, http.StatusInternalServerError, "raiseMemoRetention")
	if body := decodeInto[wire.Error](t, rec); body.Code != codeInternal || strings.Contains(body.Message, "connection reset") {
		t.Errorf("500 body = %+v; want code internal and none of the store's own words", body)
	}

	f := newFakeAccounts()
	f.signIn(person("member@example.com", false), "member-token")
	bare := &memoRig{h: NewRouter(Deps{
		DB: fakePinger{}, Accounts: f, Logger: discardLogger(), Version: "test", SecureCookies: true,
	})}
	rec = bare.raise(someUUID, "member-token", "forever")
	mustStatus(t, rec, http.StatusServiceUnavailable, "raiseMemoRetention")
	if code := decodeInto[wire.Error](t, rec).Code; code != codeMemosUnconfigured {
		t.Errorf("code = %q, want %q", code, codeMemosUnconfigured)
	}
}
