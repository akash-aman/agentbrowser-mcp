package browser

import (
	"context"
	"strings"
	"testing"

	"github.com/xcode-studio/agentbrowser-mcp/internal/config"
)

func TestCheckVersion(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, output, version, warning string
	}{
		{"tested", "agent-browser 0.38.2", "0.38.2", ""},
		{"oldest supported", "agent-browser 0.38.0", "0.38.0", ""},
		{"newer", "agent-browser 1.2.0", "1.2.0", ""},
		{"too old", "agent-browser 0.27.0", "0.27.0", "agent-browser 0.27.0 is older than 0.38.0, the oldest supported version; upgrade with `npm install -g agent-browser@latest`"},
		{"minor compares numerically", "agent-browser 0.9.9", "0.9.9", "older than 0.38.0"},
		{"unreadable", "agent-browser dev", "", `cannot read a version from "agent-browser dev"`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			m, fake := newManager(t)
			fake.SetVersion(c.output)
			got := m.CheckVersion(context.Background())
			if got.Version != c.version {
				t.Fatalf("version %q, want %q", got.Version, c.version)
			}
			if (c.warning == "") != (got.Warning == "") || !strings.Contains(got.Warning, c.warning) {
				t.Fatalf("warning %q, want %q", got.Warning, c.warning)
			}
		})
	}
}

func TestCheckVersionRunsOnce(t *testing.T) {
	t.Parallel()
	m, fake := newManager(t)
	m.CheckVersion(context.Background())
	fake.SetVersion("agent-browser 0.1.0")
	if got := m.CheckVersion(context.Background()); got.Version != TestedCLIVersion || len(fake.Calls()) != 1 {
		t.Fatalf("second check must reuse the first answer, got %+v after %d calls", got, len(fake.Calls()))
	}
}

func TestCheckVersionMissingBinary(t *testing.T) {
	t.Parallel()
	m := NewManager(&config.Config{AgentBrowserPath: "/nonexistent/agent-browser"})
	got := m.CheckVersion(context.Background())
	if got.Version != "" || !strings.Contains(got.Warning, "cannot run /nonexistent/agent-browser --version") {
		t.Fatalf("got %+v", got)
	}
}

func TestCLIVersionString(t *testing.T) {
	t.Parallel()
	if got := (CLIVersion{Version: "0.38.2"}).String(); got != "agent-browser 0.38.2 (supported: 0.38.0 or newer; tested with 0.38.2)" {
		t.Fatalf("got %q", got)
	}
	got := CLIVersion{Warning: "cannot run x"}.String()
	if got != "agent-browser unknown (supported: 0.38.0 or newer; tested with 0.38.2)\nWarning: cannot run x" {
		t.Fatalf("got %q", got)
	}
}

func TestSupportedRangeIsConsistent(t *testing.T) {
	t.Parallel()
	if compareVersions(TestedCLIVersion, MinCLIVersion) < 0 {
		t.Fatalf("tested version %s is below the minimum %s", TestedCLIVersion, MinCLIVersion)
	}
}
