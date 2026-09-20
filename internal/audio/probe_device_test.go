package audio

import (
	"testing"
)

// The one test in this package whose fixtures are BYTES RATHER THAN CODE, and
// the reason is the only reason worth introducing a testdata directory for.
//
// Every other fixture here is assembled by `oggPage` and friends, which is the
// right default: it makes the shape under test explicit and keeps the failure
// message close to the arithmetic. But a hand-built stream can only ever
// contain what we already believed about Ogg. These two files were written by
// an Android phone (CHRN-60, a Pixel 9 Pro on Android 17) through
// `MediaRecorder`'s Ogg/Opus writer, so they carry that writer's real page
// geometry — 450 pages for nine seconds, a page per 20 ms frame, lacing and
// granules as libopus actually emits them.
//
// # What the pair is for
//
// `chrn60_torn.opus` is a device recording truncated **mid-body** of a page,
// which is the shape an interrupted write leaves behind. `chrn60_trimmed.opus`
// is what the CLIENT'S trim produced from it — Dart code in
// `mobile/chronicle/lib/capture/ogg.dart`, not this package.
//
// So the two files pin two independent implementations of the same page
// arithmetic to each other **by data**. The Dart side asserts that trimming the
// torn file reproduces the trimmed file byte for byte; this side asserts that
// what the Dart side produced is something `Probe` can actually describe. A
// change to either implementation breaks a test on its own side, which is what
// makes this worth the bytes. Agreed on CHRN-60's plan as a deliberate
// cross-epic addition rather than something slipped in.
//
// # Why the audio is a tone
//
// This repository is public. The phone played a generated 440/660/880 Hz tone
// through its own speaker into its own microphone, so the fixture is genuinely
// device-written and is unambiguously nobody's voice and nobody's room.

const (
	tornFixture    = "testdata/chrn60_torn.opus"
	trimmedFixture = "testdata/chrn60_trimmed.opus"

	// What the client's trim reported for the trimmed file, from its own
	// independent arithmetic. Pinned here so a drift on either side is a test
	// failure rather than a quiet disagreement.
	fixtureDurationMS = 4514
	fixturePreSkip    = 312
)

func TestProbeDescribesWhatTheClientsTrimProduced(t *testing.T) {
	info, err := Probe(trimmedFixture)
	if err != nil {
		t.Fatalf("Probe(%s) = %v; the trim is supposed to leave a stream this package can read", trimmedFixture, err)
	}
	if info.Codec != CodecOpus {
		t.Errorf("Codec = %q, want %q", info.Codec, CodecOpus)
	}
	if info.DurationMS != fixtureDurationMS {
		t.Errorf("DurationMS = %d, want %d — this package and the client's trim disagree about the same bytes",
			info.DurationMS, fixtureDurationMS)
	}
	// CHRN-21 measured that OpusHead's input rate is whatever the encoder was
	// fed, and every libopus file reads 48000 whatever the source was. A device
	// recording is the honest check on that claim.
	if info.SampleRateHz != 48000 {
		t.Errorf("SampleRateHz = %d, want 48000", info.SampleRateHz)
	}
}

// The half that shows the trim is load-bearing rather than tidy.
//
// Handed the untrimmed file, Probe declines: the last complete page does not
// end at EOF, so no single granule describes it and it reports a chain or
// appended data. A client that shipped the torn bytes would produce a memo with
// a NULL duration, codec and sample rate — for want of arithmetic that runs in
// twenty lines on the phone.
func TestProbeRefusesTheTornFileTheTrimExistsFor(t *testing.T) {
	if _, err := Probe(tornFixture); err == nil {
		t.Fatal("Probe accepted the torn file; if this ever passes, the trim is no longer buying anything and CHRN-60's salvage needs rereading")
	}
}
