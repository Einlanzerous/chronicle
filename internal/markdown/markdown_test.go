package markdown

import (
	"bytes"
	"strings"
	"testing"

	"github.com/yuin/goldmark"
)

// CHRN-40's `Done when`: notes round-trip byte-for-byte, rendered output is
// safe against injected HTML, and reference tokens survive the trip untouched.
//
// The round-trip half is asserted against the real store, in
// internal/store/note_roundtrip_test.go — byte-for-byte is a claim about
// storage, and proving it against a buffer in this package would prove
// nothing.
//
// CHRN-48 keeps all of that and repoints the grammar at the namespaces that
// exist. Its own `Done when` — "an unknown prefix is left as plain text rather
// than guessed at, and nothing about upstream state is persisted here" — is
// TestProseIsNotAReference, TestNoPredicateMarksNoTicket and
// TestTheMarkerCarriesNoUpstreamState below.

// keySet is a live project list, as the predicate sees it.
type keySet map[string]bool

func (k keySet) HasProject(key string) bool { return k[key] }

// liveKeys is the estate's real project list, measured 2026-09-09.
var liveKeys = keySet{
	"ITLK": true, "CHRN": true, "CANT": true, "SERV": true, "PRSR": true,
	"SWY": true, "AMBR": true, "SGNT": true, "EIDO": true, "DRY": true,
	"LYCM": true, "ARGY": true, "AAST": true, "CTFG": true, "IDEA": true,
}

func render(t *testing.T, src string) string {
	t.Helper()
	out, err := Render([]byte(src))
	if err != nil {
		t.Fatalf("Render(%q): %v", src, err)
	}
	return string(out)
}

// renderKeys renders against the live key set.
func renderKeys(t *testing.T, src string) string {
	t.Helper()
	out, err := NewRenderer(liveKeys).Render([]byte(src))
	if err != nil {
		t.Fatalf("Render(%q): %v", src, err)
	}
	return string(out)
}

func scan(src string) Result { return NewRenderer(liveKeys).Scan([]byte(src)) }

// ---------------------------------------------------------------------------
// Safety. Unchanged from CHRN-40 except for the tokens.
// ---------------------------------------------------------------------------

func TestRawHTMLIsNeutralised(t *testing.T) {
	for _, in := range []string{
		`<script>alert(1)</script>`,
		`<img src=x onerror=alert(1)>`,
		`<a href="javascript:alert(1)">x</a>`,
		`<iframe src="https://evil.example"></iframe>`,
		`<style>body{display:none}</style>`,
		`<svg><animate onbegin=alert(1) /></svg>`,
		"Mixed <b>bold</b> with SWY-389 alongside",
	} {
		out := renderKeys(t, in)
		for _, forbidden := range []string{"<script", "<img src=x", "<iframe", "<style", "onerror", "onbegin"} {
			if strings.Contains(out, forbidden) {
				t.Errorf("Render(%q) leaked %q:\n%s", in, forbidden, out)
			}
		}
	}
}

func TestDangerousURLSchemesAreBlanked(t *testing.T) {
	for _, in := range []string{
		`[click](javascript:alert(1))`,
		`[click](JaVaScRiPt:alert(1))`,
		`![img](javascript:alert(1))`,
		`[click](data:text/html;base64,PHNjcmlwdD4=)`,
		`[click](vbscript:msgbox(1))`,
	} {
		out := render(t, in)
		lower := strings.ToLower(out)
		if strings.Contains(lower, "javascript:") || strings.Contains(lower, "vbscript:") ||
			strings.Contains(lower, "data:text/html") {
			t.Errorf("Render(%q) kept an executable scheme:\n%s", in, out)
		}
	}
	if out := render(t, `[ok](https://example.com/a)`); !strings.Contains(out, `href="https://example.com/a"`) {
		t.Errorf("an ordinary link did not survive: %s", out)
	}
}

// The marker writes the token into an attribute, so the token's alphabet is a
// security boundary. reference.go keeps it prose-safe AND the renderer escapes,
// so neither alone is load-bearing.
func TestATokenCannotEscapeItsAttribute(t *testing.T) {
	for _, in := range []string{
		`amber1.a"onmouseover=alert(1).b.0`,
		`amber1.a<script>.b.0`,
		"amber1.a&amp;.b.0",
		`amber1.a'x.b.0`,
	} {
		out := renderKeys(t, in)
		if strings.Contains(out, "data-ref-system") {
			t.Errorf("Render(%q) marked a token carrying attribute punctuation:\n%s", in, out)
		}
		if len(scan(in).References) != 0 {
			t.Errorf("Scan(%q) accepted a token carrying attribute punctuation", in)
		}
	}
	// And nothing reaches an attribute raw, on a token that IS accepted.
	out := renderKeys(t, "amber1.7f3e9a21-0000-4000-8000-000000000000.h:3f9ab2.0")
	if strings.Count(out, `data-ref="`) != 1 || strings.Contains(out, `="">`) {
		t.Errorf("the accepted citation did not render one well-formed attribute:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// The grammar.
// ---------------------------------------------------------------------------

// Criterion 1. Every live project key is a reference, with its key and number.
func TestEveryLiveProjectKeyIsAReference(t *testing.T) {
	body := "Touches SWY-389, CHRN-48, SERV-101, AMBR-25, IDEA-21, ITLK-1, CANT-7, " +
		"PRSR-3, SGNT-12, EIDO-4, DRY-9, LYCM-2, ARGY-216, AAST-1 and CTFG-5."
	refs := scan(body).References
	if len(refs) != 15 {
		t.Fatalf("found %d references, want 15: %+v", len(refs), refs)
	}
	for _, r := range refs {
		if r.System != SystemSwitchyard {
			t.Errorf("%q has system %q, want %q", r.Token, r.System, SystemSwitchyard)
		}
		if r.Key == "" || !strings.HasPrefix(r.Token, r.Key+"-") {
			t.Errorf("%q does not carry its own key, got %q", r.Token, r.Key)
		}
		if r.Number <= 0 {
			t.Errorf("%q parsed to number %d", r.Token, r.Number)
		}
		if !strings.Contains(body, r.Token) {
			t.Errorf("token %q is not what was written", r.Token)
		}
	}
}

// Criterion 2. THE TEST THAT FAILS IF SOMEBODY WIDENS THIS TO A WILDCARD.
//
// Every one of these matches [A-Z][A-Z0-9]{1,9}-\d+, which is exactly why
// Switchyard refuses bare key mentions in free-form prose. A note body is
// free-form prose all the way down.
func TestProseIsNotAReference(t *testing.T) {
	prose := "Encoded UTF-8, hashed with SHA-256 and AES-256, stamped ISO-8601, " +
		"per RFC-3986 over HTTP-2, during COVID-19."
	if refs := scan(prose).References; len(refs) != 0 {
		t.Errorf("prose yielded %d references: %+v", len(refs), refs)
	}
	if keys := scan(prose).UnknownKeys; len(keys) != 7 {
		// They ARE well-shaped, and that is worth recording rather than losing:
		// this slice is how a stale key set becomes visible.
		t.Errorf("UnknownKeys = %v, want the seven well-shaped non-projects", keys)
	}
	// And the rendered output is what it would be with no reference grammar.
	if got, want := renderKeys(t, prose), plainRender(t, prose); got != want {
		t.Errorf("prose rendered differently from plain markdown:\n got %s\nwant %s", got, want)
	}
}

// Criterion 3. The two namespaces CHRN-40 shipped that do not exist.
//
// `SY` is not among the fifteen live project keys. Amber has no AMB-#### id
// and nothing in it is SEALED — `grep -rni sealed` and `grep -rn 'AMB-'` over
// the Amber repository both return nothing.
func TestTheImaginedNamespacesAreProse(t *testing.T) {
	for _, in := range []string{"See SY-412 for the ledger.", "AMB-2291 is sealed."} {
		if out := renderKeys(t, in); strings.Contains(out, "data-ref-system") {
			t.Errorf("Render(%q) marked a namespace that does not exist:\n%s", in, out)
		}
	}
}

// Criterion 4. Amber citations, as cite.Parse actually defines them: three OR
// four components, and a component alphabet that is not just uuids.
func TestAmberCitations(t *testing.T) {
	const uuid = "7f3e9a21-0000-4000-8000-000000000000"
	for _, tc := range []struct {
		in    string
		token string
	}{
		{"See " + uuid2cite(uuid, uuid, "0") + " here.", uuid2cite(uuid, uuid, "0")},
		// The block is optional: cite.Parse accepts three components.
		{"See amber1." + uuid + "." + uuid + " here.", "amber1." + uuid + "." + uuid},
		// Hash-shaped identities are legal components, and a uuid-only rule
		// would leave them as prose.
		{"See amber1.h:3f9ab2." + uuid + ".2 here.", "amber1.h:3f9ab2." + uuid + ".2"},
		// A CITATION ENDING A SENTENCE KEEPS ITS FULL STOP AS PROSE, which is
		// where most of them will be written.
		{"Evidence is at " + uuid2cite(uuid, uuid, "0") + ".", uuid2cite(uuid, uuid, "0")},
	} {
		refs := scan(tc.in).References
		if len(refs) != 1 {
			t.Fatalf("Scan(%q) = %+v, want one citation", tc.in, refs)
		}
		if refs[0].System != SystemAmber {
			t.Errorf("%q has system %q", refs[0].Token, refs[0].System)
		}
		if refs[0].Token != tc.token {
			t.Errorf("token = %q, want %q", refs[0].Token, tc.token)
		}
	}

	for _, in := range []string{
		"amber1.x alone",                     // one component
		"amber1." + uuid + " alone",          // two components
		"amber2." + uuid + "." + uuid + ".0", // a grammar this build does not know
		"amber1..b.0 empty component",
		// A block must be digits. cite.Parse runs Atoi on the fourth component
		// and calls the whole reference malformed when it fails, so refusing the
		// token outright is Amber's own answer rather than a stricter one.
		"amber1.a.b.0x is not a block",
	} {
		if refs := scan(in).References; len(refs) != 0 {
			t.Errorf("Scan(%q) = %+v, want none", in, refs)
		}
	}
}

// Criterion 5, first half. Both Chronicle handles, resolving locally.
func TestChronicleHandles(t *testing.T) {
	refs := scan("Landed as CHR-0311 and discussed in DSC-0007.").References
	if len(refs) != 2 {
		t.Fatalf("Scan = %+v, want two", refs)
	}
	if refs[0].System != SystemChronicle || refs[0].Target != TargetNote || refs[0].Number != 311 {
		t.Errorf("CHR-0311 resolved to %+v", refs[0])
	}
	if refs[1].System != SystemChronicle || refs[1].Target != TargetDiscussion || refs[1].Number != 7 {
		t.Errorf("DSC-0007 resolved to %+v", refs[1])
	}
}

// CLAUDE.md chose the ticket key CHRN over CHR precisely because the two
// namespaces collide. This is that decision held to at the one place it could
// be undone: a key set that claims CHR does not get it.
func TestTheLocalNamespacesOutrankThePredicate(t *testing.T) {
	greedy := keySet{"CHR": true, "DSC": true, "SWY": true}
	refs := NewRenderer(greedy).Scan([]byte("CHR-0311 and DSC-0007 and SWY-389")).References
	if len(refs) != 3 {
		t.Fatalf("Scan = %+v, want three", refs)
	}
	if refs[0].System != SystemChronicle || refs[1].System != SystemChronicle {
		t.Errorf("a predicate claiming CHR/DSC took them: %+v", refs[:2])
	}
	if refs[2].System != SystemSwitchyard {
		t.Errorf("SWY-389 resolved to %+v", refs[2])
	}
}

// The leading boundary is Switchyard's rule — the preceding character must not
// be alphanumeric — and each case is named rather than left to whatever a
// trigger table happened to allow. The em dash and the curly quote are the
// cases an inline parser cannot reach at all.
func TestLeadingBoundary(t *testing.T) {
	for _, tc := range []struct {
		in     string
		marked bool
	}{
		{"a SWY-389 b", true},
		{"a (SWY-389) b", true},
		{"a [SWY-389] b", true},
		{`a "SWY-389" b`, true},
		{"a 'SWY-389' b", true},
		{"a ,SWY-389 b", true},
		{"a —SWY-389 b", true},
		{"a \u00a0SWY-389 b", true},
		{"a “SWY-389” b", true},
		{"a *SWY-389* b", true},
		{"SWY-389 at line head", true},
		{"ABBSWY-1 is an identifier", false},
		{"aSWY-389 is one word", false},
	} {
		got := len(scan(tc.in).References) > 0
		if got != tc.marked {
			t.Errorf("Scan(%q) marked=%v, want %v", tc.in, got, tc.marked)
		}
	}
}

// A reference is a whole word or it is not a reference.
func TestTrailingBoundary(t *testing.T) {
	for _, in := range []string{
		"SWY-389x is not a ticket",
		"SWY- on its own",
		"swy-389 is lowercase prose",
		"SWY-389_backup names a file",
		"SWY-389-2 is an identifier",
		"SWY-389/comments is a path into one",
	} {
		if refs := scan(in).References; len(refs) != 0 {
			t.Errorf("Scan(%q) = %+v, want none", in, refs)
		}
	}

	// A LEADING SLASH IS NOT A WORD BOUNDARY, AND THIS IS A DELIBERATE CHANGE
	// FROM CHRN-40, which left `path/SY-412` as prose because `/` was not in
	// its trigger list. Switchyard's rule is "the preceding character is not
	// alphanumeric" and the plan adopted it, so the last segment of a path is a
	// reference — which is what a pasted ticket URL ends with.
	//
	// The asymmetry with the trailing rule above is the point rather than an
	// oversight: a slash AFTER the key means the key is a directory with
	// something else inside it, and that is not a ticket anybody cited.
	if refs := scan("see https://switchyard/tickets/SWY-389 for it").References; len(refs) != 1 {
		t.Errorf("a ticket URL did not yield its reference: %+v", refs)
	}
	out := renderKeys(t, "swy-389 and SWY-389x but also SWY-389.")
	if n := strings.Count(out, "data-ref-system"); n != 1 {
		t.Errorf("marked %d references, want exactly the one real one:\n%s", n, out)
	}
}

// ---------------------------------------------------------------------------
// The seam.
// ---------------------------------------------------------------------------

// Criterion 7. Same input, same call, only the predicate changed.
func TestThePredicateDecidesMembership(t *testing.T) {
	const in = "Touches SWY-389."
	if refs := NewRenderer(keySet{"SWY": true}).Scan([]byte(in)).References; len(refs) != 1 {
		t.Errorf("with SWY live: %+v, want one reference", refs)
	}
	res := NewRenderer(keySet{"CHRN": true}).Scan([]byte(in))
	if len(res.References) != 0 {
		t.Errorf("with SWY absent: %+v, want none", res.References)
	}
	if len(res.UnknownKeys) != 1 || res.UnknownKeys[0] != "SWY" {
		t.Errorf("UnknownKeys = %v, want [SWY]", res.UnknownKeys)
	}
}

// Criterion 8. CHRN-48 ruling 3: with no key set, nothing is guessed.
func TestNoPredicateMarksNoTicket(t *testing.T) {
	const in = "Touches SWY-389, CHRN-48 and SERV-101."
	if got, want := render(t, in), plainRender(t, in); got != want {
		t.Errorf("the pure Render guessed at a ticket:\n got %s\nwant %s", got, want)
	}
	if refs := References([]byte(in)); len(refs) != 0 {
		t.Errorf("References = %+v, want none", refs)
	}
	// The certain namespaces still resolve without any predicate at all.
	if refs := References([]byte("CHR-0311, DSC-0007")); len(refs) != 2 {
		t.Errorf("the local handles need a predicate: %+v", refs)
	}
}

// ---------------------------------------------------------------------------
// Invariant 2, and the round trip.
// ---------------------------------------------------------------------------

// The marker may carry the token, its system and its key, and nothing that
// belongs to the upstream system — anything else is a copy that goes stale in
// silence.
func TestTheMarkerCarriesNoUpstreamState(t *testing.T) {
	out := renderKeys(t, "See SWY-389 and CHR-0311 and amber1.a.b.0.")
	for _, forbidden := range []string{
		"href=", "IN PROGRESS", "SEALED", "held", "status", "title=", "coral", "gold",
	} {
		if strings.Contains(out, forbidden) {
			t.Errorf("the marker carries %q, which is upstream state or presentation:\n%s", forbidden, out)
		}
	}
}

// Criterion 6. The token survives verbatim and the source is never touched.
func TestReferenceTokensSurviveUntouched(t *testing.T) {
	for _, tc := range []struct{ in, token, system string }{
		{"See SWY-389 for the ledger.", "SWY-389", SystemSwitchyard},
		{"Compare CHR-0311 with this.", "CHR-0311", SystemChronicle},
		{"In parens (CHRN-48) mid-sentence.", "CHRN-48", SystemSwitchyard},
		{"Bracketed [DSC-1] here.", "DSC-1", SystemChronicle},
		{"Trailing punctuation SERV-9, and SERV-10.", "SERV-9", SystemSwitchyard},
		{"Cited at amber1.a.b.0 exactly.", "amber1.a.b.0", SystemAmber},
	} {
		out := renderKeys(t, tc.in)
		if !strings.Contains(out, ">"+tc.token+"<") {
			t.Errorf("Render(%q): token %q is not present verbatim:\n%s", tc.in, tc.token, out)
		}
		if !strings.Contains(out, `data-ref-system="`+tc.system+`"`) {
			t.Errorf("Render(%q): system %q not marked:\n%s", tc.in, tc.system, out)
		}
		if !strings.Contains(out, `data-ref="`+tc.token+`"`) {
			t.Errorf("Render(%q): data-ref is not the written token:\n%s", tc.in, out)
		}
	}
}

func TestRenderDoesNotMutateItsInput(t *testing.T) {
	src := []byte("# Heading\n\nSee SWY-389.\n\n```\nSERV-9\n```\n")
	before := bytes.Clone(src)
	if _, err := NewRenderer(liveKeys).Render(src); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !bytes.Equal(src, before) {
		t.Errorf("Render mutated its input:\n got %q\nwant %q", src, before)
	}
	NewRenderer(liveKeys).Scan(src)
	if !bytes.Equal(src, before) {
		t.Errorf("Scan mutated its input:\n got %q\nwant %q", src, before)
	}
}

// CHR-0311 and CHR-311 are the same note, and the marker records which was
// written. Normalising here would quietly edit a person's prose.
func TestALeadingZeroIsKeptInTheTokenAndDroppedInTheNumber(t *testing.T) {
	refs := References([]byte("CHR-0311 and CHR-311 and CHR-00311"))
	if len(refs) != 3 {
		t.Fatalf("References = %+v, want 3", refs)
	}
	for _, r := range refs {
		if r.Number != 311 {
			t.Errorf("%q parsed to %d, want 311", r.Token, r.Number)
		}
	}
	if refs[0].Token != "CHR-0311" || refs[1].Token != "CHR-311" || refs[2].Token != "CHR-00311" {
		t.Errorf("tokens were normalised: %+v", refs)
	}
}

func TestReferencesInCodeAreNotMarked(t *testing.T) {
	for _, in := range []string{
		"Code: `SWY-389` stays put.",
		"```\nSWY-389\n```",
		"```go\nconst key = \"CHR-0311\"\n```",
		"    SWY-389 indented four spaces\n",
	} {
		out := renderKeys(t, in)
		if strings.Contains(out, "data-ref-system") {
			t.Errorf("Render(%q) marked a reference inside code:\n%s", in, out)
		}
		if len(scan(in).References) != 0 {
			t.Errorf("Scan(%q) found one inside code", in)
		}
	}
}

func TestScanListsThemInOrder(t *testing.T) {
	refs := scan("First SWY-389, then CHR-0311, then amber1.a.b.0, then SWY-389 again.").References
	want := []Reference{
		{System: SystemSwitchyard, Key: "SWY", Token: "SWY-389", Number: 389},
		{System: SystemChronicle, Key: "CHR", Target: TargetNote, Token: "CHR-0311", Number: 311},
		{System: SystemAmber, Token: "amber1.a.b.0"},
		{System: SystemSwitchyard, Key: "SWY", Token: "SWY-389", Number: 389},
	}
	if len(refs) != len(want) {
		t.Fatalf("Scan = %+v, want %d", refs, len(want))
	}
	for i := range want {
		if refs[i] != want[i] {
			t.Errorf("refs[%d] = %+v, want %+v", i, refs[i], want[i])
		}
	}
}

// Splitting a text run must not eat the newline that followed it. A reference
// at the end of a line is the case that loses it.
func TestASoftLineBreakAfterAReferenceSurvives(t *testing.T) {
	out := renderKeys(t, "Touches SWY-389\nand then the next line.")
	if !strings.Contains(out, "</span>\nand then") {
		t.Errorf("the line break after a trailing reference was lost:\n%s", out)
	}
	out = renderKeys(t, "Touches SWY-389  \nhard break.")
	if !strings.Contains(out, "<br>") {
		t.Errorf("a hard break after a trailing reference was lost:\n%s", out)
	}
}

func TestOrdinaryMarkdownStillRenders(t *testing.T) {
	out := renderKeys(t, "# Title\n\n*em* and **strong** and a [link](https://example.com).\n\n- one\n- two\n")
	for _, want := range []string{"<h1>Title</h1>", "<em>em</em>", "<strong>strong</strong>",
		`<a href="https://example.com">link</a>`, "<li>one</li>"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

func TestMentionsStillWorkAlongsideReferences(t *testing.T) {
	got := Mentions([]byte("@scribe please look at SWY-389, not `@ghost`."))
	if len(got) != 1 || got[0] != "scribe" {
		t.Errorf("Mentions = %v, want [scribe]", got)
	}
}

// plainRender is goldmark with no reference grammar at all — the baseline the
// "nothing is guessed" criteria compare against.
func plainRender(t *testing.T, src string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := goldmark.New().Convert([]byte(src), &buf); err != nil {
		t.Fatalf("plain render: %v", err)
	}
	return buf.String()
}

func uuid2cite(session, record, block string) string {
	return "amber1." + session + "." + record + "." + block
}

// A reference inside image alt text must survive into the alt attribute.
//
// It did not before CHRN-48: goldmark builds an alt by flattening the label to
// plain text, and a childless inline node contributes nothing, so the token was
// silently deleted from the alt while the rest of the sentence survived. Found
// in review of PR #76 and pre-existing — CHR-0311 did it on main too — but this
// change widens the grammar from three prefixes to fifteen project keys plus
// DSC and amber1, so it multiplies the reach of a bug that eats authored text.
func TestAReferenceSurvivesInImageAltText(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"![queue for SWY-389, by hand](x.png)", `alt="queue for SWY-389, by hand"`},
		{"![see CHR-0311](x.png)", `alt="see CHR-0311"`},
		{"![at amber1.a.b.0](x.png)", `alt="at amber1.a.b.0"`},
	} {
		if out := renderKeys(t, tc.in); !strings.Contains(out, tc.want) {
			t.Errorf("Render(%q) lost the token from alt:\n got %s\nwant %s", tc.in, out, tc.want)
		}
	}
	// And the marker itself is unchanged: the renderer still skips children.
	out := renderKeys(t, "Touches SWY-389.")
	if !strings.Contains(out, `data-ref="SWY-389">SWY-389</span>`) {
		t.Errorf("the marker gained a duplicate token:\n%s", out)
	}
}
