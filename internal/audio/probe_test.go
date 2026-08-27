package audio

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// CHRN-21's Done-when. The streams are built here rather than checked in as
// fixtures: a hand-built page is the only way to exercise a granule of -1, a
// truncated tail or a bad OpusHead version, and a binary fixture would make
// those cases unreadable in review. The real-file test at the bottom covers
// what a genuine encoder produces.

// oggPage assembles one page. granule of -1 means "no packet completes here",
// which is legal and is the case the backwards scan has to walk past.
func oggPage(t *testing.T, granule int64, seq uint32, payload []byte) []byte {
	t.Helper()
	// One lacing value per 255 bytes, plus a final short one.
	var segs []byte
	remaining := len(payload)
	for remaining >= 255 {
		segs = append(segs, 255)
		remaining -= 255
	}
	segs = append(segs, byte(remaining))
	if len(segs) > 255 {
		t.Fatalf("payload of %d bytes needs %d segments; the test helper only builds one page", len(payload), len(segs))
	}

	page := make([]byte, 0, oggPageHeader+len(segs)+len(payload))
	page = append(page, 'O', 'g', 'g', 'S')
	page = append(page, 0) // version
	page = append(page, 0) // header type
	page = binary.LittleEndian.AppendUint64(page, uint64(granule))
	page = binary.LittleEndian.AppendUint32(page, 1) // serial
	page = binary.LittleEndian.AppendUint32(page, seq)
	page = binary.LittleEndian.AppendUint32(page, 0) // CRC, not checked
	page = append(page, byte(len(segs)))
	page = append(page, segs...)
	return append(page, payload...)
}

// opusHeadPacket is the 19-byte identification header.
func opusHeadPacket(channels byte, preSkip uint16, inputRate uint32) []byte {
	p := make([]byte, 0, 19)
	p = append(p, opusHead...)
	p = append(p, 1)        // version 1: major nibble 0
	p = append(p, channels) // channel count
	p = binary.LittleEndian.AppendUint16(p, preSkip)
	p = binary.LittleEndian.AppendUint32(p, inputRate)
	p = binary.LittleEndian.AppendUint16(p, 0) // output gain
	return append(p, 0)                        // mapping family
}

// stream is a minimal but well-formed Ogg Opus file.
func stream(t *testing.T, channels byte, preSkip uint16, inputRate uint32, finalGranule int64) []byte {
	t.Helper()
	b := oggPage(t, 0, 0, opusHeadPacket(channels, preSkip, inputRate))
	b = append(b, oggPage(t, 0, 1, []byte("OpusTags\x00\x00\x00\x00\x00\x00\x00\x00"))...)
	b = append(b, oggPage(t, finalGranule, 2, make([]byte, 64))...)
	return b
}

func writeStream(t *testing.T, name string, b []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, b, 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

// ---------------------------------------------------------------- duration

// "duration_ms matches the source within one 20 ms frame" — the error is zero,
// because the arithmetic is exact rather than estimated.
func TestProbeDurationIsExact(t *testing.T) {
	// preSkip 312 is what libopus writes at 48 kHz, and is the 6.5 ms that
	// ffprobe leaves in its answer.
	const preSkip = 312

	for _, tc := range []struct {
		name   string
		wantMS int32
	}{
		{"quarter second", 250},
		{"one second", 1000},
		{"37.5 seconds", 37500},
		{"forty minutes", 40 * 60 * 1000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			granule := int64(tc.wantMS)*opusGranuleRate/1000 + preSkip
			p := writeStream(t, "m.opus", stream(t, 1, preSkip, 48000, granule))

			info, err := Probe(p)
			if err != nil {
				t.Fatalf("Probe: %v", err)
			}
			if info.DurationMS != tc.wantMS {
				t.Fatalf("duration %d ms, want %d — error of %d ms",
					info.DurationMS, tc.wantMS, info.DurationMS-tc.wantMS)
			}
		})
	}
}

// The pre-skip is the whole difference between this and the obvious tool. If a
// change ever drops the subtraction, every duration is 6.5 ms long and no test
// that only checks "roughly right" would notice.
func TestProbeSubtractsThePreSkip(t *testing.T) {
	const preSkip = 312
	granule := int64(48000) + preSkip // exactly one second of audio

	p := writeStream(t, "m.opus", stream(t, 1, preSkip, 48000, granule))
	info, err := Probe(p)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if info.DurationMS != 1000 {
		t.Fatalf("duration %d ms, want 1000; %d ms is what you get by dividing the granule "+
			"without subtracting the pre-skip", info.DurationMS, 1006)
	}

	// A different pre-skip must move the answer, or the field is being ignored
	// rather than used.
	p2 := writeStream(t, "m2.opus", stream(t, 1, 48000, 48000, granule))
	info2, err := Probe(p2)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if info2.DurationMS != 7 {
		t.Fatalf("with a one-second pre-skip the duration is %d ms, want 7", info2.DurationMS)
	}
}

// ---------------------------------------------------------------- the header

func TestProbeReadsTheIdentificationHeader(t *testing.T) {
	p := writeStream(t, "m.opus", stream(t, 2, 312, 44100, 48312))
	info, err := Probe(p)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if info.Codec != CodecOpus {
		t.Fatalf("codec %q, want %q", info.Codec, CodecOpus)
	}
	if info.SampleRateHz != 44100 {
		t.Fatalf("sample rate %d, want 44100 — the value OpusHead carries", info.SampleRateHz)
	}
	if info.Channels != 2 {
		t.Fatalf("channels %d, want 2", info.Channels)
	}
}

// An absent input rate is zero, which the caller stores as NULL rather than as
// a confident wrong number.
func TestProbeReportsAnAbsentSampleRateAsZero(t *testing.T) {
	p := writeStream(t, "m.opus", stream(t, 1, 312, 0, 48312))
	info, err := Probe(p)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if info.SampleRateHz != 0 {
		t.Fatalf("sample rate %d, want 0", info.SampleRateHz)
	}
}

// ---------------------------------------------------------------- the tail

// A granule of -1 is legal — it means no packet completes on that page — so the
// backwards scan must walk past it rather than stopping at the last "OggS".
func TestProbeWalksPastPagesWithNoGranule(t *testing.T) {
	const preSkip = 312
	b := stream(t, 1, preSkip, 48000, 48000+preSkip)
	b = append(b, oggPage(t, -1, 3, make([]byte, 32))...)
	b = append(b, oggPage(t, -1, 4, make([]byte, 32))...)

	info, err := Probe(writeStream(t, "m.opus", b))
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if info.DurationMS != 1000 {
		t.Fatalf("duration %d ms, want 1000 — a -1 granule was read as the answer", info.DurationMS)
	}
}

// The tail is scanned rather than the whole file, so a long stream must still
// find its last page. Built past the tail window to prove the scan is anchored
// at the end and not at the start.
func TestProbeFindsTheLastPageOfALongStream(t *testing.T) {
	const preSkip = 312
	b := oggPage(t, 0, 0, opusHeadPacket(1, preSkip, 48000))
	b = append(b, oggPage(t, 0, 1, []byte("OpusTags\x00\x00\x00\x00\x00\x00\x00\x00"))...)
	for seq := uint32(2); len(b) < headWindow+tailWindow; seq++ {
		b = append(b, oggPage(t, int64(seq)*960, seq, make([]byte, 4000))...)
	}
	final := int64(60)*60*opusGranuleRate + preSkip // one hour
	b = append(b, oggPage(t, final, 9999, make([]byte, 64))...)

	info, err := Probe(writeStream(t, "long.opus", b))
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if info.DurationMS != 60*60*1000 {
		t.Fatalf("duration %d ms, want %d", info.DurationMS, 60*60*1000)
	}
}

// ---------------------------------------------------------------- refusals

// "A corrupt or non-Opus file fails loudly." Loudly means an error the caller
// can log — never silence, and never a plausible number.
func TestProbeRefusesWhatItCannotRead(t *testing.T) {
	const preSkip = 312
	good := stream(t, 1, preSkip, 48000, 48000+preSkip)

	cases := map[string]struct {
		body    []byte
		notOpus bool // ErrNotOpus rather than a corruption error
	}{
		"empty file":       {body: []byte{}, notOpus: true},
		"not ogg at all":   {body: []byte("ID3\x04\x00\x00\x00\x00\x00\x00 this is an mp3"), notOpus: true},
		"ogg but not opus": {body: oggPage(t, 0, 0, []byte("\x01vorbis00000000000000000")), notOpus: true},
		"ogg later in the file": {
			// Junk in front of a valid stream. Accepting this would mean
			// scanning for a header rather than requiring one.
			body:    append([]byte("RIFF....WAVEfmt "), good...),
			notOpus: true,
		},
		"future OpusHead version": {
			body:    oggPage(t, 0, 0, append([]byte("OpusHead"), 0x10, 1, 0x38, 1, 0x80, 0xbb, 0, 0, 0, 0, 0)),
			notOpus: true,
		},
		"zero channels": {
			body:    oggPage(t, 0, 0, opusHeadPacket(0, preSkip, 48000)),
			notOpus: true,
		},
		"header only, no audio pages": {
			// A valid OpusHead and nothing after it: granule 0, so after the
			// pre-skip there is no audio. 0003's CHECK is duration_ms > 0.
			body: oggPage(t, 0, 0, opusHeadPacket(1, 0, 48000)),
		},
		"granule below the pre-skip": {
			body: stream(t, 1, 48000, 48000, 100),
		},
		"truncated tail": {
			// Header intact, audio pages replaced by junk.
			body: append(oggPage(t, 0, 0, opusHeadPacket(1, preSkip, 48000)),
				make([]byte, tailWindow+1024)...),
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Probe(writeStream(t, "m.opus", tc.body))
			if err == nil {
				t.Fatal("no error; a file this probe cannot describe must say so")
			}
			if tc.notOpus && !errors.Is(err, ErrNotOpus) {
				t.Fatalf("want ErrNotOpus so the caller can tell a wrong format from a damaged one, got %v", err)
			}
			if !tc.notOpus && errors.Is(err, ErrNotOpus) {
				t.Fatalf("reported as not-Opus, but this IS an Opus stream that is damaged: %v", err)
			}
		})
	}
}

// A missing file is an error rather than a zero Info, so a caller cannot
// mistake "there is nothing there" for "it has no duration".
func TestProbeOnAMissingFileErrors(t *testing.T) {
	if _, err := Probe(filepath.Join(t.TempDir(), "nope.opus")); err == nil {
		t.Fatal("probing a missing file returned no error")
	}
}

// ---------------------------------------------------------------- real files

// Run against files a real encoder produced. Skips without the directory,
// like the database suites skip without a DSN — CI has no Opus corpus, and a
// checked-in one would be a binary blob nobody can review.
//
//	CHRONICLE_TEST_OPUS_DIR=/path/with/*.opus go test ./internal/audio/
//
// Filenames of the form `<seconds>s` are checked against their own name, so a
// fixture set states its own expectations: gen_37.5s_48000hz_1ch.opus must
// probe to 37500 ms.
func TestProbeAgainstRealOpusFiles(t *testing.T) {
	dir := os.Getenv("CHRONICLE_TEST_OPUS_DIR")
	if dir == "" {
		t.Skip("CHRONICLE_TEST_OPUS_DIR not set; skipping the real-encoder check")
	}
	names, err := filepath.Glob(filepath.Join(dir, "*.opus"))
	if err != nil || len(names) == 0 {
		t.Fatalf("no .opus files in %s (err %v)", dir, err)
	}

	for _, name := range names {
		t.Run(filepath.Base(name), func(t *testing.T) {
			info, err := Probe(name)
			if err != nil {
				t.Fatalf("Probe: %v", err)
			}
			if info.Codec != CodecOpus || info.DurationMS <= 0 || info.Channels < 1 {
				t.Fatalf("implausible: %+v", info)
			}
			want, ok := secondsFromName(filepath.Base(name))
			if !ok {
				t.Logf("%d ms, %d Hz, %dch", info.DurationMS, info.SampleRateHz, info.Channels)
				return
			}
			// One 20 ms Opus frame, which is the ticket's tolerance. The
			// measured error on the fixture set is zero.
			if d := info.DurationMS - want; d > 20 || d < -20 {
				t.Fatalf("duration %d ms, name says %d ms, off by %d", info.DurationMS, want, d)
			}
		})
	}
}

// secondsFromName reads "gen_37.5s_..." as 37500 ms.
func secondsFromName(base string) (int32, bool) {
	for _, part := range strings.Split(base, "_") {
		if !strings.HasSuffix(part, "s") {
			continue
		}
		var whole, frac int32
		var scale int32 = 1
		body := strings.TrimSuffix(part, "s")
		dot := strings.IndexByte(body, '.')
		if dot >= 0 {
			for _, c := range body[dot+1:] {
				if c < '0' || c > '9' {
					return 0, false
				}
				frac = frac*10 + (c - '0')
				scale *= 10
			}
			body = body[:dot]
		}
		if body == "" {
			return 0, false
		}
		for _, c := range body {
			if c < '0' || c > '9' {
				return 0, false
			}
			whole = whole*10 + (c - '0')
		}
		return whole*1000 + frac*1000/scale, true
	}
	return 0, false
}
