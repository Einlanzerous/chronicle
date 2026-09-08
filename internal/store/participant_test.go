package store

import (
	"bytes"
	"encoding/csv"
	"errors"
	"strings"
	"testing"
)

// CHRN-44's three `Done when` clauses. CHRN-43's plan moved the participant
// SCHEMA upstream — a schema decision landing under evidence mode is what its
// tier note exists to prevent — so what is left here is the work the Done-when
// actually names, and one thing nobody had noticed was missing.

// ============================================================================
// "Scribe included" — the account that did not exist.
// ============================================================================

// THE GAP THIS TICKET CLOSES. `0002:19-22` says the whole reason
// tier2.users.kind exists is that "a locked IDEA-21 decision makes the Scribe a
// participant in discussions rather than a process acting on them" — and no
// migration ever created that row. CHRN-47 authors an agent turn, and until
// this there was no account to author it as.
func TestEnsureAgentCreatesTheScribeAndIsIdempotent(t *testing.T) {
	s, ctx := newTestStore(t)

	// A freshly migrated database has the owner and nobody else, which is what
	// CHRN-71's TestMigrationSeedsExactlyOneOwner asserts — and the reason this
	// account is created at boot rather than seeded by a migration.
	if _, err := s.Scribe(ctx); err == nil {
		t.Fatal("the scribe exists after migrate alone; it is supposed to be a boot-time account")
	}

	scribe, err := s.EnsureAgent(ctx, ScribeEmail, ScribeDisplayName)
	if err != nil {
		t.Fatalf("EnsureAgent: %v", err)
	}
	if scribe.Kind != KindAgent {
		t.Errorf("kind = %q, want %q", scribe.Kind, KindAgent)
	}
	if scribe.IsOwner {
		t.Error("the scribe is the owner; 0002's users_owner_is_a_person should have refused that")
	}
	if scribe.DisplayName != ScribeDisplayName {
		t.Errorf("display name = %q, want %q", scribe.DisplayName, ScribeDisplayName)
	}

	// IDEMPOTENT, because boot calls it on every start.
	again, err := s.EnsureAgent(ctx, ScribeEmail, ScribeDisplayName)
	if err != nil {
		t.Fatalf("EnsureAgent twice: %v", err)
	}
	if again.ID != scribe.ID {
		t.Errorf("a second call created a second account: %s then %s", scribe.ID, again.ID)
	}
	n, err := s.CountUsers(ctx)
	if err != nil {
		t.Fatalf("CountUsers: %v", err)
	}
	if n != 2 {
		t.Errorf("%d accounts, want 2 (owner + scribe)", n)
	}

	found, err := s.Scribe(ctx)
	if err != nil {
		t.Fatalf("Scribe: %v", err)
	}
	if found.ID != scribe.ID {
		t.Errorf("Scribe() = %s, want %s", found.ID, scribe.ID)
	}

	// AND IT HAS NO WAY IN. 'agent' is an authorship fact, not a permission
	// level (0002:19), so the account that exists to sign a turn must not also
	// be an account somebody could sign in as. Nothing mints for it — asserted
	// rather than assumed, because bootstrapOwner does mint and the two sit
	// next to each other.
	for _, kind := range []string{TokenSession, TokenInvite} {
		count, err := s.CountTokens(ctx, scribe.ID, kind)
		if err != nil {
			t.Fatalf("CountTokens(%s): %v", kind, err)
		}
		if count != 0 {
			t.Errorf("the scribe holds %d %s tokens, want 0", count, kind)
		}
	}
}

// REFUSED RATHER THAN TAKEN OVER. Flipping a person's kind would rewrite what
// their existing authorship means — every turn they have already written stays
// frozen as a person's (CH092), so the account and its history would disagree.
func TestEnsureAgentRefusesAPersonsAddress(t *testing.T) {
	s, ctx := newTestStore(t)
	taken := discPerson(t, s, ctx, ScribeEmail)

	got, err := s.EnsureAgent(ctx, ScribeEmail, ScribeDisplayName)
	if err == nil {
		t.Fatal("EnsureAgent took over a person's address")
	}
	if !strings.Contains(err.Error(), "already belongs to a person") {
		t.Errorf("err = %v, want ErrNotAnAgent", err)
	}
	if got.ID != taken {
		t.Errorf("the refusal did not name the account holding the address: %s", got.ID)
	}

	// The person is untouched — not renamed, not reclassified.
	still, err := s.GetUserByEmail(ctx, ScribeEmail)
	if err != nil {
		t.Fatalf("GetUserByEmail: %v", err)
	}
	if still.Kind != KindPerson {
		t.Errorf("kind = %q, want %q — a person was quietly turned into an agent", still.Kind, KindPerson)
	}

	// AND Scribe() REFUSES TOO, which is the half that matters at runtime.
	// bootstrapScribe makes this conflict a warning rather than a boot failure,
	// so the service runs in this state — and a Scribe() that handed back the
	// person's row would have CHRN-47 authoring agent turns as them. CH092
	// would write those turns `person` and CH090 makes them permanent, so the
	// misattribution could never be corrected, only appended to.
	got, err = s.Scribe(ctx)
	if !errors.Is(err, ErrNotAnAgent) {
		t.Errorf("Scribe() err = %v, want ErrNotAnAgent — it handed back a person", err)
	}
	if got.ID != taken {
		t.Errorf("the refusal did not name the account holding the address: %s", got.ID)
	}
}

// ============================================================================
// Done when 2 — an agent joins and leaves a thread like anybody else.
// ============================================================================

// "A participant model where an agent is a participant, not a special case
// bolted onto a human one." The assertion is that NOTHING in this path is
// agent-specific: the same two calls, against the same table.
func TestAnAgentJoinsAndLeavesAThreadLikeAnybodyElse(t *testing.T) {
	s, ctx := newTestStore(t)
	owner := discPerson(t, s, ctx, "forty-four@example.com")
	scribe, err := s.EnsureAgent(ctx, ScribeEmail, ScribeDisplayName)
	if err != nil {
		t.Fatalf("EnsureAgent: %v", err)
	}
	d := openThread(t, s, ctx, owner, "Retention")

	if err := s.AddParticipant(ctx, d.ID, owner, owner); err != nil {
		t.Fatalf("AddParticipant(person): %v", err)
	}
	if err := s.AddParticipant(ctx, d.ID, scribe.ID, owner); err != nil {
		t.Fatalf("AddParticipant(agent): %v", err)
	}

	ps, err := s.Participants(ctx, d.ID)
	if err != nil {
		t.Fatalf("Participants: %v", err)
	}
	if len(ps) != 2 {
		t.Fatalf("%d participants, want 2", len(ps))
	}

	// THE READ CARRIES THE KIND. A renderer marking the Scribe as an agent must
	// not need a second query to find out — that is what "structurally distinct
	// in the data, not only in the UI" asks for at the read surface.
	byID := map[string]DiscussionParticipant{}
	for _, p := range ps {
		byID[p.UserID.String()] = p
	}
	if got := byID[scribe.ID.String()]; !got.IsAgent() || got.DisplayName != ScribeDisplayName {
		t.Errorf("the scribe reads as %+v, want an agent named %q", got, ScribeDisplayName)
	}
	if got := byID[owner.String()]; got.IsAgent() {
		t.Errorf("the person reads as an agent: %+v", got)
	}

	// The agent can speak while it is on the thread.
	turn := appendTurn(t, s, ctx, d.ID, scribe.ID, "thirty days, and gated on a durable transcript")
	if !turn.ByAgent() {
		t.Errorf("the scribe's turn reads as %q, want agent", turn.AuthorKind)
	}

	// AND IT LEAVES BY THE SAME CALL. No agent branch anywhere.
	if err := s.RemoveParticipant(ctx, d.ID, scribe.ID, owner); err != nil {
		t.Fatalf("RemoveParticipant(agent): %v", err)
	}
	ps, err = s.Participants(ctx, d.ID)
	if err != nil {
		t.Fatalf("Participants: %v", err)
	}
	for _, p := range ps {
		if p.UserID == scribe.ID && p.Active() {
			t.Error("the scribe is still an active participant after being removed")
		}
	}

	// Its turn stays, with its authorship intact — CHRN-43's criterion 16,
	// asserted here for the agent case because that is the one that matters:
	// "six months later nobody can tell which conclusions were reasoned by a
	// person" is a claim about agent turns surviving.
	read, err := s.Turns(ctx, d.ID)
	if err != nil {
		t.Fatalf("Turns: %v", err)
	}
	if len(read) != 2 || read[1].AuthorID != scribe.ID || !read[1].ByAgent() {
		t.Errorf("the removed agent's turn changed: %+v", read)
	}
}

// ============================================================================
// Done when 3 — the distinction survives export.
// ============================================================================

// WHAT "EXPORT" MEANS HERE, since Chronicle has no export subsystem and this
// ticket is not the place to invent one: the store is Postgres, so its export
// is COPY, and the question is whether the exported BYTES carry the person /
// agent distinction or whether it is lost the moment the data leaves the
// database's own joins.
//
// CHRN-43's plan settled the design that makes this true — ruling 5 put
// author_kind ON THE TURN rather than reading tier2.users live — and called it
// "literally what CHRN-44's 'the distinction survives export' asks for". This
// is that claim, actually run.
func TestTheAuthorDistinctionSurvivesExport(t *testing.T) {
	s, ctx := newTestStore(t)
	owner := discPerson(t, s, ctx, "export@example.com")
	scribe, err := s.EnsureAgent(ctx, ScribeEmail, ScribeDisplayName)
	if err != nil {
		t.Fatalf("EnsureAgent: %v", err)
	}
	d := openThread(t, s, ctx, owner, "Exported")
	appendTurn(t, s, ctx, d.ID, scribe.ID, "an agent said this")
	appendTurn(t, s, ctx, d.ID, owner, "and a person answered")

	conn, err := s.pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()

	// NO JOIN IN THE EXPORT QUERY, which is the whole assertion. If the kind
	// lived on tier2.users this SELECT could not produce it at all.
	var out bytes.Buffer
	if _, err := conn.Conn().PgConn().CopyTo(ctx, &out,
		`COPY (SELECT seq, author_kind, body FROM tier2.discussion_turns ORDER BY seq)
		 TO STDOUT WITH (FORMAT csv)`); err != nil {
		t.Fatalf("COPY TO: %v", err)
	}

	// THE EXPORTED COLUMN IS COMPARED EXACTLY, not searched for in the line.
	//
	// A `strings.Contains(line, "agent")` here would be vacuous for the two rows
	// that matter: row 2 exports as `2,agent,an agent said this` and row 3 as
	// `3,person,and a person answered`, so both bodies contain the word being
	// looked for and SWAPPING author_kind on both turns would leave the check
	// green. Only row 1 would have constrained the column, and row 1 is the
	// person case — the agent half, which is this ticket's whole point, would
	// have asserted nothing.
	rows, err := csv.NewReader(bytes.NewReader(out.Bytes())).ReadAll()
	if err != nil {
		t.Fatalf("parse the exported CSV: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("exported %d rows, want 3: %q", len(rows), out.String())
	}
	for i, want := range []string{KindPerson, KindAgent, KindPerson} {
		if got := rows[i][1]; got != want {
			t.Errorf("exported row %d author_kind = %q, want %q (row: %q)", i+1, got, want, rows[i])
		}
	}

	// THE OTHER HALF, AND IT IS A WARNING RATHER THAN A FEATURE — recorded here
	// because CHRN-68's restore drill is reviewed as a RESULT, and a surprise
	// during a restore is the worst time to learn this.
	//
	// A full pg_dump/pg_restore is SAFE: triggers are post-data, so every turn
	// is loaded before discussion_turns_guard exists. A ROW-LEVEL replay of the
	// bytes above is not — it carries author_kind, and CH092 refuses every row,
	// because "the caller supplied it" is exactly what the trigger tests for.
	// CH041 has had the same shape since 0014; this makes it two.
	_, err = conn.Conn().PgConn().CopyFrom(ctx, strings.NewReader("99,person,replayed\n"),
		`COPY tier2.discussion_turns (seq, author_kind, body) FROM STDIN WITH (FORMAT csv)`)
	if got := sqlstate(err); got != pgAuthorKindSupplied {
		t.Errorf("a row-level replay carrying author_kind: SQLSTATE = %q (err %v), want %s — "+
			"if this stopped being true, the note in 0015 for CHRN-68 is stale", got, err, pgAuthorKindSupplied)
	}
}
