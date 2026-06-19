// Package mcp wires agent-browser CLI onto an MCP server.
package mcp

import (
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/server"

	"github.com/vercel-labs/agent-browser-mcp/internal/browser"
	"github.com/vercel-labs/agent-browser-mcp/internal/config"
	"github.com/vercel-labs/agent-browser-mcp/internal/mcp/tools"
)

// NewServer creates a fully-wired MCP server backed by the agent-browser CLI.
func NewServer(version string, cfg *config.Config, mgr *browser.Manager) *server.MCPServer {
	s := server.NewMCPServer(
		cfg.Name,
		version,
		server.WithToolCapabilities(false),
		server.WithRecovery(),
		server.WithInstructions(buildInstructions(cfg)),
	)

	tools.RegisterAll(s, cfg, mgr)

	return s
}

func buildInstructions(cfg *config.Config) string {
	var b strings.Builder
	fmt.Fprintf(&b, "This is an MCP server for agent-browser, a browser automation CLI for AI agents.\n")
	fmt.Fprintf(&b, "It provides full browser control: navigation, interaction, screenshots, network tracing, console inspection, and session management.\n\n")
	if cfg.Project != "" {
		fmt.Fprintf(&b, "Project: %s\n", cfg.Project)
	}
	if cfg.Purpose != "" {
		fmt.Fprintf(&b, "Purpose: %s\n", cfg.Purpose)
	}
	if cfg.Session != "" {
		fmt.Fprintf(&b, "Default session: %s\n", cfg.Session)
	}
	b.WriteString("\n")
	b.WriteString("## For non-vision models\n")
	b.WriteString("Every tool returns structured text output (JSON accessibility snapshots, page titles, URLs, network request/response bodies, console messages). ")
	b.WriteString("Use snapshot for page structure, network_requests for API traffic, console for JS logs — no screenshots needed.\n\n")
	b.WriteString("## Core workflow\n")
	b.WriteString("1. navigate_page to open a URL\n")
	b.WriteString("2. take_snapshot for accessibility tree with refs (@e1, @e2)\n")
	b.WriteString("3. click / fill using refs to interact\n")
	b.WriteString("4. Re-snapshot after page changes\n\n")
	b.WriteString("## Session management\n")
	b.WriteString("Every tool accepts an optional `session` parameter for isolated browser instances. ")
	b.WriteString("Default session used if omitted. Use session_list to see active sessions.\n\n")
	b.WriteString("## Help\n")
	b.WriteString("Use help_discover to discover available agent-browser subcommands and flags. ")
	b.WriteString("Use help_doctor to diagnose the environment. Use help_skills to list built-in skills.")
	return b.String()
}
