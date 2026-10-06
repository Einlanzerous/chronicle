package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Einlanzerous/chronicle/internal/cfaccess"
	"github.com/Einlanzerous/chronicle/internal/config"
	"github.com/Einlanzerous/chronicle/internal/mcp"
)

// CHRN-65 — the two MCP transports, as subcommands of the one binary.
//
// Neither opens the database, and neither calls config.Load, which would
// require a DSN: an MCP process is a client of Chronicle's HTTP API. The hosted
// container is this same image with a different command.

// runMCP serves MCP on stdin/stdout as the account CHRONICLE_TOKEN names.
//
// STDOUT IS THE PROTOCOL. Nothing here may print to it; every diagnostic goes
// to stderr, and a failure to start is returned for main to print there.
func runMCP(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("mcp takes no arguments (configure it with CHRONICLE_URL and CHRONICLE_TOKEN)")
	}
	cfg, err := config.LoadMCPStdio()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	err = mcp.RunStdio(ctx, mcp.StdioConfig{
		BaseURL: cfg.URL, Token: cfg.Token,
		Build: mcp.Build{Version: buildVersion(), Commit: commit},
	})
	// The client closing the stream, or a signal, is how this ends normally.
	if err == nil || errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

// mcpShutdownGrace bounds the HTTP server's own drain. The revokes that follow
// it are bounded separately, inside the transport, and the two together must
// fit a container's stop grace (Docker's default is 10 s).
const mcpShutdownGrace = 3 * time.Second

// runMCPServe serves MCP over Streamable HTTP for hosted clients, behind
// Cloudflare Access.
func runMCPServe(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("mcp-serve takes no arguments (it is configured from the environment)")
	}
	cfg, err := config.LoadMCPHosted()
	if err != nil {
		return err
	}
	// JSON at info, to stdout, like serve's default: the shape Dozzle and
	// Datadog read. There is no Config to take a level from -- see above.
	logger := config.Config{LogFormat: "json"}.Logger(os.Stdout)

	hosted, err := mcp.NewHosted(mcp.HostedConfig{
		APIURL: cfg.APIURL, PublicURL: cfg.PublicURL, TeamDomain: cfg.CFAccessTeamDomain,
		// This endpoint's own audience and no other: an assertion issued for
		// the web application is somebody's browser, and must not open a
		// session here.
		Verifier: cfaccess.New(cfg.CFAccessTeamDomain, []string{cfg.CFAccessAUD}),
		Build:    mcp.Build{Version: buildVersion(), Commit: commit},
		Logger:   logger,
	})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go hosted.Run(ctx)

	srv := &http.Server{Addr: cfg.Addr, Handler: hosted, ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	logger.Info("mcp: hosted transport listening", "addr", cfg.Addr, "public", cfg.PublicURL, "api", cfg.APIURL,
		"version", buildVersion())

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	drain, cancel := context.WithTimeout(context.Background(), mcpShutdownGrace)
	defer cancel()
	_ = srv.Shutdown(drain)
	// Every open session's token is revoked before the process exits; a token
	// whose revoke never left is one only its own expiry will end.
	hosted.Close()
	logger.Info("mcp: hosted transport stopped")
	return nil
}
