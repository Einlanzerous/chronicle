package markdown

import (
	"fmt"
	"html"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// ============================================================================
// THE DIALECT IS THE ESTATE WIKI'S, BECAUSE THE TIER-1 PANE RENDERS IT.
// ============================================================================
//
// CHRN-40 shipped CommonMark and nothing else. The estate wiki the tier-1 pane
// mounts (SERV-101, CHRN-100) is written for VitePress: seventy of its
// seventy-two pages carry GFM pipe tables and seventy-five `:::` containers,
// and CHRN-108 found both reaching a browser as literal `|` rows and fence
// lines. Notes and discussion turns go through the same renderer, so the
// dialect is one decision for all three surfaces rather than a tier-1 special
// case.
//
// LINKIFY IS DELIBERATELY ABSENT. It turns `https://…/SWY-389` into an autolink
// whose text is no longer an ast.Text run, so the reference inside it would
// stop being marked by Render and stop being returned by Scan — which feeds
// the backlink index. What counts as a reference is CHRN-48's grammar, not a
// rendering detail, and a dialect change must not move it by accident.
//
// Raw HTML is still dropped: nothing here adds html.WithUnsafe(), and every
// byte of HTML the extensions below emit is markup they construct themselves.
func dialect() []goldmark.Extender {
	return []goldmark.Extender{
		extension.Table,
		extension.Strikethrough,
		extension.TaskList,
		containers{},
	}
}

// containerTitles is the fixed set of container kinds, with VitePress's
// default title for each.
//
// A FIXED LIST, NOT A PATTERN. The kind becomes a class name in the output, so
// it is taken from this map and never from the source bytes, and a kind that
// is not here is not a container: `::: whatever` stays the prose it looked
// like, on CHRN-48 ruling 3's shape — an unknown token is left as text rather
// than guessed at.
var containerTitles = map[string]string{
	"info":    "INFO",
	"tip":     "TIP",
	"warning": "WARNING",
	"danger":  "DANGER",
	"details": "Details",
}

// containerNode is a `::: kind title` block.
type containerNode struct {
	ast.BaseBlock
	kind  string
	title text.Segment
	fence int
}

var containerNodeKind = ast.NewNodeKind("Container")

func (n *containerNode) Kind() ast.NodeKind { return containerNodeKind }

func (n *containerNode) Dump(src []byte, level int) {
	ast.DumpHelper(n, src, level, map[string]string{
		"Kind":  n.kind,
		"Title": string(n.title.Value(src)),
	}, nil)
}

// containerParser reads markdown-it-container's syntax, which is VitePress's:
// an opening fence of three or more colons, a kind and an optional title; a
// closing fence of at least as many colons and nothing else. A longer outer
// fence nests a shorter one. An unclosed container runs to the end of the
// document, as it does there.
type containerParser struct{}

func (p *containerParser) Trigger() []byte { return []byte{':'} }

func (p *containerParser) Open(_ ast.Node, reader text.Reader, pc parser.Context) (ast.Node, parser.State) {
	line, segment := reader.PeekLine()
	pos := pc.BlockOffset()
	if pos < 0 {
		return nil, parser.NoChildren
	}
	i := pos
	for i < len(line) && line[i] == ':' {
		i++
	}
	fence := i - pos
	if fence < 3 {
		return nil, parser.NoChildren
	}

	rest := line[i:]
	left := util.TrimLeftSpaceLength(rest)
	right := util.TrimRightSpaceLength(rest)
	if left >= len(rest)-right {
		return nil, parser.NoChildren // a bare fence names no kind
	}
	info := rest[left : len(rest)-right]
	kind := string(info)
	if end := strings.IndexAny(kind, " \t"); end >= 0 {
		kind = kind[:end]
	}
	if _, ok := containerTitles[kind]; !ok {
		return nil, parser.NoChildren
	}

	node := &containerNode{kind: kind, fence: fence}
	// The title is whatever follows the kind, as a segment of the source: it is
	// rendered escaped and never parsed, so a reference in a title is prose to
	// Render and to Scan alike.
	titleAt := i + left + len(kind)
	titleAt += util.TrimLeftSpaceLength(line[titleAt : len(line)-right])
	start := segment.Start - segment.Padding + titleAt
	stop := segment.Stop - right
	if start < stop {
		node.title = text.NewSegment(start, stop)
	}

	// Consume the fence line, so the parser does not offer its remainder to a
	// child block and read `::: info TITLE` back as a paragraph.
	reader.Advance(segment.Stop - segment.Start - trailingNewline(line) + segment.Padding)
	return node, parser.HasChildren
}

func (p *containerParser) Continue(node ast.Node, reader text.Reader, _ parser.Context) parser.State {
	line, segment := reader.PeekLine()
	fence := node.(*containerNode).fence

	w, pos := util.IndentWidth(line, reader.LineOffset())
	if w < 4 {
		i := pos
		for i < len(line) && line[i] == ':' {
			i++
		}
		if i-pos >= fence && util.IsBlank(line[i:]) {
			reader.Advance(segment.Stop - segment.Start - trailingNewline(line) + segment.Padding)
			return parser.Close
		}
	}
	return parser.Continue | parser.HasChildren
}

func (p *containerParser) Close(ast.Node, text.Reader, parser.Context) {}

func (p *containerParser) CanInterruptParagraph() bool { return true }

func (p *containerParser) CanAcceptIndentedLine() bool { return false }

func trailingNewline(line []byte) int {
	if len(line) > 0 && line[len(line)-1] == '\n' {
		return 1
	}
	return 0
}

type containerRenderer struct{}

func (r *containerRenderer) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(containerNodeKind, r.render)
}

// render writes the container's markup. The kind is a key of containerTitles,
// never source bytes; the title is escaped. Neither can carry markup in.
func (r *containerRenderer) render(w util.BufWriter, src []byte, node ast.Node, entering bool) (ast.WalkStatus, error) {
	n := node.(*containerNode)
	if n.kind == "details" {
		if entering {
			_, _ = fmt.Fprintf(w, "<details class=\"md-container md-container-details\"><summary>%s</summary>\n",
				html.EscapeString(n.titleText(src)))
		} else {
			_, _ = w.WriteString("</details>\n")
		}
		return ast.WalkContinue, nil
	}
	if entering {
		_, _ = fmt.Fprintf(w, "<div class=\"md-container md-container-%s\">\n<p class=\"md-container-title\">%s</p>\n",
			n.kind, html.EscapeString(n.titleText(src)))
	} else {
		_, _ = w.WriteString("</div>\n")
	}
	return ast.WalkContinue, nil
}

func (n *containerNode) titleText(src []byte) string {
	if n.title.Len() > 0 {
		return string(n.title.Value(src))
	}
	return containerTitles[n.kind]
}

type containers struct{}

func (containers) Extend(m goldmark.Markdown) {
	m.Parser().AddOptions(parser.WithBlockParsers(
		util.Prioritized(&containerParser{}, 700)))
	m.Renderer().AddOptions(renderer.WithNodeRenderers(
		util.Prioritized(&containerRenderer{}, 500)))
}
