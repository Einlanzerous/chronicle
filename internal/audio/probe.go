package audio

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
)

// CHRN-21 — what a recording is, read from its headers and nothing else.
//
// # Why there is no decoder here
//
// The ticket was originally "Opus to 16 kHz mono WAV, plus duration and codec
// metadata". The decode half moved to CHRN-3 by decision on 2026-08-27, so
// Chronicle carries no decoder at all: whisper.cpp needs 16 kHz WAV, ffmpeg is
// ~100 MB of non-Go dependency against a CLAUDE.md that opens with "Single
// static Go binary", and E3's ASR service is already the shared runner with
// Catenary as its second non-WAV client. Full reasoning in
// docs/decisions/chrn-21-metadata-without-a-decoder.md.
//
// What is left needs no decoder either, and is *more* accurate than the obvious
// tool. Opus's granule positions count samples at a fixed 48 kHz regardless of
// what the source was recorded at, so the duration is arithmetic over two
// numbers that are both in the file: the final page's granule position, and the
// pre-skip in OpusHead. On a 37.5 s file that gives 37500 ms exactly, where
// `ffprobe` reports 37506.5 ms — it divides the granule by 48000 without
// subtracting the 312-sample pre-skip.
//
// # It reads two small windows, not the file
//
// The header is at the start and the granule is at the end, so a 40-minute memo
// costs one read at each end rather than a pass over the whole thing. That
// matters because this runs on the ingest path, right after the bytes were
// already written once.

// Opus decodes at 48 kHz whatever the source rate was, and granule positions
// are counted in those samples. This is a property of the codec, not a choice.
const opusGranuleRate = 48000

// oggPageHeader is the fixed part of an Ogg page header, before the segment
// table: "OggS", version, type, granule(8), serial(4), sequence(4), CRC(4),
// and the segment count.
const oggPageHeader = 27

// headWindow is how much of the file is read looking for the first page.
// OpusHead is required by RFC 7845 to be alone in the first page of the
// logical stream, so this only has to be big enough for one page.
const headWindow = 64 << 10

// tailWindow is how much of the end is scanned backwards for the last page
// that carries a granule. Pages are at most ~64 KB (255 segments x 255 bytes),
// so this covers several even in the worst case.
const tailWindow = 256 << 10

// oggS is the page capture pattern.
var oggS = []byte{'O', 'g', 'g', 'S'}

// opusHead is the identification header's magic signature.
var opusHead = []byte("OpusHead")

// ErrNotOpus means the file is not Ogg-encapsulated Opus. It is deliberately
// distinguishable from a corrupt one: "somebody uploaded an m4a" and "this Opus
// file is damaged" are different facts, and only the second is alarming.
var ErrNotOpus = errors.New("audio: not an Ogg Opus stream")

// Info is what the headers say about a recording. The fields line up with the
// three nullable columns 0003 created for them.
type Info struct {
	// Codec is "opus". A constant today, and a field rather than an assumption
	// because tier2.memos.codec exists to record what was actually found — a
	// column that can only ever hold one value is a column nobody checks.
	Codec string

	// DurationMS is the playable length: the final granule less the pre-skip,
	// at 48 kHz. Exact, not estimated.
	DurationMS int32

	// SampleRateHz is OpusHead's input sample rate, and it means less than the
	// name suggests. **Do not read it as "what this was recorded at."**
	//
	// RFC 7845 §5.1 intends it as the original source rate, and it is
	// explicitly *not* the playback rate — Opus always decodes at 48 kHz. But
	// what an encoder writes is the rate it was fed, and encoders that resample
	// first record the rate *after* resampling. Measured on CHRN-79's fixture
	// set: ffmpeg's libopus writes **48000 for every file**, including sources
	// generated at 8 kHz, 16 kHz and 44.1 kHz.
	//
	// So a corpus of 48000s is the expected reading, not a bug, and this is
	// recorded because it is what the file says rather than because it is
	// informative. Zero when the field is absent, and the caller stores NULL.
	SampleRateHz int32

	// Channels is 1 or 2 for the mappings a phone produces.
	Channels int32
}

// Probe reads a recording's headers and reports what it is.
//
// It never rewrites, never decodes, and holds the file open read-only. A
// failure is a failure to *describe* the audio and must not be treated as a
// reason to reject it — see the callers: a recording Chronicle cannot parse is
// still somebody's recording, and the three columns stay NULL.
func Probe(path string) (Info, error) {
	f, err := os.Open(path)
	if err != nil {
		return Info{}, fmt.Errorf("audio: probe: %w", err)
	}
	defer func() { _ = f.Close() }()

	size, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		return Info{}, fmt.Errorf("audio: probe: %w", err)
	}
	if size == 0 {
		return Info{}, fmt.Errorf("%w: file is empty", ErrNotOpus)
	}

	head, err := readAt(f, 0, min64(size, headWindow))
	if err != nil {
		return Info{}, err
	}
	info, preSkip, err := parseOpusHead(head)
	if err != nil {
		return Info{}, err
	}

	granule, err := lastGranule(f, size)
	if err != nil {
		return Info{}, err
	}

	// The pre-skip is encoder delay: samples the decoder produces before the
	// first real one, which are not part of what anybody recorded. Subtracting
	// it is the difference between this and ffprobe.
	if granule < int64(preSkip) {
		return Info{}, fmt.Errorf("audio: probe: final granule %d is below the %d-sample pre-skip, so the stream is truncated or damaged",
			granule, preSkip)
	}
	samples := granule - int64(preSkip)

	// Rounded rather than truncated: at 48 kHz a truncation loses up to 20 µs
	// per file, which is nothing, but "duration matches the source within a
	// frame" is the Done-when and rounding is what makes the error zero rather
	// than merely small.
	ms := (samples*1000 + opusGranuleRate/2) / opusGranuleRate
	if ms <= 0 {
		// 0003's CHECK is `duration_ms > 0`, so a zero-length stream has no
		// representable duration. Refused here rather than left to violate a
		// constraint two layers down.
		return Info{}, fmt.Errorf("audio: probe: stream contains no audio (%d samples after pre-skip)", samples)
	}
	if ms > int64(^uint32(0)>>1) {
		return Info{}, fmt.Errorf("audio: probe: duration %d ms does not fit the column", ms)
	}
	info.DurationMS = int32(ms)
	return info, nil
}

// parseOpusHead finds the first Ogg page and reads the identification header
// out of it, returning the pre-skip separately because it is arithmetic rather
// than something to record.
func parseOpusHead(buf []byte) (Info, uint16, error) {
	if len(buf) < oggPageHeader || string(buf[:4]) != string(oggS) {
		// Checked at offset zero rather than searched for. A file whose first
		// bytes are not "OggS" is not an Ogg stream, and scanning for one
		// deeper in would accept a file with arbitrary junk in front of it.
		return Info{}, 0, fmt.Errorf("%w: no OggS at the start of the file", ErrNotOpus)
	}
	segments := int(buf[26])
	body := oggPageHeader + segments
	if body > len(buf) {
		return Info{}, 0, fmt.Errorf("%w: first page header is truncated", ErrNotOpus)
	}

	packet := buf[body:]
	// RFC 7845 §5.1: the identification header is 19 bytes, and must be the
	// only packet in the first page.
	if len(packet) < 19 || string(packet[:8]) != string(opusHead) {
		return Info{}, 0, fmt.Errorf("%w: first packet is not OpusHead", ErrNotOpus)
	}
	if version := packet[8]; version>>4 != 0 {
		// Major version 0 is the only one defined. A future major version may
		// move these fields, so reading them anyway would be inventing data.
		return Info{}, 0, fmt.Errorf("%w: OpusHead version %d is not one this understands", ErrNotOpus, version)
	}

	channels := packet[9]
	preSkip := binary.LittleEndian.Uint16(packet[10:12])
	inputRate := binary.LittleEndian.Uint32(packet[12:16])
	if channels == 0 {
		return Info{}, 0, fmt.Errorf("%w: OpusHead declares zero channels", ErrNotOpus)
	}
	if inputRate > uint32(^uint32(0)>>1) {
		// Beyond what an INTEGER column holds. Recorded as unknown rather than
		// wrapped into a negative, which would be worse than absent.
		inputRate = 0
	}

	return Info{
		Codec:        CodecOpus,
		SampleRateHz: int32(inputRate),
		Channels:     int32(channels),
	}, preSkip, nil
}

// CodecOpus is the value recorded in tier2.memos.codec.
const CodecOpus = "opus"

// lastGranule finds the granule position of the last page that carries one.
//
// Scanned backwards from the end because that is where the answer is, and
// because reading forward would mean walking every page of a forty-minute
// recording to learn one number. A page whose granule is -1 carries no
// completed packet, which is legal, so the scan continues past those rather
// than treating the first hit as the answer.
func lastGranule(f *os.File, size int64) (int64, error) {
	start := size - tailWindow
	if start < 0 {
		start = 0
	}
	buf, err := readAt(f, start, size-start)
	if err != nil {
		return 0, err
	}

	for i := len(buf) - oggPageHeader; i >= 0; i-- {
		if string(buf[i:i+4]) != string(oggS) {
			continue
		}
		granule := int64(binary.LittleEndian.Uint64(buf[i+6 : i+14]))
		if granule == -1 {
			// No packet completes on this page. Legal; keep going back.
			continue
		}
		if granule < 0 {
			return 0, fmt.Errorf("audio: probe: page at offset %d carries a negative granule (%d), so the stream is damaged",
				start+int64(i), granule)
		}
		return granule, nil
	}
	// A window this size holds several pages even at the maximum page length,
	// so finding none means the tail is not Ogg — a truncated or overwritten
	// file, which is exactly the case this is supposed to catch loudly.
	return 0, fmt.Errorf("audio: probe: no Ogg page with a granule in the last %d bytes; the stream is truncated or damaged",
		len(buf))
}

// readAt reads exactly n bytes at off.
func readAt(f *os.File, off, n int64) ([]byte, error) {
	buf := make([]byte, n)
	if _, err := f.ReadAt(buf, off); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("audio: probe: reading %d bytes at %d: %w", n, off, err)
	}
	return buf, nil
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
