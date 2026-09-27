package store

import (
	"math"
	"testing"
)

func i32(v int32) *int32 { return &v }
func i64(v int64) *int64 { return &v }

// CHRN-85 criterion 1: the rule exists once, and this is its whole behaviour.
// The rows are the shapes the prod corpus actually has (2026-09-26) plus the
// edges that would each be a silent wrong answer.
func TestResolveDuration(t *testing.T) {
	for _, c := range []struct {
		name       string
		header     *int32
		transcript *int64
		wantMS     int64 // 0 with wantSrc == "" means nil
		wantSrc    DurationSource
	}{
		{"header only: an Ogg Opus memo not yet transcribed", i32(104000), nil, 104000, DurationFromHeader},
		{"transcript only: the m4a eval corpus", nil, i64(34773), 34773, DurationFromTranscript},
		// The measured prod shape: header minus transcript is exactly -19 ms on
		// every memo that has both. Header first, so the answer does not move
		// by 19 ms when the transcript lands.
		{"both, 19 ms apart: the header", i32(104000), i64(104019), 104000, DurationFromHeader},
		// A file that is not the length its header says. Still the header --
		// nothing reconciles them; internal/transcribe is what says so out loud.
		{"both, more than a second apart: still the header", i32(60000), i64(62500), 60000, DurationFromHeader},
		{"neither: nil, and not a zero", nil, nil, 0, ""},

		// Non-positive is absent, on both sides.
		{"a zero transcript is absent, not 0:00", nil, i64(0), 0, ""},
		{"a negative transcript is absent", nil, i64(-5), 0, ""},
		{"a zero transcript does not displace a real header", i32(3000), i64(0), 3000, DurationFromHeader},
		{"a non-positive header falls through to the transcript", i32(0), i64(34773), 34773, DurationFromTranscript},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := ResolveDuration(c.header, c.transcript)
			if c.wantSrc == "" {
				if got != nil {
					t.Fatalf("ResolveDuration = %+v, want nil", *got)
				}
				return
			}
			if got == nil || got.MS != c.wantMS || got.Source != c.wantSrc {
				t.Fatalf("ResolveDuration = %+v, want %d from %q", got, c.wantMS, c.wantSrc)
			}
		})
	}
}

// The int32 payloads (BatchItem, DeferredItem) narrow explicitly. A value past
// int32 is "no duration a screen can show" and never a wrapped number that
// looks like a length.
func TestInt32MSNarrowsExplicitly(t *testing.T) {
	if got := (*Duration)(nil).Int32MS(); got != nil {
		t.Errorf("nil Duration narrowed to %d, want nil", *got)
	}
	if got := (&Duration{MS: 34773}).Int32MS(); got == nil || *got != 34773 {
		t.Errorf("34773 narrowed to %v", got)
	}
	if got := (&Duration{MS: math.MaxInt32}).Int32MS(); got == nil || *got != math.MaxInt32 {
		t.Errorf("MaxInt32 narrowed to %v, want it kept", got)
	}
	// 24.8 days of audio and a bit. int32(d.MS) would wrap this to a NEGATIVE
	// number, or worse to a small positive one, and either reads as a length.
	if got := (&Duration{MS: math.MaxInt32 + 1}).Int32MS(); got != nil {
		t.Errorf("MaxInt32+1 narrowed to %d, want nil rather than a wrapped or clamped length", *got)
	}
	if got := (&Duration{MS: 1 << 40}).Int32MS(); got != nil {
		t.Errorf("1<<40 narrowed to %d, want nil", *got)
	}
}
