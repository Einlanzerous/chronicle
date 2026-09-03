package store

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"os"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// CHRN-33's store surface: the row that is a lock, the guard that keeps a
// confirmation terminal, and the two structural rules the tier split and the
// deadlock argument rest on.

func seedTriageable(t *testing.T, s *Store, ctx context.Context, hash string) uuid.UUID {
	t.Helper()
	memoID, _ := seedMemoWithTranscript(t, s, ctx, hash, "whisper.cpp/small.en")
	for _, step := range [][2]string{
		{StateCaptured, StateQueued}, {StateQueued, StateTranscribing},
		{StateTranscribing, StateTranscribed},
	} {
		if _, err := s.AdvanceMemoState(ctx, memoID, step[0], step[1], ""); err != nil {
			t.Fatalf("advance %s -> %s: %v", step[0], step[1], err)
		}
	}
	return memoID
}

func aDecision(memoID uuid.UUID, key string) Decision {
	return Decision{
		MemoID: memoID, Destination: LinkTicket, ProjectKey: "CHRN", Type: "task",
		Title: "Do the thing", Description: "## Summary", IdempotencyKey: key,
	}
}

// THE ROW IS THE LOCK. A second claim on a memo that already has one is not
// ours, whatever we send — which is what T2 branches on, and why it must never
// create for a row it did not put there.
func TestASecondClaimOnOneMemoIsNotOurs(t *testing.T) {
	s, ctx := newTestStore(t)
	memoID := seedTriageable(t, s, ctx, strings.Repeat("a", 64))

	first, claim, err := s.ClaimMemoLink(ctx, aDecision(memoID, "key-1"))
	if err != nil || claim != ClaimInserted {
		t.Fatalf("first claim: %q err=%v", claim, err)
	}
	second, claim, err := s.ClaimMemoLink(ctx, aDecision(memoID, "key-2"))
	if err != nil {
		t.Fatalf("second claim: %v", err)
	}
	if claim != ClaimExisting {
		t.Fatalf("a second claim reported %q, want %q", claim, ClaimExisting)
	}
	if second.ID != first.ID || second.SentIdempotencyKey != "key-1" {
		t.Fatalf("the second claim overwrote the first: %+v", second)
	}
}

// A REFUSED ROW IS RE-ARMED BY A DIFFERENT DECISION, and only by a different
// one. That is what lets an operator correct a misfile without waiting out
// Switchyard's 24-hour idempotency cache.
func TestARefusedRowIsReArmedByANewDecisionOnly(t *testing.T) {
	s, ctx := newTestStore(t)
	memoID := seedTriageable(t, s, ctx, strings.Repeat("b", 64))

	if _, _, err := s.ClaimMemoLink(ctx, aDecision(memoID, "key-1")); err != nil {
		t.Fatal(err)
	}
	status := 404
	if _, err := s.ResolveMemoLink(ctx, memoID, ClaimInserted,
		func(context.Context, LinkAttempt) (LinkResolution, error) {
			return LinkResolution{Action: LinkRefuse, RefusedStatus: &status,
				RefusedReason: "project is archived"}, nil
		}); err != nil {
		t.Fatalf("refuse: %v", err)
	}

	// An IDENTICAL resend is not ours: it would refuse identically.
	_, claim, err := s.ClaimMemoLink(ctx, aDecision(memoID, "key-2"))
	if err != nil {
		t.Fatal(err)
	}
	if claim != ClaimExisting {
		t.Fatalf("an identical resend of a refused decision claimed %q", claim)
	}
	if l, _ := s.MemoLinkFor(ctx, memoID); !l.Refused() {
		t.Fatal("the identical resend cleared the refusal")
	}

	// A DIFFERENT decision is.
	corrected := aDecision(memoID, "key-3")
	corrected.ProjectKey = "SWY"
	l, claim, err := s.ClaimMemoLink(ctx, corrected)
	if err != nil {
		t.Fatal(err)
	}
	switch {
	case claim != ClaimRearmed:
		t.Fatalf("a corrected decision claimed %q, want %q", claim, ClaimRearmed)
	case l.Refused():
		t.Fatal("the re-armed row is still refused")
	case l.SentIdempotencyKey != "key-3":
		t.Fatalf("sent_idempotency_key = %q, want the fresh one", l.SentIdempotencyKey)
	case l.SentProjectKey != "SWY":
		t.Fatalf("sent_project_key = %q, want the correction", l.SentProjectKey)
	case l.SweptAt != nil || l.CandidateKeys != nil:
		t.Fatalf("the re-arm kept the previous decision's sweep findings: %+v", l)
	}
}

// RE-ARMING UNDER THE KEY THAT WAS REFUSED IS REFUSED BY THE DATABASE, because
// Switchyard has that refusal cached under it: the correction would replay the
// 404 it was correcting. Enforced in the guard rather than only in the one code
// path that does it today.
func TestReArmingNeedsAFreshIdempotencyKey(t *testing.T) {
	s, ctx := newTestStore(t)
	memoID := seedTriageable(t, s, ctx, strings.Repeat("c", 64))

	if _, _, err := s.ClaimMemoLink(ctx, aDecision(memoID, "reused-key")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolveMemoLink(ctx, memoID, ClaimInserted,
		func(context.Context, LinkAttempt) (LinkResolution, error) {
			return LinkResolution{Action: LinkRefuse, RefusedReason: "no"}, nil
		}); err != nil {
		t.Fatal(err)
	}

	corrected := aDecision(memoID, "reused-key")
	corrected.ProjectKey = "SWY"
	_, _, err := s.ClaimMemoLink(ctx, corrected)
	if !errors.Is(err, ErrLinkKeyReused) {
		t.Fatalf("err = %v, want ErrLinkKeyReused", err)
	}
}

// A CONFIRMATION IS TERMINAL. The accept path answers `applied` with a stored
// key and no outward call, and that answer is only honest while the key cannot
// change afterwards.
func TestAConfirmedLinkIsImmutable(t *testing.T) {
	s, ctx := newTestStore(t)
	memoID := seedTriageable(t, s, ctx, strings.Repeat("d", 64))

	if _, _, err := s.ClaimMemoLink(ctx, aDecision(memoID, "key-1")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ResolveMemoLink(ctx, memoID, ClaimInserted,
		func(context.Context, LinkAttempt) (LinkResolution, error) {
			return LinkResolution{Action: LinkConfirm, TicketKey: "CHRN-1", AdvanceTo: StateTriaged}, nil
		}); err != nil {
		t.Fatalf("confirm: %v", err)
	}

	_, err := s.ResolveMemoLink(ctx, memoID, ClaimExisting,
		func(context.Context, LinkAttempt) (LinkResolution, error) {
			return LinkResolution{Action: LinkConfirm, TicketKey: "CHRN-2"}, nil
		})
	if err == nil {
		t.Fatal("a confirmed link was re-pointed at another ticket")
	}
	l, _ := s.MemoLinkFor(ctx, memoID)
	if l.TicketKey == nil || *l.TicketKey != "CHRN-1" {
		t.Fatalf("ticket_key = %v, want the original", l.TicketKey)
	}
}

// THE OUTWARD CALL RUNS INSIDE THE TRANSACTION THAT HOLDS THE LOCKS. Asserted
// from inside the callback, by asking whether the row reads as locked to
// anybody else — which is the same probe the admin report uses, so the two
// cannot disagree about what "in flight" means.
func TestTheCallbackRunsWithBothRowsLocked(t *testing.T) {
	s, ctx := newTestStore(t)
	memoID := seedTriageable(t, s, ctx, strings.Repeat("e", 64))

	link, _, err := s.ClaimMemoLink(ctx, aDecision(memoID, "key-1"))
	if err != nil {
		t.Fatal(err)
	}
	if flight, err := s.LinksInFlight(ctx, []uuid.UUID{link.ID}); err != nil || flight[link.ID] {
		t.Fatalf("the row reads as in flight before T2 ran: %v %v", flight, err)
	}

	var lockedInside bool
	if _, err := s.ResolveMemoLink(ctx, memoID, ClaimInserted,
		func(inner context.Context, att LinkAttempt) (LinkResolution, error) {
			// A different pool connection: this is what the sweep and the admin
			// probe see while the callback is running.
			flight, err := s.LinksInFlight(context.Background(), []uuid.UUID{att.Link.ID})
			if err != nil {
				return LinkResolution{}, err
			}
			lockedInside = flight[att.Link.ID]
			return LinkResolution{Action: LinkConfirm, TicketKey: "CHRN-1", AdvanceTo: StateTriaged}, nil
		}); err != nil {
		t.Fatalf("ResolveMemoLink: %v", err)
	}
	if !lockedInside {
		t.Fatal("the outward call ran outside the transaction's locks")
	}
	if flight, _ := s.LinksInFlight(ctx, []uuid.UUID{link.ID}); flight[link.ID] {
		t.Fatal("the lock outlived the transaction")
	}
}

// A held memo is not advanced, and the caller can still record the link. The
// store enforces the mechanism; which arm to take is the caller's.
func TestAResolutionCanConfirmWithoutAdvancingTheMemo(t *testing.T) {
	s, ctx := newTestStore(t)
	memoID := seedTriageable(t, s, ctx, strings.Repeat("f", 64))
	if _, _, err := s.ClaimMemoLink(ctx, aDecision(memoID, "key-1")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AdvanceMemoState(ctx, memoID, StateTranscribed, StateHeld, "on hold"); err != nil {
		t.Fatal(err)
	}

	if _, err := s.ResolveMemoLink(ctx, memoID, ClaimInserted,
		func(context.Context, LinkAttempt) (LinkResolution, error) {
			return LinkResolution{Action: LinkConfirm, TicketKey: "CHRN-1"}, nil
		}); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	m, _ := s.GetMemo(ctx, memoID)
	if m.State != StateHeld {
		t.Fatalf("memo is %q, want the hold left standing", m.State)
	}
	if l, _ := s.MemoLinkFor(ctx, memoID); !l.Confirmed() {
		t.Fatal("the link was not confirmed")
	}
}

// ============================================================================
// Structural guards — the two rules that no runtime test can pin down, because
// they are about what the code CANNOT do rather than what it does.
// ============================================================================

// LOCK ORDER IS LINK ROW, THEN MEMO ROW, ON EVERY PATH THAT TAKES BOTH.
//
// Two orders deadlock on a busy evening and Postgres kills one of them at
// random, which surfaces as a triage batch that fails a different item every
// time and cannot be reproduced. There is no runtime test for the absence of a
// second order, so this asserts the structure that makes one impossible: EXACTLY
// ONE STATEMENT IN THIS PACKAGE LOCKS A MEMO ROW, it lives in resolveLocked, and
// resolveLocked is only ever called with the link row already locked.
func TestExactlyOnePlaceLocksAMemoRow(t *testing.T) {
	src := readSource(t, "memolink.go")

	const memoLock = "FROM tier2.memos WHERE id = $1 FOR UPDATE"
	if n := strings.Count(src, memoLock); n != 1 {
		t.Fatalf("%d statements lock a memo row; there must be exactly one, "+
			"so that no second lock order can exist", n)
	}
	// And it is inside resolveLocked, which is reached only after the link row
	// has been taken — by ResolveMemoLink and by SweepMemoLink, both of which
	// select the link row FOR UPDATE first.
	fn := funcBody(t, "memolink.go", "resolveLocked")
	if !strings.Contains(fn, memoLock) {
		t.Fatal("the memo lock is not in resolveLocked, so the ordering argument no longer holds")
	}
	for _, caller := range []string{"ResolveMemoLink", "SweepMemoLink"} {
		body := funcBody(t, "memolink.go", caller)
		link := strings.Index(body, "FROM tier2.memo_links")
		call := strings.Index(body, "resolveLocked(")
		if link < 0 || call < 0 || link > call {
			t.Fatalf("%s does not lock the link row before reaching resolveLocked", caller)
		}
		if !strings.Contains(body, "FOR UPDATE") {
			t.Fatalf("%s does not lock the link row at all", caller)
		}
	}
}

// THE TWO POOLS NEVER SHARE A TRANSACTION, and they cannot: they connect as
// different roles. This asserts the halves stay apart in the source, which is
// the only place the mistake could be made — the tier-1 file must never name a
// tier-2 write target, and the tier-2 decision files must never name tier 1.
func TestTheTierHalvesDoNotReachIntoEachOther(t *testing.T) {
	if src := readSource(t, "proposal.go"); strings.Contains(src, "tier2.memo_links") {
		t.Fatal("the tier-1 file names tier2.memo_links — proposals are read on the tier-1 " +
			"pool and decisions are written on the main one, and nothing may span them")
	}
	for _, f := range []string{"memolink.go", "triage.go"} {
		src := readSource(t, f)
		// tier1.memo_proposals is the only tier-1 table; naming it from a
		// tier-2 write path would be a join across two pools that cannot exist.
		if strings.Contains(src, "tier1.") {
			t.Fatalf("%s reaches into tier 1 from a tier-2 write path", f)
		}
	}
}

// Tier1Store STILL EXPOSES NO METHOD THAT WRITES TIER 2 — asserted over the
// whole method set rather than over the ones CHRN-33 happened to add, so a
// later addition is caught by the same test.
func TestTier1StoreHasNoTierTwoWriteMethod(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}
	writes := []string{"INSERT INTO tier2.", "UPDATE tier2.", "DELETE FROM tier2."}

	fset := token.NewFileSet()
	var checked int
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, d := range file.Decls {
			fn, ok := d.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || !isTier1Receiver(fn.Recv) {
				continue
			}
			checked++
			var b strings.Builder
			if err := printNode(&b, fset, fn); err != nil {
				t.Fatal(err)
			}
			for _, w := range writes {
				if strings.Contains(b.String(), w) {
					t.Fatalf("%s: Tier1Store.%s writes tier 2 (%s)", name, fn.Name.Name, w)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("no Tier1Store methods were examined, so this proves nothing")
	}
}

func isTier1Receiver(recv *ast.FieldList) bool {
	if len(recv.List) != 1 {
		return false
	}
	star, ok := recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	id, ok := star.X.(*ast.Ident)
	return ok && id.Name == "Tier1Store"
}

func readSource(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

// funcBody returns one function's source text.
func funcBody(t *testing.T, file, name string) string {
	t.Helper()
	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, file, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", file, err)
	}
	for _, d := range f.Decls {
		fn, ok := d.(*ast.FuncDecl)
		if !ok || fn.Name.Name != name {
			continue
		}
		var b strings.Builder
		if err := printNode(&b, fset, fn); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	t.Fatalf("%s: no function %s", file, name)
	return ""
}

func printNode(w *strings.Builder, fset *token.FileSet, n ast.Node) error {
	return printer.Fprint(w, fset, n)
}
