package mcp

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	sdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

// StdioConfig is what the stdio transport needs: where the API is, and the one
// credential the process runs as.
type StdioConfig struct {
	BaseURL string
	Token   string
	Build   Build
}

// stdioStartTimeout bounds the one call made before the server starts.
const stdioStartTimeout = 15 * time.Second

// RunStdio serves MCP on stdin/stdout as the account cfg.Token names, until
// ctx ends or the client closes the stream.
//
// It asks the API who the token belongs to BEFORE it speaks MCP, and refuses
// to start if it cannot find out. The identity decides which tools exist, so a
// server that started without one would have to either offer nothing or guess.
// Nothing is ever written to stdout but the protocol: a failure here is an
// error for the caller to print on stderr.
func RunStdio(ctx context.Context, cfg StdioConfig) error {
	return RunStdioOn(ctx, cfg, &sdk.StdioTransport{})
}

// RunStdioOn is RunStdio over a transport the caller supplies. The process
// passes the real stdin and stdout; a test passes one end of a pipe.
func RunStdioOn(ctx context.Context, cfg StdioConfig, transport sdk.Transport) error {
	api, err := NewAPI(cfg.BaseURL, cfg.Token, &http.Client{Timeout: 30 * time.Second})
	if err != nil {
		return fmt.Errorf("mcp: %w", err)
	}

	startCtx, cancel := context.WithTimeout(ctx, stdioStartTimeout)
	id, err := resolve(startCtx, TransportStdio, api)
	cancel()
	if errors.Is(err, ErrUnauthorized) {
		return fmt.Errorf("mcp: CHRONICLE_TOKEN was refused by %s (unknown, revoked, or not a session token)", cfg.BaseURL)
	}
	if err != nil {
		return fmt.Errorf("mcp: cannot establish who this process runs as: %w", err)
	}

	server := BuildServer(cfg.Build, Session{Identity: id, API: api}, nil)
	return server.Run(ctx, transport)
}
