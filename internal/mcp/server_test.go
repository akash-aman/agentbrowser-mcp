package mcp

import (
	"strings"
	"testing"

	"github.com/vercel-labs/agent-browser-mcp/internal/browser"
	"github.com/vercel-labs/agent-browser-mcp/internal/config"
	"github.com/vercel-labs/agent-browser-mcp/internal/mcp/tools"
)

func TestInstructionsContainEfficiencyTable(t *testing.T) {
	cfg := &config.Config{Toolsets: config.AllToolsets, Project: "Shop", Purpose: "QA", Session: "qa"}
	got := BuildInstructions(cfg, browser.CLIVersion{})
	for _, r := range EfficiencyRules {
		row := "| " + r[0] + " | " + r[1] + " | " + r[2] + " |"
		if !strings.Contains(got, row) {
			t.Errorf("instructions missing row %q", row)
		}
	}
	for _, s := range []string{"Project: Shop", "Purpose: QA", "Default session: qa", "Enabled toolsets: core, network, devtools, emulation, storage"} {
		if !strings.Contains(got, s) {
			t.Errorf("instructions missing %q", s)
		}
	}
}

// TestInstructionsNameRealTools catches a tool rename that would leave the
// guidance pointing at a tool that no longer exists.
func TestInstructionsNameRealTools(t *testing.T) {
	cfg := &config.Config{Toolsets: config.AllToolsets, DefaultTimeout: 1000, AgentBrowserPath: "agent-browser"}
	reg := tools.NewRegistry(cfg, browser.NewManager(cfg))
	text := BuildInstructions(cfg, browser.CLIVersion{})
	for _, name := range []string{"navigate", "snapshot", "page_text", "click", "fill", "mouse", "screenshot", "batch", "find", "eval_script", "wait", "network", "console", "get", "debugger", "performance"} {
		if !strings.Contains(text, name) {
			t.Errorf("instructions no longer mention %s", name)
		}
		if _, ok := reg.Handler(name); !ok {
			t.Errorf("instructions mention %s but no such tool is registered", name)
		}
	}
}

func TestInstructionsWarnAboutUnsupportedCLI(t *testing.T) {
	cfg := &config.Config{Toolsets: config.AllToolsets}
	if got := BuildInstructions(cfg, browser.CLIVersion{Version: browser.TestedCLIVersion}); strings.Contains(got, "Warning") {
		t.Fatalf("supported CLI must not warn:\n%s", got)
	}
	got := BuildInstructions(cfg, browser.CLIVersion{Version: "0.27.0", Warning: "agent-browser 0.27.0 is older than 0.38.0"})
	if !strings.Contains(got, "Warning: agent-browser 0.27.0 is older than 0.38.0. Tools may fail") {
		t.Fatalf("missing version warning:\n%s", got)
	}
}
