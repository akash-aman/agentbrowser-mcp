// Command agent-browser-mcp is an MCP server that exposes every agent-browser
// CLI command as MCP tools. It speaks the Model Context Protocol over stdio.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/mark3labs/mcp-go/server"

	"github.com/vercel-labs/agent-browser-mcp/internal/browser"
	"github.com/vercel-labs/agent-browser-mcp/internal/config"
	mcpsrv "github.com/vercel-labs/agent-browser-mcp/internal/mcp"
)

const version = "2.0.0"

func main() {
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "agent-browser-mcp: configuration error:", err)
		os.Exit(1)
	}

	mgr := browser.NewManager(cfg)
	s, shutdown := mcpsrv.NewServer(version, cfg, mgr)

	fmt.Fprintf(os.Stderr, "agent-browser-mcp %s: listening on stdio; %s\n", version, mgr.CheckVersion(context.Background()))
	// ServeStdio returns when stdin closes or on SIGINT/SIGTERM, which it
	// traps itself (as context.Canceled); both are a normal stop.
	err = server.ServeStdio(s)
	shutdown(context.Background())
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "agent-browser-mcp: server error:", err)
		os.Exit(1)
	}
}
