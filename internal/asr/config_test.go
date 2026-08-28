package asr

import (
	"strings"
	"testing"
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
