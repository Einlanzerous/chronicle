package markdown

import (
	"strings"
	"testing"
)

// CHRN-108's `Done when`, the half this package can prove: tables and
// containers render; injected HTML inside a cell, a container body or a
// container title is neutralised; and a reference inside a cell or a container
// is marked by Render AND returned by Scan. The other half — zero literal `|`
// rows and `:::` fences across the deployed estate wiki — is a sweep over a
// corpus this repository does not hold, recorded as the ticket's evidence.

func assertContains(t *testing.T, in, out string, wants ...string) {
	t.Helper()
	for _, want := range wants {
		if !strings.Contains(out, want) {
			t.Errorf("Render(%q) is missing %q:\n%s", in, want, out)
		}
	}
}

func TestTablesRender(t *testing.T) {
	in := "| service | port |\n| :--- | ---: |\n| chronicle | 4009 |\n"
	out := renderKeys(t, in)
	assertContains(t, in, out, "<table>", "<th", ">service</th>", "<td", ">chronicle</td>", ">4009</td>")
	if strings.Contains(out, "|") {
		t.Errorf("a pipe survived into the output:\n%s", out)
	}
}

// The wiki's key/value sheets open with an empty header row, which is a table
// and not a paragraph of pipes.
func TestATableWithAnEmptyHeaderRowRenders(t *testing.T) {
	in := "|  |  |\n| --- | --- |\n| Prod services | 36 |\n"
	assertContains(t, in, renderKeys(t, in), "<table>", ">Prod services</td>", ">36</td>")
}

func TestStrikethroughAndTaskListsRender(t *testing.T) {
	in := "- [x] shipped\n- [ ] ~~dropped~~\n"
	assertContains(t, in, renderKeys(t, in),
		`<input checked="" disabled="" type="checkbox"> shipped`,
		`<input disabled="" type="checkbox"> <del>dropped</del>`)
}

func TestContainersRender(t *testing.T) {
	for _, tc := range []struct{ kind, defaultTitle string }{
		{"info", "INFO"}, {"tip", "TIP"}, {"warning", "WARNING"}, {"danger", "DANGER"},
	} {
		in := "::: " + tc.kind + "\nThe body, *emphasised*.\n:::\n"
		assertContains(t, in, renderKeys(t, in),
			`<div class="md-container md-container-`+tc.kind+`">`,
			`<p class="md-container-title">`+tc.defaultTitle+`</p>`,
			"<p>The body, <em>emphasised</em>.</p>\n</div>")

		titled := "::: " + tc.kind + " GENERATED PAGE\nbody\n:::\n"
		assertContains(t, titled, renderKeys(t, titled), `<p class="md-container-title">GENERATED PAGE</p>`)
	}

	in := "::: details Click to expand\nhidden\n:::\n"
	assertContains(t, in, renderKeys(t, in),
		`<details class="md-container md-container-details"><summary>Click to expand</summary>`,
		"<p>hidden</p>\n</details>")
}

// CHRN-48 ruling 3's shape: a kind nobody listed is not guessed at. The fence
// lines stay the prose they looked like, and nothing gets a class name taken
// from the source.
func TestAnUnknownContainerKindIsProse(t *testing.T) {
	for _, in := range []string{
		"::: whatever\nbody\n:::\n",
		"::: \" onmouseover=\"x\nbody\n:::\n",
		":::\nno kind at all\n:::\n",
		"::: INFO\nkinds are lowercase, as VitePress has them\n:::\n",
	} {
		out := renderKeys(t, in)
		if strings.Contains(out, "md-container") {
			t.Errorf("Render(%q) opened a container:\n%s", in, out)
		}
		if !strings.Contains(out, "<p>:::") {
			t.Errorf("Render(%q) did not leave the fence as prose:\n%s", in, out)
		}
	}
}

func TestContainerFences(t *testing.T) {
	t.Run("a longer outer fence nests a shorter one", func(t *testing.T) {
		in := ":::: warning OUTER\n::: tip\ninner\n:::\nstill outer\n::::\nafter\n"
		out := renderKeys(t, in)
		assertContains(t, in, out,
			"<p>inner</p>\n</div>\n<p>still outer</p>\n</div>\n<p>after</p>")
	})
	t.Run("a closing fence may be longer than the opening one", func(t *testing.T) {
		in := "::: info\nbody\n:::::\nafter\n"
		assertContains(t, in, renderKeys(t, in), "<p>body</p>\n</div>\n<p>after</p>")
	})
	t.Run("a fence with trailing text does not close", func(t *testing.T) {
		in := "::: info\nbody\n::: not a close\nstill inside\n:::\n"
		assertContains(t, in, renderKeys(t, in), "<p>body\n::: not a close\nstill inside</p>\n</div>")
	})
	t.Run("an unclosed container runs to the end", func(t *testing.T) {
		in := "::: info\nbody\n\nmore\n"
		assertContains(t, in, renderKeys(t, in), "<p>body</p>\n<p>more</p>\n</div>")
	})
	t.Run("a container interrupts a paragraph", func(t *testing.T) {
		in := "lead-in\n::: info\nbody\n:::\n"
		assertContains(t, in, renderKeys(t, in), "<p>lead-in</p>\n<div class=\"md-container md-container-info\">")
	})
	t.Run("a fence indented four spaces is code", func(t *testing.T) {
		in := "    ::: info\n    body\n    :::\n"
		out := renderKeys(t, in)
		if strings.Contains(out, "md-container") || !strings.Contains(out, "<pre><code>::: info") {
			t.Errorf("Render(%q) treated indented code as a container:\n%s", in, out)
		}
	})
	t.Run("a fence inside a code block is code", func(t *testing.T) {
		in := "```\n::: info\n:::\n```\n"
		out := renderKeys(t, in)
		if strings.Contains(out, "md-container") {
			t.Errorf("Render(%q) opened a container inside code:\n%s", in, out)
		}
	})
}

// The dialect adds markup; it must not add a way in for anyone else's.
func TestInjectedHTMLInTheDialectIsNeutralised(t *testing.T) {
	for _, in := range []string{
		"| a | b |\n| --- | --- |\n| <script>alert(1)</script> | <img src=x onerror=alert(1)> |\n",
		"| a |\n| --- |\n| [x](javascript:alert(1)) |\n",
		"::: warning\n<script>alert(1)</script>\n<iframe src=\"https://evil.example\"></iframe>\n:::\n",
		"::: warning <script>alert(1)</script>\nbody\n:::\n",
		"::: details <img src=x onerror=alert(1)>\nbody\n:::\n",
		"::: info \"><svg onload=alert(1)>\nbody\n:::\n",
		"~~<script>alert(1)</script>~~\n",
		"- [ ] <style>body{display:none}</style>\n",
	} {
		out := renderKeys(t, in)
		for _, forbidden := range []string{"<script", "<img src=x", "<iframe", "<svg", "<style", "javascript:"} {
			if strings.Contains(out, forbidden) {
				t.Errorf("Render(%q) leaked %q:\n%s", in, forbidden, out)
			}
		}
	}

	// And a title is text: escaped, not dropped.
	in := "::: warning <b>bold</b> & more\nbody\n:::\n"
	assertContains(t, in, renderKeys(t, in), `<p class="md-container-title">&lt;b&gt;bold&lt;/b&gt; &amp; more</p>`)
}

// The package's own promise — "nothing about WHAT a reference is can differ
// between the two" — held only while Render and Scan read the same dialect. A
// table cell and a container body are where they could now disagree, so both
// are asserted against both.
func TestRenderAndScanAgreeInTablesAndContainers(t *testing.T) {
	in := "| ticket | note |\n| --- | --- |\n| SWY-389 | CHR-0311 |\n\n" +
		"::: info SERV-9 in a title is prose\nSee amber1.a.b.0 and CHRN-108.\n:::\n"

	out := renderKeys(t, in)
	var marked []string
	for _, token := range []string{"SWY-389", "CHR-0311", "amber1.a.b.0", "CHRN-108"} {
		if !strings.Contains(out, `data-ref="`+token+`"`) {
			t.Errorf("Render did not mark %q:\n%s", token, out)
		}
		marked = append(marked, token)
	}
	if strings.Contains(out, `data-ref="SERV-9"`) {
		t.Errorf("a reference in a container title was marked; titles are text:\n%s", out)
	}

	refs := scan(in).References
	if len(refs) != len(marked) {
		t.Fatalf("Scan = %+v, want exactly %v", refs, marked)
	}
	for i, ref := range refs {
		if ref.Token != marked[i] {
			t.Errorf("Scan[%d] = %q, Render marked %q", i, ref.Token, marked[i])
		}
	}

	if got := Mentions([]byte("::: tip\n@scribe, look at this.\n:::\n")); len(got) != 1 || got[0] != "scribe" {
		t.Errorf("Mentions inside a container = %v, want [scribe]", got)
	}
}

// Linkify is left out of the dialect on purpose: an autolinked URL's text is
// not a text run, so a reference inside one would silently stop being a
// reference to Render, to Scan and to the backlink index built from it.
func TestLinkifyIsNotPartOfTheDialect(t *testing.T) {
	in := "Tracked at https://switchyard.example/tickets/SWY-389 today."
	out := renderKeys(t, in)
	if strings.Contains(out, "<a ") {
		t.Errorf("a bare URL was autolinked:\n%s", out)
	}
	if !strings.Contains(out, `data-ref="SWY-389"`) {
		t.Errorf("the reference inside a bare URL stopped being marked:\n%s", out)
	}
	if refs := scan(in).References; len(refs) != 1 || refs[0].Token != "SWY-389" {
		t.Errorf("Scan = %+v, want [SWY-389]", refs)
	}
}
