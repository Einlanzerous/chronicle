// Package markdown is Chronicle's note pipeline: store raw, render on read,
// sanitise on the way out — the same split Switchyard uses.
//
// ============================================================================
// THE STORED BYTES ARE THE AUTHORED BYTES.
// ============================================================================
//
// Nothing here normalises, reformats or re-serialises a note. There is no
// parse-then-write path in this package at all, and that is deliberate: a
// pipeline that tidies markdown on the way in is a pipeline that silently
// edits what a person wrote, in the one store whose whole justification is
// that its contents cannot be regenerated. Render is a pure function from
// stored bytes to HTML, and the stored bytes are never its output.
//
// That is also what meets CHRN-48's "references round-trip through the editor
// unchanged" before any editor exists: there is no write path to round-trip
// through. A reference survives an edit because nothing here ever rewrites the
// text it was written in.
//
// ============================================================================
// REFERENCES ARE MARKED, NEVER RESOLVED.
// ============================================================================
//
// `SWY-389` and `amber1.…` name rows in other systems. CLAUDE.md's second
// invariant says they are linked and never copied, and the failure mode it
// names is exact: resolve one here, bake its title or its status into the
// rendered HTML, and Chronicle now holds a third source of truth that goes
// stale in silence.
//
// So this package emits a MARKER and nothing else:
//
//	<span class="ref ref-switchyard" data-ref-system="switchyard"
//	      data-ref-key="SWY" data-ref="SWY-389">SWY-389</span>
//
// The token's own text is byte-identical to what was written. The marker
// carries the system, the key and the token, and NOTHING THAT CAN GO STALE —
// no title, no status, no colour, no URL. E7 resolves it at render time into a
// live card, and the estate-wide colour rule (coral is Switchyard, gold is
// Amber) belongs to that card's stylesheet, keyed off data-ref-system, rather
// than to anything baked in here.
//
// THE SYSTEM AND THE KEY ARE SEPARATE ATTRIBUTES, which is CHRN-48's amendment
// to CHRN-40's marker. Colour belongs to Switchyard, not to any one project, and
// a marker keyed on the project would make an estate rule a fifteen-way mapping
// that grows every time the estate gains a repository. Changing the marker cost
// no migration and no backfill — the marker is render output and the stored
// bytes are the authored bytes, which is exactly the property that would have
// been lost had a reference ever been flattened at write time.
//
// `CHR-####` and `DSC-####` are marked the same way and are the kinds that are
// NOT foreign: both ends live in Chronicle's own database. The marker is
// identical because the difference is about where resolution happens, not about
// how a reference is written.
//
// The grammar, and why it is not Switchyard's wildcard, is in reference.go.
package markdown

import (
	"bytes"
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// refNode is a marked reference in the AST.
type refNode struct {
	ast.BaseInline
	ref Reference
}

var refNodeKind = ast.NewNodeKind("EstateReference")

func (n *refNode) Kind() ast.NodeKind { return refNodeKind }

func (n *refNode) Dump(src []byte, level int) {
	ast.DumpHelper(n, src, level, map[string]string{
		"System": n.ref.System,
		"Key":    n.ref.Key,
		"Token":  n.ref.Token,
	}, nil)
}

// refTransformer marks references after the inline pass, over text runs.
//
// ============================================================================
// WHY A TRANSFORMER AND NOT AN INLINE PARSER.
// ============================================================================
//
// CHRN-40 used an inline parser, and goldmark consults those at ASCII
// punctuation, at ASCII whitespace and at the head of a line — never in the
// middle of a run and never at a multi-byte character. That is not a Trigger()
// list that can be widened: util.punctTable is zero for every byte above 0x7f,
// so an em dash, a non-breaking space and a curly quote are not trigger
// positions at all. Measured before this change: `—SY-412`, `"SY-412"` and
// ` SY-412` were prose while `*SY-412*` marked, which is a boundary nobody
// chose.
//
// Recognising over text runs instead makes the rule literally Switchyard's —
// the shape, plus "the preceding character is not alphanumeric" — and it is the
// mechanism this package already trusts for @handles. The one recogniser,
// scanSegment, serves both this and Scan, so the check and the consequence
// cannot disagree about what a reference is.
type refTransformer struct{ keys ProjectKeys }

func (t *refTransformer) Transform(doc *ast.Document, reader text.Reader, _ parser.Context) {
	src := reader.Source()
	// Collected before any mutation: splicing rewrites the child lists this
	// walk would otherwise be standing in.
	for _, txt := range collectText(doc) {
		t.mark(txt, src)
	}
}

// mark splits one text node into text and reference nodes.
func (t *refTransformer) mark(txt *ast.Text, src []byte) {
	seg := txt.Segment

	var nodes []ast.Node
	prev := seg.Start
	for _, m := range scanSegment(src, seg.Start, seg.Stop, t.keys) {
		if m.ref.System == "" {
			// A recorded miss. It stays exactly the prose it looked like.
			continue
		}
		if m.start > prev {
			nodes = append(nodes, ast.NewTextSegment(text.NewSegment(prev, m.start)))
		}
		nodes = append(nodes, &refNode{ref: m.ref})
		prev = m.end
	}
	if len(nodes) == 0 {
		return
	}

	// THE LINE-BREAK FLAGS BELONG TO THE LAST PIECE, and the last piece may be
	// empty. A text node ending in a reference still ends in the newline that
	// followed it, and dropping the flag would run the next line on. An empty
	// trailing segment renders nothing and carries the break.
	tail := ast.NewTextSegment(text.NewSegment(prev, seg.Stop))
	tail.SetSoftLineBreak(txt.SoftLineBreak())
	tail.SetHardLineBreak(txt.HardLineBreak())
	nodes = append(nodes, tail)

	parent := txt.Parent()
	for _, n := range nodes {
		parent.InsertBefore(parent, txt, n)
	}
	parent.RemoveChild(parent, txt)
}

// collectText gathers the text runs a reference may appear in.
//
// IT SKIPS CODE, which is the rule CHRN-40 argued and this keeps: a note
// explaining a bug by quoting `SWY-389` in backticks is discussing a string,
// not referring to a ticket, and marking it would put a live card in the middle
// of a code sample.
func collectText(doc ast.Node) []*ast.Text {
	var out []*ast.Text
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n.Kind() {
		case ast.KindCodeSpan, ast.KindCodeBlock, ast.KindFencedCodeBlock:
			return ast.WalkSkipChildren, nil
		case ast.KindText:
			out = append(out, n.(*ast.Text))
		}
		return ast.WalkContinue, nil
	})
	return out
}

type refRenderer struct{}

func (r *refRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(refNodeKind, r.render)
}

func (r *refRenderer) render(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*refNode).ref

	// THE ATTRIBUTES ARE ESCAPED, AND THE SAFETY DOES NOT REST ON THE GRAMMAR.
	// CHRN-40 wrote them raw and justified it on refPattern admitting only
	// [A-Z]+-[0-9]+. That argument does not survive Amber citations, whose
	// components are not restricted to letters and digits — reference.go keeps
	// the admitted alphabet prose-safe, and this escapes anyway, so a later
	// widening of one cannot silently become an injection through the other.
	// System and Target are package constants and cannot vary.
	_, _ = fmt.Fprintf(w, `<span class="ref ref-%s" data-ref-system="%s"`, n.System, n.System)
	if n.Key != "" {
		_, _ = fmt.Fprintf(w, ` data-ref-key="%s"`, html.EscapeString(n.Key))
	}
	if n.Target != "" {
		_, _ = fmt.Fprintf(w, ` data-ref-target="%s"`, n.Target)
	}
	// The writer is goldmark's buffered one; a write error here surfaces from
	// Convert when the buffer is flushed, so there is nothing this function
	// could do with it that Render does not already do.
	_, _ = fmt.Fprintf(w, ` data-ref="%s">%s</span>`,
		html.EscapeString(n.Token), html.EscapeString(n.Token))
	return ast.WalkSkipChildren, nil
}

type refExtension struct{ keys ProjectKeys }

func (e *refExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithASTTransformers(
		util.Prioritized(&refTransformer{keys: e.keys}, 500)))
	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(&refRenderer{}, 500)))
}

// Renderer renders notes against a set of live Switchyard project keys.
//
// One is built per key set and reused; constructing it configures a goldmark
// pipeline, which is not something to do per render.
type Renderer struct {
	md   goldmark.Markdown
	keys ProjectKeys
}

// NewRenderer returns a renderer that marks a KEY-N token only when keys
// accepts its project.
//
// A NIL PREDICATE IS THE PURE PATH and is not a degenerate case: it marks the
// namespaces that are certain from the token alone — CHR, DSC and amber1 — and
// leaves every ticket-shaped token as prose. CHRN-48 ruling 3: an unknown
// prefix is left as plain text rather than guessed at, and "unknown" includes
// "nobody asked".
func NewRenderer(keys ProjectKeys) *Renderer {
	return &Renderer{
		// html.WithUnsafe() IS DELIBERATELY ABSENT, and it is the whole of the
		// sanitising story. Without it goldmark does not pass raw HTML from the
		// source through to the output, so a note containing a script tag
		// renders as neutralised text rather than as a script. There is no
		// allow-list sanitiser downstream because there is nothing for one to
		// remove: the renderer is the only thing that writes HTML here, and it
		// writes only what it constructs itself. A second library scrubbing the
		// output would be defending against a path that does not exist, and
		// would invite somebody to add WithUnsafe() later on the grounds that
		// the scrubber will catch it.
		md:   goldmark.New(goldmark.WithExtensions(&refExtension{keys: keys})),
		keys: keys,
	}
}

// defaultRenderer is the pure pipeline: no predicate, no network, no guessing.
var defaultRenderer = NewRenderer(nil)

// plainParser has no reference transformer, so its text nodes are intact.
//
// Scan and Mentions walk it rather than a marked tree. They share scanSegment
// with the transformer, so nothing about WHAT a reference is can differ between
// the two; only whether the tree was rewritten.
var plainParser = goldmark.New().Parser()

// Render turns stored markdown into HTML that is safe to embed in a page.
//
// It never mutates src and never returns it: the authored bytes stay exactly
// as they were stored.
func (r *Renderer) Render(src []byte) ([]byte, error) {
	var buf bytes.Buffer
	if err := r.md.Convert(src, &buf); err != nil {
		return nil, fmt.Errorf("markdown: render: %w", err)
	}
	return buf.Bytes(), nil
}

// Scan returns every estate reference in a document, in the order they appear,
// skipping anything inside code, together with the well-shaped keys the
// predicate rejected.
//
// Duplicates are kept — a note that mentions SWY-389 three times refers to it
// three times — while UnknownKeys is deduplicated, because it is a signal about
// the key set rather than about the document.
func (r *Renderer) Scan(src []byte) Result {
	doc := plainParser.Parse(text.NewReader(src))
	res := Result{}
	seen := map[string]bool{}
	for _, txt := range collectText(doc) {
		for _, m := range scanSegment(src, txt.Segment.Start, txt.Segment.Stop, r.keys) {
			switch {
			case m.ref.System != "":
				res.References = append(res.References, m.ref)
			case m.unknownKey != "" && !seen[m.unknownKey]:
				seen[m.unknownKey] = true
				res.UnknownKeys = append(res.UnknownKeys, m.unknownKey)
			}
		}
	}
	return res
}

// Render renders against no key set. See NewRenderer.
func Render(src []byte) ([]byte, error) { return defaultRenderer.Render(src) }

// References returns every estate reference a document names with certainty —
// notes, discussions and Amber citations. Ticket references need a key set; see
// NewRenderer and Renderer.Scan.
func References(src []byte) []Reference { return defaultRenderer.Scan(src).References }

// mentionPattern matches an @handle. Letters, digits, hyphen and underscore,
// which is what an account name can hold; the leading @ is required.
//
// LOWERCASE-INSENSITIVE, unlike the reference grammar, and for the opposite
// reason. A reference written `chr-311` in prose is usually not a reference, so
// recognition is strict. A person typing `@Scribe` at the start of a sentence
// plainly means the Scribe, and refusing to notice would make the trigger feel
// broken rather than precise.
var mentionPattern = regexp.MustCompile(`@([A-Za-z0-9_-]+)`)

// Mentions returns every @handle in a document, lowercased, in the order they
// appear and without duplicates.
//
// IT SKIPS CODE, which is the whole reason this lives here rather than being a
// regex at the call site. CHRN-47 turns a mention into an agent reply, so a
// person writing about the feature — "reply when somebody writes `@scribe`" —
// would otherwise summon the thing they were describing. Code spans and code
// blocks are where people put the example, and goldmark's AST is what already
// knows the difference.
func Mentions(src []byte) []string {
	doc := plainParser.Parse(text.NewReader(src))
	seen := map[string]bool{}
	var out []string
	for _, txt := range collectText(doc) {
		for _, m := range mentionPattern.FindAllSubmatch(txt.Segment.Value(src), -1) {
			h := strings.ToLower(string(m[1]))
			if !seen[h] {
				seen[h] = true
				out = append(out, h)
			}
		}
	}
	return out
}
