package main

import (
	"context"
	"strings"
	"testing"

	"github.com/Einlanzerous/chronicle/internal/config"
	"github.com/Einlanzerous/chronicle/internal/markdown"
	"github.com/Einlanzerous/chronicle/internal/resolve"
	"github.com/Einlanzerous/chronicle/internal/switchyard"
)

// CHRN-97 criterion 13: setup() registers the Switchyard and Amber transports
// when their configuration is present and neither otherwise; a Chronicle
// configured with neither starts normally, logs one line per upstream saying
// so, and answers `unconfigured` rather than refusing to boot.
//
// Port 1 answers nothing, so the configured cases prove a transport is
// REGISTERED by the state moving off unconfigured -- to unreachable -- rather
// than by a live upstream. Which transport is behind a card is the resolver's
// business; whether one is behind it at all is this file's.
func TestBuildResolverRegistersOnlyWhatIsConfigured(t *testing.T) {
	ticket := markdown.Reference{System: markdown.SystemSwitchyard, Key: "SWY", Token: "SWY-389", Number: 389}
	cite := markdown.Reference{System: markdown.SystemAmber, Token: "amber1.a.b.0"}

	states := func(t *testing.T, r *resolve.Resolver) (resolve.State, resolve.State) {
		t.Helper()
		out := r.Resolve(context.Background(), []markdown.Reference{ticket, cite})
		return out[0].State, out[1].State
	}

	t.Run("neither configured starts, says so twice, and answers unconfigured", func(t *testing.T) {
		logger, buf := quietLogger()
		r, keys, err := buildResolver(config.Config{}, nil, logger)
		if err != nil {
			t.Fatalf("a Chronicle with no upstreams refused to build its resolver: %v", err)
		}
		if keys != nil {
			t.Error("a key set was built with no tracker to fetch it from")
		}
		sy, am := states(t, r)
		if sy != resolve.StateUnconfigured || am != resolve.StateUnconfigured {
			t.Errorf("states = %s / %s, want unconfigured / unconfigured", sy, am)
		}
		// One line per upstream, at Info, naming the variables.
		for _, want := range []string{"no ticket tracker", "no capture archive"} {
			if n := strings.Count(buf.String(), want); n != 1 {
				t.Errorf("%q logged %d times, want exactly once:\n%s", want, n, buf.String())
			}
		}
		if strings.Contains(buf.String(), "level=WARN") {
			t.Errorf("an unset upstream is not a broken deployment and must not warn:\n%s", buf.String())
		}
	})

	t.Run("switchyard only", func(t *testing.T) {
		logger, buf := quietLogger()
		sw, err := switchyard.New("http://127.0.0.1:1", "not-a-real-token")
		if err != nil {
			t.Fatal(err)
		}
		r, keys, err := buildResolver(config.Config{SwitchyardURL: "http://127.0.0.1:1"}, sw, logger)
		if err != nil {
			t.Fatal(err)
		}
		if keys == nil {
			t.Error("no key set was built, so the scanner has nothing to recognise ticket references against")
		}
		sy, am := states(t, r)
		if sy == resolve.StateUnconfigured {
			t.Error("a ticket reference answered unconfigured with a tracker configured")
		}
		if am != resolve.StateUnconfigured {
			t.Errorf("a citation answered %s with no archive configured", am)
		}
		if strings.Contains(buf.String(), "not-a-real-token") {
			t.Error("the token reached a log line")
		}
	})

	t.Run("both", func(t *testing.T) {
		logger, buf := quietLogger()
		sw, err := switchyard.New("http://127.0.0.1:1", "not-a-real-token")
		if err != nil {
			t.Fatal(err)
		}
		cfg := config.Config{
			SwitchyardURL: "http://127.0.0.1:1",
			AmberURL:      "http://127.0.0.1:1",
			AmberToken:    "not-a-real-token-either",
		}
		r, _, err := buildResolver(cfg, sw, logger)
		if err != nil {
			t.Fatal(err)
		}
		sy, am := states(t, r)
		if sy == resolve.StateUnconfigured || am == resolve.StateUnconfigured {
			t.Errorf("states = %s / %s with both upstreams configured", sy, am)
		}
		if strings.Contains(buf.String(), "not-a-real-token") {
			t.Error("a token reached a log line")
		}
	})
}
