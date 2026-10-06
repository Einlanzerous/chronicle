package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

// captureStdout runs f with os.Stdout redirected and returns what was written.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	orig := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = orig }()
	f()
	_ = w.Close()
	out, _ := io.ReadAll(r)
	return string(out)
}

// On stdio, stdout is the protocol: a process that cannot start must say so on
// stderr -- which is where main prints the error run returns -- and put
// nothing at all on stdout for a client to choke on.
func TestMCPThatCannotStartWritesNothingToStdout(t *testing.T) {
	for name, env := range map[string][2]string{
		"no token":           {"https://chronicle-direct.example.com", ""},
		"no URL":             {"", "chr_abc"},
		"an unreachable API": {"http://127.0.0.1:1", "chr_abc"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("CHRONICLE_URL", env[0])
			t.Setenv("CHRONICLE_TOKEN", env[1])
			var err error
			out := captureStdout(t, func() { err = run([]string{"mcp"}) })
			if err == nil {
				t.Fatal("run(mcp) returned nil; want an error for main to print on stderr")
			}
			if strings.Contains(err.Error(), "\n") {
				t.Errorf("the error is more than one line: %q", err)
			}
			if out != "" {
				t.Errorf("stdout = %q; nothing but MCP may be written there", out)
			}
		})
	}
}

// Neither MCP subcommand needs, or reads, a database DSN.
func TestMCPSubcommandsDoNotAskForADatabase(t *testing.T) {
	t.Setenv("CHRONICLE_DATABASE_URL", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("CHRONICLE_URL", "")
	t.Setenv("CHRONICLE_TOKEN", "")
	t.Setenv("CHRONICLE_MCP_API_URL", "")
	for _, sub := range []string{"mcp", "mcp-serve"} {
		err := run([]string{sub})
		if err == nil {
			t.Fatalf("run(%s) started with nothing configured", sub)
		}
		if strings.Contains(err.Error(), "DATABASE_URL") {
			t.Errorf("run(%s) asked for a database: %v", sub, err)
		}
	}
}
