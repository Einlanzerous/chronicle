package config

import (
	"strings"
	"testing"
)

func TestLoadMCPStdio(t *testing.T) {
	t.Setenv("CHRONICLE_URL", "https://chronicle-direct.example.com/")
	t.Setenv("CHRONICLE_TOKEN", " chr_abc ")
	c, err := LoadMCPStdio()
	if err != nil {
		t.Fatalf("LoadMCPStdio: %v", err)
	}
	if c.URL != "https://chronicle-direct.example.com" || c.Token != "chr_abc" {
		t.Errorf("got %+v", c)
	}

	// No default URL: a wrong default sends a credential somewhere unintended.
	for name, env := range map[string][2]string{
		"no URL":         {"", "chr_abc"},
		"no token":       {"https://chronicle-direct.example.com", ""},
		"not a URL":      {"chronicle-direct.example.com", "chr_abc"},
		"not http":       {"ftp://chronicle-direct.example.com", "chr_abc"},
		"a query string": {"https://chronicle-direct.example.com/?x=1", "chr_abc"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Setenv("CHRONICLE_URL", env[0])
			t.Setenv("CHRONICLE_TOKEN", env[1])
			if _, err := LoadMCPStdio(); err == nil {
				t.Error("accepted; want an error")
			}
		})
	}
}

func TestLoadMCPHosted(t *testing.T) {
	good := func(t *testing.T) {
		t.Setenv("CHRONICLE_MCP_API_URL", "http://chronicle:4009")
		t.Setenv("CHRONICLE_MCP_PUBLIC_URL", "https://chronicle-mcp.example.com/mcp/")
		t.Setenv("CHRONICLE_CF_ACCESS_TEAM_DOMAIN", "team.cloudflareaccess.com")
		t.Setenv("CHRONICLE_MCP_CF_ACCESS_AUD", "mcp-tag")
		t.Setenv("CHRONICLE_MCP_PORT", "")
	}

	good(t)
	c, err := LoadMCPHosted()
	if err != nil {
		t.Fatalf("LoadMCPHosted: %v", err)
	}
	want := MCPHosted{
		APIURL: "http://chronicle:4009", Addr: ":4080", CFAccessTeamDomain: "team.cloudflareaccess.com",
		CFAccessAUD: "mcp-tag", PublicURL: "https://chronicle-mcp.example.com/mcp",
	}
	if c != want {
		t.Errorf("got %+v, want %+v", c, want)
	}

	for name, unset := range map[string]string{
		"no API URL":     "CHRONICLE_MCP_API_URL",
		"no public URL":  "CHRONICLE_MCP_PUBLIC_URL",
		"no team domain": "CHRONICLE_CF_ACCESS_TEAM_DOMAIN",
		"no audience":    "CHRONICLE_MCP_CF_ACCESS_AUD",
	} {
		t.Run(name, func(t *testing.T) {
			good(t)
			t.Setenv(unset, "")
			_, err := LoadMCPHosted()
			if err == nil || !strings.Contains(err.Error(), unset) {
				t.Errorf("err = %v, want one naming %s", err, unset)
			}
		})
	}

	// The web application's tag is not a substitute, however similar the name.
	t.Run("the web audience variable is not read", func(t *testing.T) {
		good(t)
		t.Setenv("CHRONICLE_MCP_CF_ACCESS_AUD", "")
		t.Setenv("CHRONICLE_CF_ACCESS_AUD", "web-tag")
		if _, err := LoadMCPHosted(); err == nil {
			t.Error("the web application's audience was accepted for the MCP endpoint")
		}
	})
	t.Run("a public URL with no path", func(t *testing.T) {
		good(t)
		t.Setenv("CHRONICLE_MCP_PUBLIC_URL", "https://chronicle-mcp.example.com")
		if _, err := LoadMCPHosted(); err == nil {
			t.Error("accepted a public URL that names no endpoint path")
		}
	})
	t.Run("a bad port", func(t *testing.T) {
		good(t)
		t.Setenv("CHRONICLE_MCP_PORT", "99999")
		if _, err := LoadMCPHosted(); err == nil {
			t.Error("accepted port 99999")
		}
	})
}

// Neither MCP loader can be satisfied by, or cares about, a database DSN or --
// for the hosted one -- a token. Stated as a test because the absence is the
// design.
func TestMCPLoadersHoldNoDatabaseAndTheHostedOneNoToken(t *testing.T) {
	t.Setenv("CHRONICLE_DATABASE_URL", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("CHRONICLE_TOKEN", "")
	t.Setenv("CHRONICLE_MCP_API_URL", "http://chronicle:4009")
	t.Setenv("CHRONICLE_MCP_PUBLIC_URL", "https://chronicle-mcp.example.com/mcp")
	t.Setenv("CHRONICLE_CF_ACCESS_TEAM_DOMAIN", "team.cloudflareaccess.com")
	t.Setenv("CHRONICLE_MCP_CF_ACCESS_AUD", "mcp-tag")
	if _, err := LoadMCPHosted(); err != nil {
		t.Errorf("LoadMCPHosted needed a DSN or a token: %v", err)
	}
}
