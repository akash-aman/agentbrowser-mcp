// Command agent-browser-mcp is an MCP server that exposes every agent-browser
// CLI command as MCP tools. It speaks the Model Context Protocol over stdio.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/mark3labs/mcp-go/server"

	"github.com/vercel-labs/agent-browser-mcp/internal/browser"
	"github.com/vercel-labs/agent-browser-mcp/internal/config"
	mcpsrv "github.com/vercel-labs/agent-browser-mcp/internal/mcp"
)

const version = "1.0.0"

func main() {
	cfg, err := config.Load(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "agent-browser-mcp: configuration error:", err)
		os.Exit(1)
	}

	mgr := browser.NewManager(cfg)

	// Clean up sessions on shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Fprintln(os.Stderr, "agent-browser-mcp: shutting down, closing browser sessions...")
		mgr.CloseAll(context.Background())
		os.Exit(0)
	}()

	fmt.Fprintf(os.Stderr, "agent-browser-mcp: listening on stdio\n")

	s := mcpsrv.NewServer(version, cfg, mgr)
	if err := server.ServeStdio(s); err != nil {
		fmt.Fprintln(os.Stderr, "agent-browser-mcp: server error:", err)
		mgr.CloseAll(context.Background())
		os.Exit(1)
	}
}
