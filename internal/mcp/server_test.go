package mcp

import (
	"slices"
	"strings"
	"testing"

	"github.com/vercel-labs/agent-browser-mcp/internal/browser"
	"github.com/vercel-labs/agent-browser-mcp/internal/config"
	"github.com/vercel-labs/agent-browser-mcp/internal/mcp/tools"
)

func TestInstructionsContainTriageAndEfficiencyRules(t *testing.T) {
	cfg := &config.Config{Toolsets: config.AllToolsets, Project: "Shop", Purpose: "QA", Session: "qa"}
	got := BuildInstructions(cfg, browser.CLIVersion{})
	for _, r := range TriageRules {
		if row := "| " + r.Symptom + " | " + r.Start + " |"; !strings.Contains(got, row) {
			t.Errorf("instructions missing triage row %q", row)
		}
	}
	for _, r := range EfficiencyRules {
		if !strings.Contains(got, "- "+r+"\n") {
			t.Errorf("instructions missing efficiency line %q", r)
		}
	}
	for _, s := range []string{"Project: Shop", "Purpose: QA", "Default session: qa", "Stay in one browser"} {
		if !strings.Contains(got, s) {
			t.Errorf("instructions missing %q", s)
		}
	}
	if strings.Contains(got, "toolsets are enabled") {
		t.Errorf("with every toolset on, there is nothing to say about toolsets:\n%s", got)
	}
}

// TestInstructionsOnlyTriageEnabledTools keeps the triage table from sending
// the model to tools that a narrower --toolsets did not register.
func TestInstructionsOnlyTriageEnabledTools(t *testing.T) {
	cfg := &config.Config{Toolsets: []string{config.ToolsetCore, config.ToolsetStorage}}
	got := BuildInstructions(cfg, browser.CLIVersion{})
	if !strings.Contains(got, "Only these toolsets are enabled: core, storage.") {
		t.Errorf("instructions do not say which toolsets are on:\n%s", got)
	}
	for _, r := range TriageRules {
		row := "| " + r.Symptom + " |"
		enabled := !slices.ContainsFunc(r.Toolsets, func(t string) bool { return !cfg.HasToolset(t) })
		if strings.Contains(got, row) != enabled {
			t.Errorf("row %q shown=%v, want %v", r.Symptom, !enabled, enabled)
		}
	}
}

// TestInstructionsFitClaudeCodeLimit: Claude Code cuts each server's
// instructions at 2,048 characters, which would drop the end of the guidance.
func TestInstructionsFitClaudeCodeLimit(t *testing.T) {
	cfg := &config.Config{
		Toolsets: config.AllToolsets,
		Project:  strings.Repeat("p", 30),
		Purpose:  strings.Repeat("u", 150),
		Session:  "qa-session",
	}
	got := BuildInstructions(cfg, browser.CLIVersion{Warning: "agent-browser 0.27.0 is older than 0.38.0, the oldest supported version"})
	if n := len([]rune(got)); n > 2048 {
		t.Fatalf("instructions are %d characters with a typical config; Claude Code keeps 2048", n)
	}
}

// TestInstructionsNameRealTools catches a tool rename that would leave the
// guidance pointing at a tool that no longer exists.
func TestInstructionsNameRealTools(t *testing.T) {
	cfg := &config.Config{Toolsets: config.AllToolsets, DefaultTimeout: 1000, AgentBrowserPath: "agent-browser"}
	reg := tools.NewRegistry(cfg, browser.NewManager(cfg))
	text := BuildInstructions(cfg, browser.CLIVersion{})
	for _, name := range []string{"navigate", "snapshot", "page_text", "mouse", "screenshot", "batch", "find", "eval_script", "wait", "network", "console", "debugger", "performance", "debug_ui", "application", "emulate", "close_browser", "elements"} {
		if !strings.Contains(text, name) {
			t.Errorf("instructions no longer mention %s", name)
		}
		if _, ok := reg.Handler(name); !ok {
			t.Errorf("instructions mention %s but no such tool is registered", name)
		}
	}
}

// TestInstructionsNameRealArguments checks the actions and parameters the
// triage table tells the model to use.
func TestInstructionsNameRealArguments(t *testing.T) {
	cfg := &config.Config{Toolsets: config.AllToolsets, DefaultTimeout: 1000, AgentBrowserPath: "agent-browser"}
	reg := tools.NewRegistry(cfg, browser.NewManager(cfg))
	text := BuildInstructions(cfg, browser.CLIVersion{})
	cases := []struct{ tool, param, value string }{
		{"performance", "action", "vitals"},
		{"performance", "action", "lighthouse"},
		{"performance", "action", "heap_snapshot"},
		{"performance", "action", "coverage_start"},
		{"performance", "action", "coverage_stop"},
		{"debug_ui", "action", "rendering"},
		{"debug_ui", "fpsMeter", ""},
		{"debug_ui", "paintFlashing", ""},
		{"debug_ui", "layoutShifts", ""},
		{"console", "kind", "errors"},
		{"network", "status", ""},
		{"emulate", "device", ""},
		{"debugger", "action", "breakpoint"},
		{"debugger", "action", "scope"},
		{"screenshot", "annotate", ""},
		{"elements", "action", "styles"},
		{"elements", "action", "computed"},
		{"elements", "action", "contrast"},
		{"elements", "action", "a11y"},
	}
	props := map[string]map[string]any{}
	for _, st := range reg.Tools() {
		props[st.Tool.Name] = st.Tool.InputSchema.Properties
	}
	for _, c := range cases {
		word := c.value
		if word == "" {
			word = c.param
		}
		if !strings.Contains(text, word) {
			t.Errorf("instructions no longer mention %s; drop the case", word)
		}
		p, ok := props[c.tool][c.param].(map[string]any)
		if !ok {
			t.Errorf("%s has no %s parameter", c.tool, c.param)
			continue
		}
		if c.value == "" {
			continue
		}
		var enum []string
		switch e := p["enum"].(type) {
		case []string:
			enum = e
		case []any:
			for _, v := range e {
				enum = append(enum, v.(string))
			}
		}
		if !slices.Contains(enum, c.value) {
			t.Errorf("%s %s has no value %q (have %v)", c.tool, c.param, c.value, enum)
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
