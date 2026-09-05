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
// ============================================================================
// REFERENCES ARE MARKED, NEVER RESOLVED.
// ============================================================================
//
// `SY-412` and `AMB-2291` name rows in other systems. CLAUDE.md's second
// invariant says they are linked and never copied, and the failure mode it
// names is exact: resolve one here, bake its title or its status into the
// rendered HTML, and Chronicle now holds a third source of truth that goes
// stale in silence.
//
// So this package emits a MARKER and nothing else:
//
//	<span class="ref ref-sy" data-ref-kind="SY" data-ref="SY-412">SY-412</span>
//
// The token's own text is byte-identical to what was written. The marker
// carries the kind and the token and NOTHING THAT CAN GO STALE — no title, no
// status, no colour, no URL. E7 resolves it at render time into a live card,
// and the estate-wide colour rule (coral is Switchyard, gold is Amber) belongs
// to that card's stylesheet, keyed off data-ref-kind, rather than to anything
// baked in here.
//
// `CHR-####` is marked the same way and is the one kind that is NOT foreign:
// both ends live in Chronicle's own database, which is what CHRN-42 calls the
// one kind of link that is stored. The marker is identical because the
// difference is about where resolution happens, not about how a reference is
// written.
package markdown

import (
	"bytes"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// Reference kinds, as they are written and as data-ref-kind carries them.
const (
	KindSwitchyard = "SY"
	KindAmber      = "AMB"
	KindNote       = "CHR"
)

// refPattern matches an estate reference at the current position.
//
// UPPERCASE ONLY, unlike store.ParseNoteRef which parses leniently. The two
// are different jobs: a person typing `chr-311` into a search box means a
// note, and the same letters inside a sentence of prose usually do not. A
// lenient renderer would mark the word "amb-1" in a half-finished sentence and
// hand E7 a reference to resolve that nobody wrote.
var refPattern = regexp.MustCompile(`^(SY|AMB|CHR)-([0-9]+)`)

// Reference is one estate reference found in a document.
type Reference struct {
	Kind   string // SY, AMB or CHR
	Token  string // the text exactly as written, e.g. "SY-412"
	Number int64
}

// refNode is a marked reference in the AST.
type refNode struct {
	ast.BaseInline
	RefKind string
	Token   string
	Number  int64
}

var refNodeKind = ast.NewNodeKind("EstateReference")

func (n *refNode) Kind() ast.NodeKind { return refNodeKind }

func (n *refNode) Dump(src []byte, level int) {
	ast.DumpHelper(n, src, level, map[string]string{
		"Kind":  n.RefKind,
		"Token": n.Token,
	}, nil)
}

// refParser recognises references in inline text.
//
// It never runs inside a code span or a fenced code block, and that is the
// point rather than an accident: goldmark's code-span parser consumes its
// content whole and inline parsers are not consulted for block-level code at
// all. A note explaining a bug by quoting `SY-412` in backticks is discussing
// a string, not referring to a ticket, and marking it would put a live card in
// the middle of a code sample.
type refParser struct{}

// Trigger follows goldmark's convention, which is not the obvious one and cost
// a debugging pass to find: inline parsers are consulted ONLY at punctuation,
// at whitespace, and at the head of a line — never at a letter in the middle
// of one. Triggering on 'S', 'A' and 'C' registers a parser that is never
// called. `' '` stands for "any whitespace, and a line head", exactly as it
// does in goldmark's own linkify extension.
//
// A reference always begins a word, so those positions are the only ones where
// one can start. `(` and `[` are here because `(SY-412)` is how a reference
// most often appears mid-sentence with no space before it. `*` and `_` are
// deliberately NOT: they are emphasis delimiters, and a parser competing for
// them buys a rare case at the cost of a common one. A reference attached
// directly to some other punctuation — an em dash, say — renders as plain
// text, which is the safe direction to fail in.
func (p *refParser) Trigger() []byte { return []byte{' ', '(', '['} }

func (p *refParser) Parse(parent ast.Node, block text.Reader, _ parser.Context) ast.Node {
	line, segment := block.PeekLine()
	if len(line) == 0 {
		return nil
	}

	// At a delimiter rather than at the reference itself, so step over it —
	// and remember to put it back as text, since consuming it here would eat
	// the space before every marked reference.
	consumes := 0
	if isRefLead(line[0]) {
		consumes = 1
		line = line[1:]
	}

	m := refPattern.FindSubmatchIndex(line)
	if m == nil {
		return nil
	}
	end := m[1]
	// A reference ends a word too. `SY-412x` is an identifier somebody wrote,
	// not a ticket followed by a letter.
	if end < len(line) && isRefWordByte(line[end]) {
		return nil
	}

	n, err := strconv.ParseInt(string(line[m[4]:m[5]]), 10, 64)
	if err != nil {
		// A run of digits too long for an int64 is not a reference anybody
		// holds. Left as ordinary text rather than reported: this is prose.
		return nil
	}

	if consumes > 0 {
		ast.MergeOrAppendTextSegment(parent, segment.WithStop(segment.Start+consumes))
	}
	block.Advance(consumes + end)
	return &refNode{
		RefKind: string(line[m[2]:m[3]]),
		Token:   string(line[:end]),
		Number:  n,
	}
}

// isRefLead reports whether b is a delimiter a reference may follow directly.
// It must agree with Trigger, plus the whitespace that `' '` stands in for.
func isRefLead(b byte) bool {
	switch b {
	case ' ', '\t', '(', '[':
		return true
	}
	return false
}

// isRefWordByte reports whether b would make a reference part of a longer word.
func isRefWordByte(b byte) bool {
	switch {
	case b >= 'a' && b <= 'z', b >= 'A' && b <= 'Z', b >= '0' && b <= '9':
		return true
	case b == '-' || b == '_' || b == '/':
		return true
	}
	return false
}

type refRenderer struct{}

func (r *refRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(refNodeKind, r.render)
}

func (r *refRenderer) render(w util.BufWriter, _ []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	if !entering {
		return ast.WalkContinue, nil
	}
	n := node.(*refNode)
	// No escaping needed and none omitted by oversight: refPattern admits only
	// [A-Z]+-[0-9]+, so neither the attribute nor the text can carry a quote,
	// an angle bracket or an ampersand.
	// The writer is goldmark's buffered one; a write error here surfaces from
	// Convert when the buffer is flushed, so there is nothing this function
	// could do with it that Render does not already do.
	_, _ = fmt.Fprintf(w, `<span class="ref ref-%s" data-ref-kind="%s" data-ref="%s">%s</span>`,
		strings.ToLower(n.RefKind), n.RefKind, n.Token, n.Token)
	return ast.WalkSkipChildren, nil
}

type refExtension struct{}

func (e *refExtension) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithInlineParsers(
		util.Prioritized(&refParser{}, 500)))
	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(&refRenderer{}, 500)))
}

// md is the one configured pipeline.
//
// html.WithUnsafe() IS DELIBERATELY ABSENT, and it is the whole of the
// sanitising story. Without it goldmark does not pass raw HTML from the source
// through to the output, so a note containing a script tag renders as
// neutralised text rather than as a script. There is no allow-list sanitiser
// downstream because there is nothing for one to remove: the renderer is the
// only thing that writes HTML here, and it writes only what it constructs
// itself. A second library scrubbing the output would be defending against a
// path that does not exist, and would invite somebody to add WithUnsafe()
// later on the grounds that the scrubber will catch it.
var md = goldmark.New(goldmark.WithExtensions(&refExtension{}))

// Render turns stored markdown into HTML that is safe to embed in a page.
//
// It never mutates src and never returns it: the authored bytes stay exactly
// as they were stored.
func Render(src []byte) ([]byte, error) {
	var buf bytes.Buffer
	if err := md.Convert(src, &buf); err != nil {
		return nil, fmt.Errorf("markdown: render: %w", err)
	}
	return buf.Bytes(), nil
}

// References returns every estate reference in a document, in the order they
// appear, skipping anything inside code. Duplicates are kept — a note that
// mentions SY-412 three times refers to it three times.
func References(src []byte) []Reference {
	doc := md.Parser().Parse(text.NewReader(src))
	var out []Reference
	_ = ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		if r, ok := n.(*refNode); ok {
			out = append(out, Reference{Kind: r.RefKind, Token: r.Token, Number: r.Number})
		}
		return ast.WalkContinue, nil
	})
	return out
}
