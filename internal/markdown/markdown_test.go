package markdown

import (
	"bytes"
	"strings"
	"testing"
)

// CHRN-40's `Done when`: notes round-trip byte-for-byte, rendered output is
// safe against injected HTML, and reference tokens survive the trip untouched.
//
// The round-trip half is asserted against the real store, in
// internal/store/note_roundtrip_test.go — byte-for-byte is a claim about
// storage, and proving it against a buffer in this package would prove
// nothing.

func render(t *testing.T, src string) string {
	t.Helper()
	out, err := Render([]byte(src))
	if err != nil {
		t.Fatalf("Render(%q): %v", src, err)
	}
	return string(out)
}

// `Done when` #2, first half: raw HTML in a note never reaches the page.
func TestRawHTMLIsNeutralised(t *testing.T) {
	for _, in := range []string{
		`<script>alert(1)</script>`,
		`<img src=x onerror=alert(1)>`,
		`<a href="javascript:alert(1)">x</a>`,
		`<iframe src="https://evil.example"></iframe>`,
		`<style>body{display:none}</style>`,
		`<svg><animate onbegin=alert(1) /></svg>`,
		"Mixed <b>bold</b> with SY-412 alongside",
	} {
		out := render(t, in)
		for _, forbidden := range []string{"<script", "<img src=x", "<iframe", "<style", "onerror", "onbegin"} {
			if strings.Contains(out, forbidden) {
				t.Errorf("Render(%q) leaked %q:\n%s", in, forbidden, out)
			}
		}
	}
}

// The other way HTML gets injected: a link whose scheme executes.
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
	// An ordinary link is untouched, so the check above is not passing by
	// blanking everything.
	if out := render(t, `[ok](https://example.com/a)`); !strings.Contains(out, `href="https://example.com/a"`) {
		t.Errorf("an ordinary link did not survive: %s", out)
	}
}

// `Done when` #3: the token text is byte-identical in the output.
func TestReferenceTokensSurviveUntouched(t *testing.T) {
	for _, tc := range []struct{ in, token, kind string }{
		{"See SY-412 for the ledger.", "SY-412", "SY"},
		{"AMB-2291 is sealed.", "AMB-2291", "AMB"},
		{"Compare CHR-0311 with this.", "CHR-0311", "CHR"},
		{"In parens (SY-412) mid-sentence.", "SY-412", "SY"},
		{"Bracketed [AMB-1] here.", "AMB-1", "AMB"},
		{"Trailing punctuation SY-9, and SY-10.", "SY-9", "SY"},
	} {
		out := render(t, tc.in)
		if !strings.Contains(out, ">"+tc.token+"<") {
			t.Errorf("Render(%q): token %q is not present verbatim:\n%s", tc.in, tc.token, out)
		}
		if !strings.Contains(out, `data-ref-kind="`+tc.kind+`"`) {
			t.Errorf("Render(%q): kind %q not marked:\n%s", tc.in, tc.kind, out)
		}
		// CHR-0311 keeps its leading zero: the marker records what was
		// written, not a normalised form of it.
		if !strings.Contains(out, `data-ref="`+tc.token+`"`) {
			t.Errorf("Render(%q): data-ref is not the written token:\n%s", tc.in, out)
		}
	}
}

// INVARIANT 2, at the renderer. The marker may carry the token and its kind
// and nothing that belongs to the upstream system, because anything else is a
// copy that goes stale in silence.
func TestTheMarkerCarriesNoUpstreamState(t *testing.T) {
	out := render(t, "See SY-412 and AMB-2291.")
	for _, forbidden := range []string{
		"href=", "IN PROGRESS", "SEALED", "status", "title=", "coral", "gold",
	} {
		if strings.Contains(out, forbidden) {
			t.Errorf("the marker carries %q, which is upstream state or presentation:\n%s", forbidden, out)
		}
	}
}

// A note explaining a bug by quoting a ticket key in backticks is discussing a
// string, not referring to a ticket. Marking it would drop a live card into the
// middle of a code sample.
func TestReferencesInCodeAreNotMarked(t *testing.T) {
	for _, in := range []string{
		"Code: `SY-412` stays put.",
		"```\nSY-412\n```",
		"```go\nconst key = \"AMB-2291\"\n```",
		"    SY-412 indented four spaces\n",
	} {
		out := render(t, in)
		if strings.Contains(out, "data-ref-kind") {
			t.Errorf("Render(%q) marked a reference inside code:\n%s", in, out)
		}
		if len(References([]byte(in))) != 0 {
			t.Errorf("References(%q) found one inside code", in)
		}
	}
}

// A reference is a whole word or it is not a reference.
func TestReferenceWordBoundaries(t *testing.T) {
	for _, in := range []string{
		"FOO-AMB-1 is an identifier",
		"SY-412x is not a ticket",
		"SY- on its own",
		"sy-412 is lowercase prose",
		"path/SY-412 is a filename",
		"SY-412_backup names a file",
	} {
		if out := render(t, in); strings.Contains(out, "data-ref-kind") {
			t.Errorf("Render(%q) marked a non-reference:\n%s", in, out)
		}
	}
	// ...and the same strings with a real reference alongside still find it.
	out := render(t, "FOO-AMB-1 and sy-412 but also SY-412.")
	if n := strings.Count(out, "data-ref-kind"); n != 1 {
		t.Errorf("marked %d references, want exactly the one real one:\n%s", n, out)
	}
}

func TestReferencesListsThemInOrder(t *testing.T) {
	refs := References([]byte("First SY-412, then AMB-2291, then CHR-0311, then SY-412 again."))
	want := []Reference{
		{Kind: KindSwitchyard, Token: "SY-412", Number: 412},
		{Kind: KindAmber, Token: "AMB-2291", Number: 2291},
		{Kind: KindNote, Token: "CHR-0311", Number: 311},
		{Kind: KindSwitchyard, Token: "SY-412", Number: 412},
	}
	if len(refs) != len(want) {
		t.Fatalf("References = %+v, want %d", refs, len(want))
	}
	for i := range want {
		if refs[i] != want[i] {
			t.Errorf("refs[%d] = %+v, want %+v", i, refs[i], want[i])
		}
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

// Render is a pure function of its input. A pipeline that tidies markdown is a
// pipeline that silently edits what a person wrote.
func TestRenderDoesNotMutateItsInput(t *testing.T) {
	src := []byte("# Heading\n\nSee SY-412.\n\n```\nSY-9\n```\n")
	before := bytes.Clone(src)
	if _, err := Render(src); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !bytes.Equal(src, before) {
		t.Errorf("Render mutated its input:\n got %q\nwant %q", src, before)
	}
	References(src)
	if !bytes.Equal(src, before) {
		t.Errorf("References mutated its input:\n got %q\nwant %q", src, before)
	}
}

// Ordinary markdown still works — the extension does not cost the pipeline its
// day job.
func TestOrdinaryMarkdownStillRenders(t *testing.T) {
	out := render(t, "# Title\n\n*em* and **strong** and a [link](https://example.com).\n\n- one\n- two\n")
	for _, want := range []string{"<h1>Title</h1>", "<em>em</em>", "<strong>strong</strong>",
		`<a href="https://example.com">link</a>`, "<li>one</li>"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}
