package mcp

import (
	"context"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// Build is what this server says it is in the MCP handshake.
type Build struct {
	Version string
	Commit  string
}

// Tool is one entry in the registry: what the tool is, whether it changes
// anything, and how to attach it to a server for a session.
type Tool struct {
	Name        string
	Description string
	// Mutates is true for a tool that changes anything in Chronicle. It is the
	// ONE thing BuildServer consults to decide whether a session is offered
	// the tool, and the tool's readOnlyHint is derived from it, so the two
	// cannot disagree. A tool that writes and declares false here is the bug
	// this field exists to make visible in review.
	Mutates bool

	attach func(*sdk.Server, *sdk.Tool, Session)
}

// define declares a tool with typed input and output. The handler receives the
// session it was registered for; it never looks an identity up for itself.
func define[In, Out any](name, description string, mutates bool,
	handler func(context.Context, Session, In) (Out, error)) Tool {
	return Tool{
		Name: name, Description: description, Mutates: mutates,
		attach: func(s *sdk.Server, t *sdk.Tool, sess Session) {
			sdk.AddTool(s, t, func(ctx context.Context, _ *sdk.CallToolRequest, in In) (*sdk.CallToolResult, Out, error) {
				out, err := handler(ctx, sess, in)
				return nil, out, err
			})
		},
	}
}

// Tools is the registry: every tool this server has, for any session. CHRN-66
// adds the reads here and CHRN-67 the writes; which of them a given session is
// offered is BuildServer's decision and not theirs.
func Tools() []Tool {
	return []Tool{whoami}
}

// BuildServer returns an MCP server for one session, carrying the tools that
// session may be offered.
//
// THIS IS THE ONLY PLACE A TOOL IS ATTACHED TO A SERVER, for either transport,
// and so the only place the write gate is applied: a tool that Mutates is
// skipped unless the session's identity MayWrite.
func BuildServer(build Build, sess Session, opts *sdk.ServerOptions) *sdk.Server {
	return buildServer(build, sess, opts, Tools())
}

// buildServer is BuildServer over an explicit tool list, so a test can put a
// mutating tool through the gate before any real one exists.
func buildServer(build Build, sess Session, opts *sdk.ServerOptions, tools []Tool) *sdk.Server {
	version := build.Version
	if version == "" {
		version = "dev"
	}
	s := sdk.NewServer(&sdk.Implementation{Name: "chronicle", Title: "Chronicle", Version: version}, opts)
	for _, t := range tools {
		if t.Mutates && !sess.Identity.MayWrite() {
			continue
		}
		t.attach(s, &sdk.Tool{
			Name:        t.Name,
			Description: t.Description,
			Annotations: &sdk.ToolAnnotations{ReadOnlyHint: !t.Mutates},
		}, sess)
	}
	return s
}
