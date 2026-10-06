package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// The two MCP subcommands (CHRN-65) have loaders of their own, and neither
// calls Load. That is deliberate and it is the point: Load requires a database
// DSN, and an MCP process is a CLIENT of Chronicle's API that holds no DSN and
// no database role. A loader that could not return one is a stronger statement
// of that than a field nobody reads.

// MCPStdio is what `chronicle mcp` needs.
type MCPStdio struct {
	// URL is where Chronicle's API is. Required, with no default: a wrong
	// default would send a credential somewhere nobody chose.
	URL string
	// Token is the session token of the ONE account this process runs as.
	Token string
}

// LoadMCPStdio reads the stdio transport's configuration.
func LoadMCPStdio() (MCPStdio, error) {
	var c MCPStdio
	var err error
	if c.URL, err = absoluteURL("CHRONICLE_URL", false); err != nil {
		return c, err
	}
	c.Token = strings.TrimSpace(os.Getenv("CHRONICLE_TOKEN"))
	if c.Token == "" {
		return c, fmt.Errorf("config: CHRONICLE_TOKEN is required (the session token of the account this process runs as)")
	}
	return c, nil
}

// MCPHosted is what `chronicle mcp-serve` needs.
//
// THERE IS NO TOKEN IN IT, and LoadMCPHosted does not read CHRONICLE_TOKEN.
// The hosted transport's identity is each request's Access assertion; a
// credential in this process's environment would be one token acting for
// everyone who connects.
type MCPHosted struct {
	// APIURL is where Chronicle's API is, for the exchange and for tool calls.
	APIURL string
	// Addr is the listen address.
	Addr string
	// CFAccessTeamDomain is the Zero Trust team domain -- the same fact, under
	// the same name, as serve's.
	CFAccessTeamDomain string
	// CFAccessAUD is the MCP Access application's own audience tag. The same
	// variable serve reads as MCPCFAccessAUD: one value, two readers. It is
	// NOT serve's CHRONICLE_CF_ACCESS_AUD, which is the web application's.
	CFAccessAUD string
	// PublicURL is the MCP endpoint as clients reach it. Configured, never
	// derived from the Host header.
	PublicURL string
}

// DefaultMCPPort is the hosted transport's in-container port. Nothing
// publishes it; the edge reaches it by service name.
const DefaultMCPPort = 4080

// LoadMCPHosted reads the hosted transport's configuration.
func LoadMCPHosted() (MCPHosted, error) {
	var c MCPHosted
	var err error
	if c.APIURL, err = absoluteURL("CHRONICLE_MCP_API_URL", false); err != nil {
		return c, err
	}
	// The path is part of it: it is the route this process serves MCP on, and
	// the resource the 401's metadata pointer describes.
	if c.PublicURL, err = absoluteURL("CHRONICLE_MCP_PUBLIC_URL", true); err != nil {
		return c, err
	}

	c.CFAccessTeamDomain = strings.TrimSpace(os.Getenv("CHRONICLE_CF_ACCESS_TEAM_DOMAIN"))
	if c.CFAccessTeamDomain == "" {
		return c, fmt.Errorf("config: CHRONICLE_CF_ACCESS_TEAM_DOMAIN is required (the Zero Trust team domain)")
	}
	c.CFAccessAUD = strings.TrimSpace(os.Getenv("CHRONICLE_MCP_CF_ACCESS_AUD"))
	if c.CFAccessAUD == "" {
		return c, fmt.Errorf("config: CHRONICLE_MCP_CF_ACCESS_AUD is required " +
			"(the MCP Access application's own audience tag, not CHRONICLE_CF_ACCESS_AUD)")
	}

	port := DefaultMCPPort
	if v := strings.TrimSpace(os.Getenv("CHRONICLE_MCP_PORT")); v != "" {
		port, err = strconv.Atoi(v)
		if err != nil || port <= 0 || port > 65535 {
			return c, fmt.Errorf("config: CHRONICLE_MCP_PORT %q is not a valid port", v)
		}
	}
	c.Addr = fmt.Sprintf(":%d", port)
	return c, nil
}

// absoluteURL reads a required http(s) URL, trimmed of trailing slashes.
func absoluteURL(name string, pathRequired bool) (string, error) {
	v := strings.TrimRight(strings.TrimSpace(os.Getenv(name)), "/")
	if v == "" {
		return "", fmt.Errorf("config: %s is required", name)
	}
	u, err := url.Parse(v)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("config: %s %q is not an absolute http(s) URL", name, v)
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("config: %s %q must not carry a query string or a fragment", name, v)
	}
	if pathRequired && strings.Trim(u.Path, "/") == "" {
		return "", fmt.Errorf("config: %s %q must include the endpoint's path (e.g. /mcp)", name, v)
	}
	return v, nil
}
