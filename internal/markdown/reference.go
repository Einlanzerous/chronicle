package markdown

import (
	"strconv"
)

// ============================================================================
// THE GRAMMAR. FOUR NAMESPACES, THREE OWNERS.
// ============================================================================
//
// CHRN-40 shipped this parser against `SY-412` and `AMB-2291`. Both came from
// the canvas, where they are illustrations, and both were read as identifiers.
// Measured on 2026-09-09: no Switchyard project is keyed `SY` — the live keys
// are SWY, CHRN, SERV, AMBR, IDEA and ten more — and `grep -rni sealed` and
// `grep -rn 'AMB-'` over the whole Amber repository return nothing at all.
// Amber has no numeric item id and nothing in it is ever SEALED.
//
// So two of the three shipped kinds named namespaces that do not exist, while
// every real key a note would actually contain rendered as prose. This file is
// the correction, and CHRN-48's plan carries the argument.
//
//	<KEY>-<N>, KEY a live Switchyard project   switchyard   CHRN-49 resolves
//	CHR-####                                   chronicle    a local note
//	DSC-####                                   chronicle    a local discussion
//	amber1.<session>.<record>[.<block>]        amber        CHRN-50 resolves
//
// ============================================================================
// SHAPE IS DECIDED HERE. MEMBERSHIP IS NOT. STATE NEVER IS.
// ============================================================================
//
// Whether a token LOOKS like a reference is decidable from the bytes, and that
// is this file's whole job. Whether `SWY-389` names a live ticket is a question
// only Switchyard can answer, and it arrives through the ProjectKeys predicate.
// What that ticket's title and status are is CHRN-51's, resolved at render time
// and stored nowhere.
//
// ============================================================================
// WHY NOT SWITCHYARD'S WILDCARD, WHICH THE TICKET POINTS AT.
// ============================================================================
//
// `EXTERNAL_REF_KEY_PREFIX=*` matches any `[A-Z][A-Z0-9]{1,9}-\d+` against the
// projects table, and the ticket calls it "the same shape of problem solved on
// the other side". It is — but Switchyard runs it over PR TITLES AND BRANCH
// NAMES, and explicitly refuses bare mentions in PR BODIES, in its own words:
// "PR bodies are free-form prose that frequently mention a key in passing."
//
// A note body is a PR body all the way down, and prose is full of tokens with
// exactly that shape:
//
//	UTF-8   SHA-256   AES-256   ISO-8601   RFC-3986   HTTP-2   COVID-19
//
// Under a wildcard every one of them becomes a card, and a BROKEN one, because
// CHRN-49 requires an unresolvable reference to render visibly broken. The
// false-positive corpus in the tests is the guard: it fails the moment somebody
// widens this to a wildcard. CHRN-48 ruling 1.

// Reference systems, as data-ref-system carries them.
//
// THE SYSTEM IS NOT THE PROJECT KEY. Coral is a property of Switchyard, not of
// the string "SWY", and with fifteen live project keys a marker keyed on the
// project would make the estate colour rule a fifteen-way mapping that every
// client has to duplicate and that grows with the estate.
const (
	SystemSwitchyard = "switchyard"
	SystemAmber      = "amber"
	SystemChronicle  = "chronicle"
)

// What a chronicle reference points at. CHR and DSC share a system and resolve
// against different tables, so the marker carries both.
const (
	TargetNote       = "note"
	TargetDiscussion = "discussion"
)

// The two local prefixes, which are Chronicle's own and never go to Switchyard.
const (
	keyNote       = "CHR"
	keyDiscussion = "DSC"
)

// amberPrefix is Amber's in-band grammar version, and it is matched literally.
//
// AMBR-11 puts the version in the reference itself on purpose: "a citation
// outlives the edition that minted it… A reference found in a two-year-old
// ticket has to say which grammar it was written under." An `amber2` token is
// therefore left as prose rather than guessed at — a grammar this build does
// not understand is not a reference this build can resolve.
const amberPrefix = "amber1."

// ProjectKeys reports whether a key names a live Switchyard project.
//
// DECLARED HERE RATHER THAN IMPORTED, and the reason is INTERFACE WIDTH rather
// than a cycle. scribe.Catalogue (internal/scribe/validate.go) spells
// HasProject identically, and internal/scribe imports nothing of Chronicle's at
// all, so taking it would compile — but it carries THREE methods: HasProject,
// HasPage and HasNote. This package can answer one, and accepting the other two
// would make every caller of NewRenderer implement methods it has no opinion
// about.
//
// A cycle does exist one package further down, which is worth knowing before
// anybody tries to unify the spelling: internal/store imports this package
// (notelink.go) and internal/scribe/catalogue imports internal/store, so the
// IMPLEMENTATION cannot be imported here even though the interface could.
//
// The reuse is therefore structural, which is what it should be:
// *catalogue.Snapshot satisfies this as it stands, with no adapter.
//
// The consequence is worth keeping: internal/markdown stays a LEAF PACKAGE with
// no internal imports and no logger, which is what lets the pure Render be
// tested with no database and no network.
type ProjectKeys interface {
	HasProject(key string) bool
}

// Reference is one estate reference found in a document.
//
// Token is the text exactly as written and is never normalised: `CHR-0311` and
// `CHR-311` both carry Number 311 and their own spelling.
type Reference struct {
	System string // switchyard | amber | chronicle
	Key    string // project key for switchyard, CHR or DSC for chronicle
	Target string // note | discussion, for chronicle only
	Token  string // as written
	Number int64  // for the KEY-N forms; zero for an Amber citation
}

// Result is one pass over a document.
type Result struct {
	References []Reference

	// UnknownKeys are well-shaped keys the predicate rejected, deduplicated and
	// in order of first appearance.
	//
	// RETURNED AS DATA BECAUSE THIS PACKAGE HAS NO LOGGER AND SHOULD NOT GET
	// ONE. A miss renders as ordinary prose, so nothing about the page says
	// anything is wrong, and this slice is the only signal that the key set is
	// stale or that somebody typed `SY-412`. The caller with a logger — CHRN-97's
	// handler — logs it. Adding a dependency to a leaf package for a debug line
	// is the wrong trade.
	UnknownKeys []string
}

// match is one recognised token, or one recorded miss.
type match struct {
	start, end int
	ref        Reference // System empty means a miss: render as prose
	unknownKey string
}

// scanSegment finds references in src[start:stop].
//
// THE BOUNDARY TESTS READ src, NOT THE SEGMENT. A segment is whatever fragment
// the parser handed us — inline structure splits text at emphasis, links and
// code — and a word boundary is a property of the document, not of the
// fragment. Reading src means `*SWY-389*` and `a—SWY-389` get the same answer
// they would get in plain prose.
func scanSegment(src []byte, start, stop int, keys ProjectKeys) []match {
	var out []match
	for i := start; i < stop; {
		// THE LEADING GUARD IS SWITCHYARD'S: the preceding character must not
		// be alphanumeric, so `ABBSWY-1` does not yield `SWY-1`.
		//
		// This is the rule an inline parser CANNOT deliver, and it is why this
		// package recognises over text runs instead. goldmark consults inline
		// parsers only at ASCII punctuation, ASCII whitespace and the head of a
		// line, so an em dash (E2 80 94), a non-breaking space (C2 A0) and a
		// curly quote (E2 80 9C) are never trigger positions whatever Trigger()
		// returns. Measured on v1.7.4 before this change: `—SY-412`, `"SY-412"`,
		// `,SY-412` and ` SY-412` were all prose, while `*SY-412*` marked.
		// Widening the trigger table fixes the ASCII half and can never fix the
		// rest.
		if i > 0 && isAlnum(src[i-1]) {
			i++
			continue
		}
		if m, ok := matchAmber(src, i, stop); ok {
			out = append(out, m)
			i = m.end
			continue
		}
		if m, ok := matchKey(src, i, stop, keys); ok {
			out = append(out, m)
			i = m.end
			continue
		}
		i++
	}
	return out
}

// matchKey recognises `[A-Z][A-Z0-9]{1,9}-[0-9]+`, Switchyard's own PROJECT_KEY
// shape (shared/schemas/common.ts) with a number after it.
func matchKey(src []byte, i, stop int, keys ProjectKeys) (match, bool) {
	if i >= stop || !isUpper(src[i]) {
		return match{}, false
	}
	j := i + 1
	for j < stop && j-i < 10 && isUpperOrDigit(src[j]) {
		j++
	}
	if j-i < 2 || j >= stop || src[j] != '-' {
		return match{}, false
	}
	key := string(src[i:j])
	j++ // the hyphen

	digits := j
	for j < stop && isDigit(src[j]) {
		j++
	}
	if j == digits {
		return match{}, false
	}

	// A reference ends a word: `SWY-389x` and `SWY-389-2` are identifiers
	// somebody wrote, not a ticket followed by something.
	if j < len(src) && isRefWordByte(src[j]) {
		return match{}, false
	}

	n, err := strconv.ParseInt(string(src[digits:j]), 10, 64)
	if err != nil {
		// A run of digits too long for an int64 is not a reference anybody
		// holds. Left as prose rather than reported: this is a sentence.
		return match{}, false
	}

	m := match{start: i, end: j}
	token := string(src[i:j])

	// THE LOCAL NAMESPACES ARE MATCHED BEFORE THE PREDICATE IS CONSULTED, and
	// a key set containing CHR or DSC cannot take them. CLAUDE.md chose the
	// ticket key CHRN over CHR precisely because the two namespaces collide;
	// this is that decision held to at the one place it could be undone.
	switch key {
	case keyNote:
		m.ref = Reference{System: SystemChronicle, Key: key, Target: TargetNote, Token: token, Number: n}
	case keyDiscussion:
		m.ref = Reference{System: SystemChronicle, Key: key, Target: TargetDiscussion, Token: token, Number: n}
	default:
		if keys == nil || !keys.HasProject(key) {
			// Well-shaped and not a live project: recorded, and rendered as the
			// prose it is. CHRN-48 rulings 1 and 3.
			m.unknownKey = key
			return m, true
		}
		m.ref = Reference{System: SystemSwitchyard, Key: key, Token: token, Number: n}
	}
	return m, true
}

// matchAmber recognises `amber1.<session>.<record>[.<block>]`.
//
// ============================================================================
// A STRICTER ALPHABET THAN AMBER'S OWN, DELIBERATELY.
// ============================================================================
//
// Amber's validComponent refuses only the dot separator, `/`, `\`, whitespace
// and control characters — so a quote, an angle bracket and an ampersand are
// all legal in a component it mints. That is right for Amber, which validates
// what it produced. It is WRONG HERE, twice over:
//
//   - This code decides which tokens in PROSE are references. Amber's alphabet
//     would swallow the punctuation around a citation in a sentence.
//   - The marker writes the token into an HTML attribute. Mirroring Amber's
//     alphabet would let `amber1.a"onmouseover=x.b.0` land inside data-ref
//     unescaped, which is an attribute injection.
//
// So a component is [A-Za-z0-9:_-]+ — enough for a uuid (hex and dashes) and
// for the hash-shaped `h:hex` identities cite.go names, and nothing that prose
// or HTML cares about. The renderer escapes the attribute anyway, so the safety
// does not depend on this alphabet staying narrow.
func matchAmber(src []byte, i, stop int) (match, bool) {
	if stop-i < len(amberPrefix) || string(src[i:i+len(amberPrefix)]) != amberPrefix {
		return match{}, false
	}
	j := i + len(amberPrefix)

	// Session, then record. Both required: cite.Parse accepts three or four
	// components and this is the three.
	var ok bool
	if j, ok = scanComponent(src, j, stop); !ok {
		return match{}, false
	}
	if j >= stop || src[j] != '.' {
		return match{}, false
	}
	j++
	if j, ok = scanComponent(src, j, stop); !ok {
		return match{}, false
	}

	// THE BLOCK IS OPTIONAL, AND A TRAILING FULL STOP IS NOT A BLOCK. Consume
	// `.` only when digits follow it, so `…see amber1.a.b.0.` at the end of a
	// sentence keeps its full stop as prose — which is where most citations
	// will be written.
	if j+1 < stop && src[j] == '.' && isDigit(src[j+1]) {
		j++
		for j < stop && isDigit(src[j]) {
			j++
		}
	}

	// A citation ends a word too, on the component alphabet rather than the
	// key one, so a trailing `.` is fine and a trailing letter is not.
	if j < len(src) && isAmberByte(src[j]) {
		return match{}, false
	}

	token := string(src[i:j])
	return match{
		start: i,
		end:   j,
		ref:   Reference{System: SystemAmber, Token: token},
	}, true
}

// scanComponent consumes one Amber component and reports where it ended.
func scanComponent(src []byte, i, stop int) (int, bool) {
	j := i
	for j < stop && isAmberByte(src[j]) {
		j++
	}
	return j, j > i
}

func isUpper(b byte) bool { return b >= 'A' && b <= 'Z' }
func isDigit(b byte) bool { return b >= '0' && b <= '9' }

func isUpperOrDigit(b byte) bool { return isUpper(b) || isDigit(b) }

func isAlnum(b byte) bool {
	return isUpper(b) || isDigit(b) || (b >= 'a' && b <= 'z')
}

// isAmberByte is the component alphabet: uuid characters plus `h:`-shaped ids.
func isAmberByte(b byte) bool {
	return isAlnum(b) || b == ':' || b == '_' || b == '-'
}

// isRefWordByte reports whether b would make a KEY-N reference part of a longer
// word.
//
// `/` is here but NOT in the leading guard, and the asymmetry is deliberate. A
// slash after the key means the key is a directory with something else inside
// it — `SWY-389/comments` — which nobody cited. A slash before it means the key
// is the last segment, which is how a pasted ticket URL ends. CHRN-40 refused
// both because `/` was not in its trigger list; adopting Switchyard's leading
// rule changes that half on purpose.
func isRefWordByte(b byte) bool {
	return isAlnum(b) || b == '-' || b == '_' || b == '/'
}
