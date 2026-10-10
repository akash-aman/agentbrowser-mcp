// Command agent-browser-mcp is an MCP server that exposes every agent-browser
// CLI command as MCP tools. It speaks the Model Context Protocol over stdio.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"sync"
	"syscall"

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
	stop := sync.OnceFunc(func() { shutdown(context.Background()) })
	// A client stops a stdio server with SIGINT, then SIGTERM and SIGKILL
	// within half a second (Claude Code on reconnect). ServeStdio first waits
	// for the calls in flight, which can take seconds (a click on a paused
	// page, a slow CLI command), so the server was killed before it released
	// its CDP connections. Exit as soon as they are released instead.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigs
		stop()
		os.Exit(0)
	}()
	// ServeStdio returns when stdin closes or on SIGINT/SIGTERM, which it
	// traps too (as context.Canceled); both are a normal stop.
	err = server.ServeStdio(s)
	stop()
	if err != nil && !errors.Is(err, context.Canceled) {
		fmt.Fprintln(os.Stderr, "agent-browser-mcp: server error:", err)
		os.Exit(1)
	}
}
