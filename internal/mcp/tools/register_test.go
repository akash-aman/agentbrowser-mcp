package tools

import (
	"slices"
	"strings"
	"testing"

	"github.com/xcode-studio/agentbrowser-mcp/internal/config"
)

var wantToolsets = map[string][]string{
	config.ToolsetCore: {"navigate", "click", "fill", "type", "press_key", "element_action", "select_option", "scroll", "drag",
		"upload_file", "download", "eval_script", "close_browser", "snapshot", "page_text", "get", "find", "wait", "screenshot",
		"mouse", "tabs", "dialog", "console", "batch", "help"},
	config.ToolsetNetwork:   {"network"},
	config.ToolsetDevtools:  {"save_pdf", "performance", "react", "record", "diff", "debug_ui", "debugger", "elements", "cdp"},
	config.ToolsetEmulation: {"emulate"},
	config.ToolsetStorage:   {"cookies", "storage", "state", "clipboard", "session", "auth", "application"},
}

func toolNames(e *env) []string {
	var names []string
	for _, st := range e.reg.Tools() {
		names = append(names, st.Tool.Name)
	}
	slices.Sort(names)
	return names
}

func TestToolsetFiltering(t *testing.T) {
	t.Parallel()
	cases := []struct {
		flag string
		sets []string
	}{
		{"all", config.AllToolsets},
		{"core", []string{config.ToolsetCore}},
		{"core,network", []string{config.ToolsetCore, config.ToolsetNetwork}},
		{"storage", []string{config.ToolsetCore, config.ToolsetStorage}},
	}
	for _, c := range cases {
		t.Run(c.flag, func(t *testing.T) {
			t.Parallel()
			sets, err := config.ParseToolsets(c.flag)
			if err != nil {
				t.Fatal(err)
			}
			e := newEnv(t, func(cfg *config.Config) { cfg.Toolsets = sets })
			var want []string
			for _, s := range c.sets {
				want = append(want, wantToolsets[s]...)
			}
			slices.Sort(want)
			if got := toolNames(e); !slices.Equal(got, want) {
				t.Fatalf("tools\n got %v\nwant %v", got, want)
			}
		})
	}
}

func TestToolsetAssignment(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	for set, names := range wantToolsets {
		for _, n := range names {
			if got := e.reg.Toolset(n); got != set {
				t.Errorf("%s is in toolset %q, want %q", n, got, set)
			}
		}
	}
}

func TestToolAnnotations(t *testing.T) {
	t.Parallel()
	readOnlyTools := []string{"snapshot", "page_text", "get", "wait", "screenshot"}
	destructiveTools := []string{"close_browser", "cookies", "storage", "state", "application", "cdp"}
	e := newEnv(t)
	for _, st := range e.reg.Tools() {
		ann, name := st.Tool.Annotations, st.Tool.Name
		isRO := ann.ReadOnlyHint != nil && *ann.ReadOnlyHint
		isDestructive := ann.DestructiveHint != nil && *ann.DestructiveHint
		if isRO != slices.Contains(readOnlyTools, name) {
			t.Errorf("%s readOnlyHint = %v", name, isRO)
		}
		if isDestructive != slices.Contains(destructiveTools, name) {
			t.Errorf("%s destructiveHint = %v", name, isDestructive)
		}
	}
}

func TestEveryToolTakesSession(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	for _, st := range e.reg.Tools() {
		if st.Tool.Name == "help" {
			continue
		}
		p, ok := st.Tool.InputSchema.Properties["session"].(map[string]any)
		if !ok {
			t.Errorf("%s has no session parameter", st.Tool.Name)
			continue
		}
		// Models otherwise open a session per task, each in its own window.
		if d, _ := p["description"].(string); !strings.Contains(d, "opens another window") {
			t.Errorf("%s session description %q does not warn that a new session opens a window", st.Tool.Name, d)
		}
	}
}

// TestSchemaBudget stops tool schemas from creeping back toward the 1.x size
// (150 tools, 67 KB of compact JSON sent with every request). The budget
// covers all toolsets including the CDP debugger, profiling, application and
// Elements tools, and descriptions that say when to reach for each DevTools
// tool; --toolsets core is under half of it. Claude Code loads schemas on
// demand, so most of this is only sent when a tool is first used.
func TestSchemaBudget(t *testing.T) {
	t.Parallel()
	const budget = 48 << 10
	e := newEnv(t)
	size := len(e.toolsList())
	t.Logf("tools/list with all toolsets: %d tools, %d bytes", len(e.reg.Tools()), size)
	if size > budget {
		t.Fatalf("tools/list is %d bytes, budget %d", size, budget)
	}
}
