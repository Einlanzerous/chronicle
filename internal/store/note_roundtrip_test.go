package store

import (
	"testing"
)

// CHRN-40's first `Done when`: notes round-trip byte-for-byte.
//
// IT IS A CLAIM ABOUT STORAGE, so it is asserted against the real database
// rather than against a buffer in internal/markdown. What it rules out is a
// pipeline that normalises on the way in — trailing whitespace stripped, CRLF
// folded to LF, a BOM eaten, smart quotes substituted. Every one of those is a
// silent edit to authored text in the store whose whole justification is that
// its contents cannot be regenerated.

func TestNotesRoundTripByteForByte(t *testing.T) {
	s, ctx := newTestStore(t)
	page, author := notePage(t, s, ctx, "roundtrip@example.com")

	// Each of these has bitten a markdown pipeline somewhere.
	bodies := map[string]string{
		"trailing whitespace":               "a line with two trailing spaces  \nand another\n",
		"CRLF":                              "windows\r\nline\r\nendings\r\n",
		"no trailing newline":               "no newline at the end",
		"leading blank lines":               "\n\n\nthree blank lines first",
		"tabs":                              "\tindented\twith\ttabs",
		"BOM":                               "\ufeffbyte order mark first",
		"unicode":                           "café — naïve “smart quotes” … 🎙 日本語",
		"nul-adjacent control":              "bell\x07 and vertical tab\x0b",
		"markdown that looks reformattable": "#heading with no space\n*   loose   list\n-  another\n",
		"references":                        "See SY-412 and AMB-2291 and CHR-0311 and `SY-9`.",
		"empty":                             "",
		"only whitespace":                   "   \n\t\n  ",
	}

	for name, body := range bodies {
		t.Run(name, func(t *testing.T) {
			n, _, err := s.CreateNote(ctx, NewNote{
				PageID: page, AuthorID: author, ConfirmedBy: author, Title: name, Body: body,
			})
			if err != nil {
				t.Fatalf("CreateNote: %v", err)
			}
			got, err := s.CurrentRevision(ctx, n.ID)
			if err != nil {
				t.Fatalf("CurrentRevision: %v", err)
			}
			if got.Body != body {
				t.Errorf("body round-trip changed the bytes:\n got %q\nwant %q", got.Body, body)
			}
			if got.Title != name {
				t.Errorf("title round-trip changed the bytes:\n got %q\nwant %q", got.Title, name)
			}

			// And again through an append, since that is the other write path.
			rev, err := s.AppendRevision(ctx, n.ID, NewRevision{
				AuthorID: author, ConfirmedBy: author, Title: name, Body: body,
			})
			if err != nil {
				t.Fatalf("AppendRevision: %v", err)
			}
			if rev.Body != body {
				t.Errorf("append round-trip changed the bytes:\n got %q\nwant %q", rev.Body, body)
			}
		})
	}
}
