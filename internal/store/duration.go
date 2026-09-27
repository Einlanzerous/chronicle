package store

import "math"

// DurationSource says which column answered a resolved duration. The strings
// are the wire's `MemoProvenance.duration_source` enum, and internal/api pins
// the two together with a test rather than a cast.
type DurationSource string

const (
	// DurationFromHeader is `tier2.memos.duration_ms`: Ogg granule arithmetic,
	// exact, read at ingest before anything has decoded the file, and populated
	// for Ogg Opus only (CHRN-21).
	DurationFromHeader DurationSource = "memo_header"

	// DurationFromTranscript is `tier2.transcripts.audio_duration_ms`: measured
	// by the ASR service off the 16 kHz mono WAV AFTER ffmpeg normalisation, and
	// populated for everything that transcribes.
	DurationFromTranscript DurationSource = "transcript"
)

// Duration is how long a recording is, and which column said so.
type Duration struct {
	MS     int64
	Source DurationSource
}

// ResolveDuration is the ONE rule for "how long is this recording" (CHRN-85).
// Every surface that shows a duration calls it -- MemoProvenance, the triage
// batch, the deferred list and the hold response -- so no reader chooses a
// column for itself and a fifth one (CHRN-69's metrics) has nothing to invent.
//
// The header when there is one, otherwise the transcript's measurement,
// otherwise nil. Nil, never a zero: a zero renders as `0:00`, which is a
// claim, where nil is "nothing has measured this yet".
//
// HEADER FIRST, on purpose. It exists from ingest and the transcript arrives
// later, so a memo's displayed length is stable from the moment the memo exists
// and does not shift when it is transcribed. Measured on prod (2026-09-26) the
// two disagree by a constant 19 ms on every memo that has both -- one Opus
// frame, below any display's resolution -- so preferring the exact one costs
// nothing and the disagreement is not reconciled, only logged when it stops
// being that (internal/transcribe).
//
// NOTHING IS COPIED between the tables. The transcript's value is read at the
// moment it is shown and never written onto the memo: that would be a second
// copy of one measurement in tier 2, and it would look like the probe had
// described a memo it never read (upload.go and the watcher retry the probe
// only while `duration_ms IS NULL`).
//
// A non-positive value counts as absent on both sides. The header's own CHECK
// is `duration_ms > 0`, so for the header that is the database's rule stated
// again; the transcript has no such CHECK, and a silent file's `0` must not
// render as a length.
//
// WHICH TRANSCRIPT ROW is the caller's, and it is the SAME row on every
// surface: the memo's best transcript, a complete one else the latest partial,
// from any model (GetTranscript's choice, written once as bestTranscriptOrder).
// It is not the durable row the triage excerpt comes from. Durability decides
// whether a transcript may license deleting audio; a recording's length is a
// fact about the audio, so a memo whose only transcript is partial, or from a
// model outside the durable set, still has one. An earlier draft read the lists'
// duration off the durable row and a review found the cost: a held memo in that
// state showed a length in the hold response and none in the deferred list.
func ResolveDuration(header *int32, transcript *int64) *Duration {
	if header != nil && *header > 0 {
		return &Duration{MS: int64(*header), Source: DurationFromHeader}
	}
	if transcript != nil && *transcript > 0 {
		return &Duration{MS: *transcript, Source: DurationFromTranscript}
	}
	return nil
}

// Int32MS narrows a resolved duration for the payloads whose wire type is
// `int32` (`BatchItem`, `DeferredItem`), where MemoProvenance's is `int64` and
// the transcript column is `bigint`. Nil for nil.
//
// EXPLICIT rather than an `int32(d.MS)`, whose failure mode is a wrapped, wrong
// and plausible-looking number. int32 milliseconds is 24.8 days of audio, which
// no voice memo is; a value past it comes back nil -- "no duration a screen can
// show" -- and never a clamp that would state a length nobody measured.
func (d *Duration) Int32MS() *int32 {
	if d == nil || d.MS > math.MaxInt32 {
		return nil
	}
	ms := int32(d.MS)
	return &ms
}
