package audio

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// The audio part's media type comes from what the recording IS, not from what
// it is called — a filename is display-only in this system.
//
// Moved here from internal/transcribe with MediaType itself (CHRN-107 ruling
// 5), unchanged but for the signature: the table is the ASR submission's and
// now covers the audio stream's Content-Type as well, because both callers ask
// this one function.
func TestMediaType(t *testing.T) {
	opus := "opus"
	webm := "memo.webm"
	m4a := "voice.M4A"
	odd := "recording.xyz"

	cases := []struct {
		name            string
		codec, filename *string
		want            string
	}{
		{"codec wins over filename", &opus, &webm, "audio/ogg"},
		{"filename when there is no codec", nil, &webm, "audio/webm"},
		{"extensions are case-insensitive", nil, &m4a, "audio/mp4"},
		{"an unknown extension falls back", nil, &odd, "audio/ogg"},
		{"nothing known falls back", nil, nil, "audio/ogg"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := MediaType(c.codec, c.filename); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

// internal/transcribe CALLS this function rather than keeping a copy, which is
// the whole point of hoisting it: the media type the ASR service is told and
// the media type a browser is told cannot disagree.
//
// Asserted over the source rather than by behaviour, because a second copy
// would pass every behavioural test in both packages on the day it was made
// and then drift. Reading a sibling package's file is unusual and is the
// cheapest guard that can actually fail.
func TestTranscribeCallsThisFunctionRatherThanKeepingACopy(t *testing.T) {
	src, err := os.ReadFile(filepath.Join("..", "transcribe", "transcribe.go"))
	if err != nil {
		t.Fatalf("reading internal/transcribe: %v", err)
	}
	if bytes.Contains(src, []byte("func mediaTypeFor")) {
		t.Error("internal/transcribe still defines mediaTypeFor: two answers to one question, " +
			"which is the failure this repo names in four places")
	}
	if !bytes.Contains(src, []byte("audio.MediaType(")) {
		t.Error("internal/transcribe does not call audio.MediaType; the ASR submission and the " +
			"audio stream would be naming the same recording from two rules")
	}
}
