package mcp

import (
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestClampLimit(t *testing.T) {
	for _, tc := range []struct {
		name        string
		asked       int
		wantUsed    int
		wantClamped bool
		def, max    int
	}{
		{"not asked for", 0, 10, false, 10, 25},
		{"negative is replaced, and said so", -3, 10, true, 10, 25},
		{"inside the cap passes through", 7, 7, false, 10, 25},
		{"at the cap passes through", 25, 25, false, 10, 25},
		{"over the cap is clamped, not refused", 26, 25, true, 10, 25},
	} {
		t.Run(tc.name, func(t *testing.T) {
			used, clamped := ClampLimit(tc.asked, tc.def, tc.max)
			if used != tc.wantUsed || clamped != tc.wantClamped {
				t.Errorf("ClampLimit(%d, %d, %d) = %d, %v; want %d, %v",
					tc.asked, tc.def, tc.max, used, clamped, tc.wantUsed, tc.wantClamped)
			}
			if used <= 0 || used > tc.max {
				t.Errorf("used = %d: the API would refuse it", used)
			}
		})
	}
}

// The tool defaults never exceed the cap of the API family they are sent to.
func TestDefaultsFitTheirAPICaps(t *testing.T) {
	if DefaultSearchLimit > APISearchCap || DefaultInboxLimit > APITriageCap {
		t.Fatalf("defaults %d/%d exceed the API caps %d/%d",
			DefaultSearchLimit, DefaultInboxLimit, APISearchCap, APITriageCap)
	}
}

func TestTruncateCutsOnCharactersAndSaysSo(t *testing.T) {
	if got, cut := Truncate("abc", 3); got != "abc" || cut {
		t.Errorf("at the limit = %q, %v; want it whole", got, cut)
	}
	got, cut := Truncate("héllo wörld", 5)
	if got != "héllo" || !cut {
		t.Errorf("over the limit = %q, %v; want %q cut", got, cut, "héllo")
	}
	// A multi-byte character is never split.
	if got, _ := Truncate(strings.Repeat("日", 10), 4); got != "日日日日" {
		t.Errorf("cut mid-character: %q", got)
	}
}

func TestClassifyKeepsFourFactsApart(t *testing.T) {
	for status, want := range map[int]Availability{
		http.StatusOK:                 Available,
		http.StatusNotFound:           NotFound,
		http.StatusGone:               Withdrawn,
		http.StatusServiceUnavailable: Unconfigured,
	} {
		got, err := Classify("GET /x", status)
		if err != nil || got != want {
			t.Errorf("Classify(%d) = %q, %v; want %q", status, got, err, want)
		}
	}
	if _, err := Classify("GET /x", http.StatusUnauthorized); !errors.Is(err, ErrUnauthorized) {
		t.Errorf("401 = %v, want ErrUnauthorized", err)
	}
	if _, err := Classify("GET /x", http.StatusInternalServerError); err == nil || !strings.Contains(err.Error(), "GET /x") {
		t.Errorf("500 = %v, want an error naming the operation", err)
	}
}
