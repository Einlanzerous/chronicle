package asr

import (
	"strings"
	"testing"
	"time"
)

// ASR_CLIENT_TOKENS is the whole of the auth surface, so its parser is where a
// mistake becomes an open transcription service.

func TestClientTokensRequireAtLeastOne(t *testing.T) {
	for _, raw := range []string{"", "   ", "\n"} {
		_, err := parseClientTokens(raw)
		if err == nil {
			t.Fatalf("%q was accepted; an empty token set must be a boot error, never \"open\"", raw)
		}
		if !strings.Contains(err.Error(), "ASR_CLIENT_TOKENS") {
			t.Fatalf("the error does not name the variable: %v", err)
		}
	}
}

func TestClientTokensParsePairs(t *testing.T) {
	const a = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const b = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	got, err := parseClientTokens("chronicle:" + a + ", catenary:" + b)
	if err != nil {
		t.Fatal(err)
	}
	if got[a] != "chronicle" || got[b] != "catenary" {
		t.Fatalf("parsed %v", redactValues(got))
	}
}

// A short token is somebody's placeholder. Refused at boot rather than left to
// be discovered by whoever guesses it.
func TestShortTokensAreRefused(t *testing.T) {
	if _, err := parseClientTokens("chronicle:hunter2"); err == nil {
		t.Fatal("a seven-character token was accepted")
	}
}

// Two clients sharing one token means client_id is not actually derived from
// the credential, which is the thing §6 turns on.
func TestSharedTokensAreRefused(t *testing.T) {
	const same = "cccccccccccccccccccccccccccccccccc"
	if _, err := parseClientTokens("chronicle:" + same + " catenary:" + same); err == nil {
		t.Fatal("two clients were allowed to share one token")
	}
}

func TestMalformedEntriesAreRefused(t *testing.T) {
	for _, raw := range []string{"nocolon", ":tokenonlyaaaaaaaaaaaaaaaaaaaaaaaaaa", "name:"} {
		if _, err := parseClientTokens(raw); err == nil {
			t.Fatalf("%q was accepted", raw)
		}
	}
}

// redactValues renders the map for a failure message WITHOUT the tokens. A test
// that prints a credential on failure is a test that prints a credential into
// CI logs on the day it fails.
func redactValues(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for _, name := range m {
		out = append(out, name)
	}
	return out
}

// --- CHRN-26's knobs -------------------------------------------------------

// The rates are per DEVICE, and an unset variable means "this deployment is the
// R9700 CHRN-24 measured". A worker somewhere else sets its own, because a
// deadline computed from somebody else's GPU is either a false kill or no bound
// at all.
func TestExpectedRatesDefaultToTheMeasuredDevice(t *testing.T) {
	got, err := parseExpectedRates("")
	if err != nil {
		t.Fatal(err)
	}
	if got["small.en"] != 57.9 {
		t.Fatalf("small.en defaults to %v; CHRN-24's resident column says 57.9", got["small.en"])
	}
	// A copy, not the package variable: a caller that edited it would move the
	// default for the next process to read it.
	got["small.en"] = 1
	if DefaultExpectedRates["small.en"] != 57.9 {
		t.Fatal("parseExpectedRates handed out the package's own map")
	}
}

func TestExpectedRatesParsePairs(t *testing.T) {
	got, err := parseExpectedRates("small.en=40.1, large-v3=11.2")
	if err != nil {
		t.Fatal(err)
	}
	if got["small.en"] != 40.1 || got["large-v3"] != 11.2 {
		t.Fatalf("parsed %v", got)
	}
	// A model with no rate is NOT an error: it uses the slowest CHRN-24
	// measured, so an unknown model errs wide rather than killing a healthy job.
	if _, named := got["medium.en"]; named {
		t.Fatal("a model nobody configured acquired a rate")
	}
}

func TestExpectedRatesRefuseNonsense(t *testing.T) {
	for _, raw := range []string{"small.en", "small.en=", "small.en=fast", "small.en=0", "small.en=-2"} {
		if _, err := parseExpectedRates(raw); err == nil {
			t.Fatalf("%q was accepted; a zero or missing rate makes every deadline infinite or instant", raw)
		}
	}
}

// A deadline factor below 1 is a deadline shorter than the job itself, which
// kills healthy work on every run. Refused at boot rather than discovered as a
// queue that never finishes anything.
func TestDeadlineFactorBelowOneIsRefused(t *testing.T) {
	t.Setenv("ASR_DATABASE_URL", "postgres://x/y")
	t.Setenv("ASR_CLIENT_TOKENS", "chronicle:"+strings.Repeat("a", 34))
	t.Setenv("ASR_INFERENCE_DEADLINE_FACTOR", "0.5")

	if _, err := Load(); err == nil {
		t.Fatal("a deadline factor of 0.5 booted")
	} else if !strings.Contains(err.Error(), "ASR_INFERENCE_DEADLINE_FACTOR") {
		t.Fatalf("the error does not name the variable: %v", err)
	}
}

// The resident child listens on LOOPBACK and has no authentication of any kind.
// A malformed address is a boot error rather than a supervisor that cannot
// parse its own child's address later.
func TestWhisperServerAddrMustBeHostPort(t *testing.T) {
	t.Setenv("ASR_DATABASE_URL", "postgres://x/y")
	t.Setenv("ASR_CLIENT_TOKENS", "chronicle:"+strings.Repeat("a", 34))
	t.Setenv("ASR_WHISPER_SERVER_ADDR", "8081")

	if _, err := Load(); err == nil {
		t.Fatal("a bare port was accepted as an address")
	} else if !strings.Contains(err.Error(), "ASR_WHISPER_SERVER_ADDR") {
		t.Fatalf("the error does not name the variable: %v", err)
	}
}

// The defaults the decision fixed, read back through Load so a rename of an
// environment variable cannot pass silently.
func TestCHRN26DefaultsAreTheSettledOnes(t *testing.T) {
	t.Setenv("ASR_DATABASE_URL", "postgres://x/y")
	t.Setenv("ASR_CLIENT_TOKENS", "chronicle:"+strings.Repeat("a", 34))

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.ModelSwitchMaxWait != 60*time.Second {
		t.Fatalf("ASR_MODEL_SWITCH_MAX_WAIT defaults to %s; ruling 1 settled it at 60s, and "+
			"CHRN-29 publishes that number to client two", c.ModelSwitchMaxWait)
	}
	if c.InferenceDeadlineFactor != 5 || c.MinInferenceDeadline != 30*time.Second {
		t.Fatalf("the deadline is %vx floored at %s; §7 says 5x floored at 30s",
			c.InferenceDeadlineFactor, c.MinInferenceDeadline)
	}
	if c.DeviceID != "r9700" {
		t.Fatalf("ASR_DEVICE_ID defaults to %q", c.DeviceID)
	}
	if c.WhisperServerBin != "whisper-server" || c.WhisperServerAddr != "127.0.0.1:8081" {
		t.Fatalf("the resident child is %q on %q", c.WhisperServerBin, c.WhisperServerAddr)
	}
}

// The ceilings are refused at zero rather than read as "no ceiling". An
// unbounded retry loop is the thing CHRN-28 exists to close, and it should not
// be reachable by typing 0.
func TestTheAttemptCeilingsRefuseZero(t *testing.T) {
	for _, name := range []string{"ASR_MAX_ATTEMPTS", "ASR_MAX_ATTEMPTS_WEDGED"} {
		for _, v := range []string{"0", "-1", "many"} {
			t.Run(name+"="+v, func(t *testing.T) {
				t.Setenv("ASR_DATABASE_URL", "postgres://x/y")
				t.Setenv("ASR_CLIENT_TOKENS", "chronicle:"+strings.Repeat("a", 34))
				t.Setenv(name, v)

				if _, err := Load(); err == nil {
					t.Fatalf("%s=%q booted; an unbounded retry loop is one typo away", name, v)
				} else if !strings.Contains(err.Error(), name) {
					t.Fatalf("the error does not name the variable: %v", err)
				}
			})
		}
	}
}

func TestTheAttemptCeilingsDefaultToTheSettledNumbers(t *testing.T) {
	t.Setenv("ASR_DATABASE_URL", "postgres://x/y")
	t.Setenv("ASR_CLIENT_TOKENS", "chronicle:"+strings.Repeat("a", 34))

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.MaxAttempts != 5 || c.MaxAttemptsWedged != 2 {
		t.Fatalf("ceilings are %d and %d; want 5 ordinary and 2 for a wedged run",
			c.MaxAttempts, c.MaxAttemptsWedged)
	}
}
