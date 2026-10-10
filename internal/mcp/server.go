// Package mcp wires agent-browser CLI onto an MCP server.
package mcp

import (
	"context"
	"fmt"
	"slices"
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

// TriageRule maps what a developer reports or asks for to the tools that
// investigate it, so the model reaches for them without being told to.
type TriageRule struct {
	Symptom, Start string
	Toolsets       []string // shown only when all of these are enabled
}

// TriageRules is the "investigate on your own" table sent to the model.
var TriageRules = []TriageRule{
	{`Slow page, "optimize", Web Vitals, SEO`, "performance vitals, lighthouse; trace or profiler for causes", []string{config.ToolsetDevtools}},
	{"Looks wrong: CSS, layout, contrast, a11y", "elements styles, computed, contrast, a11y", []string{config.ToolsetDevtools}},
	{"Jank, stutter, slow scroll/animation", "debug_ui rendering fpsMeter/paintFlashing, performance trace", []string{config.ToolsetDevtools}},
	{"Layout jumps", "performance vitals (CLS), debug_ui rendering layoutShifts", []string{config.ToolsetDevtools}},
	{"Bug, broken feature, wrong value", `console kind:"errors", network status:"400-599", debugger breakpoint + scope`, []string{config.ToolsetNetwork, config.ToolsetDevtools}},
	{"Memory grows", "performance heap_snapshot before/after", []string{config.ToolsetDevtools}},
	{"Big bundle, unused JS/CSS", "performance coverage_start, use page, coverage_stop", []string{config.ToolsetDevtools}},
	{"Caching, service worker, storage", "application", []string{config.ToolsetStorage}},
	{"Mobile/responsive layout", "emulate device, screenshot", []string{config.ToolsetEmulation}},
}

// EfficiencyRules are the "cheapest tool first" lines sent to the model.
var EfficiencyRules = []string{
	"snapshot (interactive:true) to see the page; screenshot for visual checks (annotate:true maps to @refs)",
	"page_text to read, find by text or label, @ref over mouse x,y (canvas, maps)",
	"batch known steps; wait for a condition, not a fixed time",
	"pattern and limit on console and network; debugger scope over eval_script logging",
}

// BuildInstructions renders the server instructions for cfg, warning the
// model when the installed agent-browser CLI is not a supported version.
// Claude Code cuts instructions at 2,048 characters, so the triage table
// comes first and everything stays short.
func BuildInstructions(cfg *config.Config, cli browser.CLIVersion) string {
	var b strings.Builder
	b.WriteString("Chrome DevTools via agent-browser: debug, profile, test and automate websites.\n")
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
	b.WriteString("\n## Investigate on your own\n")
	b.WriteString("Use unasked when you build, fix or test a site; users name symptoms, not tools.\n")
	b.WriteString("| Symptom or task | Start with |\n|---|---|\n")
	for _, r := range TriageRules {
		if slices.ContainsFunc(r.Toolsets, func(t string) bool { return !cfg.HasToolset(t) }) {
			continue
		}
		fmt.Fprintf(&b, "| %s | %s |\n", r.Symptom, r.Start)
	}
	b.WriteString("navigate reports load JS errors and failed requests; follow up. After code changes, reload and recheck.\n")
	b.WriteString("\n## Workflow\n")
	b.WriteString("navigate, snapshot for @refs, act by @ref (valid while its element exists); snapshot:\"delta\" on an action shows the change.\n")
	b.WriteString("Stay in one browser: omit session unless you need a separate login (each name opens a window); close_browser yours.\n")
	b.WriteString("\n## Cheapest tool first\n")
	for _, r := range EfficiencyRules {
		fmt.Fprintf(&b, "- %s\n", r)
	}
	if len(cfg.Toolsets) < len(config.AllToolsets) {
		fmt.Fprintf(&b, "\nOnly these toolsets are enabled: %s.", strings.Join(cfg.Toolsets, ", "))
	}
	return b.String()
}
