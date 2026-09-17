package audio

import (
	"path/filepath"
	"strings"
)

// MediaType names what a recording IS, from the two columns that describe it.
//
// ============================================================================
// ONE RULE, TWO CALLERS, AND THAT IS WHY IT LIVES HERE.
// ============================================================================
//
// It was `internal/transcribe.mediaTypeFor`, unexported, answering the
// Content-Type of the audio part in an ASR submission. CHRN-107 gave it a
// second caller — GET /audio/{memo_id} tells a browser the same thing — and
// two answers to one question is the failure this repo names in four places.
// The media type the ASR service is told and the media type a player is told
// cannot now disagree, which is the property CHRN-84 established for the
// accepted-container sets.
//
// IT TAKES TWO FIELDS RATHER THAN A store.Memo, and that is forced rather than
// stylistic: internal/store imports this package for audio.Ref
// (retention.go), so a signature naming store.Memo would be
// audio → store → audio, a cycle. These are the only two fields it ever read.
//
// The codec CHRN-21 read from the headers is preferred over the filename,
// because a filename is display-only in this system and is never used to
// derive anything. The extension is the fallback for a memo whose headers
// could not be read.
//
// THE FINAL FALLBACK IS A GUESS AND SHOULD BE READ AS ONE. CHRN-85 found every
// memo in the live corpus arriving m4a with a NULL codec, so the extension
// branch is the one that fires; audio/ogg is reached only by a memo with
// neither column, and it is the right guess because every recording either
// ingest path has produced is Opus in Ogg and the ASR service probes the
// content anyway. Sniffing the first twelve bytes would beat it — and would be
// a second answer to a question this function already answers.
func MediaType(codec, originalFilename *string) string {
	if codec != nil {
		switch strings.ToLower(*codec) {
		case "opus", "vorbis":
			return "audio/ogg"
		}
	}
	if originalFilename != nil {
		switch strings.ToLower(filepath.Ext(*originalFilename)) {
		case ".webm":
			return "audio/webm"
		case ".m4a", ".mp4", ".aac":
			return "audio/mp4"
		case ".mp3":
			return "audio/mpeg"
		case ".wav":
			return "audio/wav"
		case ".ogg", ".opus", ".oga":
			return "audio/ogg"
		}
	}
	return "audio/ogg"
}
