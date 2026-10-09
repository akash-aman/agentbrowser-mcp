// Package mcp wires agent-browser CLI onto an MCP server.
package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/server"

	"github.com/vercel-labs/agent-browser-mcp/internal/browser"
	"github.com/vercel-labs/agent-browser-mcp/internal/config"
	"github.com/vercel-labs/agent-browser-mcp/internal/mcp/tools"
)

// NewServer creates a fully-wired MCP server backed by the agent-browser CLI,
// and the function to call when it stops.
func NewServer(version string, cfg *config.Config, mgr *browser.Manager) (*server.MCPServer, func(context.Context)) {
	s := server.NewMCPServer(
		cfg.Name,
		version,
		server.WithToolCapabilities(false),
		server.WithRecovery(),
		server.WithInstructions(BuildInstructions(cfg, mgr.CheckVersion(context.Background()))),
	)
	reg := tools.RegisterAll(s, cfg, mgr)
	return s, func(ctx context.Context) { shutdown(ctx, cfg, mgr, reg) }
}

// shutdown releases this server's CDP connections, which also resumes a page
// it paused and drops its throttling. Browser sessions stay open unless
// CloseOnExit is set, because they are daemons shared with other clients and
// the next server after a reconnect.
func shutdown(ctx context.Context, cfg *config.Config, mgr *browser.Manager, reg *tools.Registry) {
	reg.Close()
	if cfg.CloseOnExit {
		mgr.CloseAll(ctx)
	}
}

// EfficiencyRules is the "choose the cheapest tool" table sent to the model.
// Each row is goal | prefer | avoid.
var EfficiencyRules = [][3]string{
	{"See page structure", "snapshot (interactive:true, selector to scope)", "screenshot — only for visual/layout checks"},
	{"Read content", "page_text (with selector)", "full snapshot, get html"},
	{"Act on an element", "click/fill/... with @ref", "mouse x,y — only canvas, maps, or targets with no @ref"},
	{"Vision: locate what you see", "screenshot annotate:true, then act by @ref", "estimating coordinates from pixels"},
	{"Several known steps", "one batch call", "one call per step"},
	{"See what an action changed", `snapshot:"delta" on the action, or snapshot delta:true`, "a fresh full snapshot"},
	{"Find by visible text or label", "find", "eval_script with querySelector"},
	{"Wait for the page", "wait for element/text/url/load", "wait for=time fixed sleeps"},
	{"Debug API or JS", "network / console with filter, pattern, limit", "unfiltered dumps"},
	{"Step through JavaScript", "debugger breakpoint, trigger it with click/eval_script, then stack/scope/evaluate", "logging with eval_script"},
	{"Find performance problems", "performance vitals or metrics first; lighthouse for a full audit", "trace/profiler files unless you need a deep dive"},
}

// BuildInstructions renders the server instructions for cfg, warning the
// model when the installed agent-browser CLI is not a supported version.
func BuildInstructions(cfg *config.Config, cli browser.CLIVersion) string {
	var b strings.Builder
	b.WriteString("Browser automation via agent-browser: navigation, interaction, screenshots, network, console, sessions.\n")
	if cfg.Project != "" {
		fmt.Fprintf(&b, "Project: %s\n", cfg.Project)
	}
	if cfg.Purpose != "" {
		fmt.Fprintf(&b, "Purpose: %s\n", cfg.Purpose)
	}
	if cfg.Session != "" {
		fmt.Fprintf(&b, "Default session: %s\n", cfg.Session)
	}
	if cli.Warning != "" {
		fmt.Fprintf(&b, "Warning: %s. Tools may fail until it is fixed; tell the user.\n", cli.Warning)
	}
	b.WriteString("\n## Workflow\n")
	b.WriteString("1. navigate to a URL\n")
	b.WriteString("2. snapshot to get the accessibility tree with @refs (@e1, @e2)\n")
	b.WriteString("3. click/fill/type with those @refs; a ref lasts while its element does (until it is replaced or the page navigates), and snapshot:\"delta\" on an action shows what changed\n")
	b.WriteString("\n## Choose the cheapest tool\n")
	b.WriteString("| Goal | Prefer | Avoid / only when |\n|---|---|---|\n")
	for _, r := range EfficiencyRules {
		fmt.Fprintf(&b, "| %s | %s | %s |\n", r[0], r[1], r[2])
	}
	b.WriteString("\nEvery tool takes an optional `session` for an isolated browser. ")
	fmt.Fprintf(&b, "Enabled toolsets: %s.", strings.Join(cfg.Toolsets, ", "))
	return b.String()
}
