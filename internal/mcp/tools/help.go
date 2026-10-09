package tools

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/vercel-labs/agent-browser-mcp/internal/config"
)

func (r *Registry) registerHelp() {
	r.add(config.ToolsetCore, mcp.NewTool("help",
		mcp.WithDescription("agent-browser CLI help for a command (discover), version and environment diagnosis (doctor), or bundled usage guides (skills)."),
		mcp.WithString("topic", mcp.Enum("discover", "doctor", "skills"), mcp.Description("Default discover.")),
		mcp.WithString("command", mcp.Description("discover: command path, e.g. network or tab new.")),
		mcp.WithString("name", mcp.Description("skills: skill to load, e.g. core. Default lists skills.")),
		mcp.WithBoolean("full", mcp.Description("skills: include the full command reference.")),
		mcp.WithBoolean("fix", mcp.Description("doctor: also repair what it finds.")),
		mutating(),
	), r.handleHelp)
}

func helpArgv(req mcp.CallToolRequest) ([]string, error) {
	b := newArgv(req)
	switch b.enum("topic", "discover", "discover", "doctor", "skills") {
	case "doctor":
		b.add("doctor").boolFlag("--fix", "fix")
	case "skills":
		if b.str("name") == "" {
			b.add("skills", "list")
		} else {
			b.add("skills", "get").opt("name").boolFlag("--full", "full")
		}
	default:
		b.add(strings.Fields(b.str("command"))...).add("--help")
	}
	return b.done()
}

// handleHelp returns the CLI's plain-text output even when it exits non-zero,
// which --help sometimes does.
func (r *Registry) handleHelp(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, err := helpArgv(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	res, err := r.mgr.Run(ctx, "", args...)
	header := ""
	if args[0] == "doctor" {
		header = r.mgr.CheckVersion(ctx).String() + "\n\n"
		if res != nil {
			if report, ok := formatDoctor(res.RawStdout); ok {
				return textResult(header+report, r.cfg.MaxOutput), nil
			}
		}
	}
	if res != nil && !res.JSON {
		if text := strings.TrimSpace(res.RawStdout + "\n" + res.RawStderr); text != "" {
			return textResult(header+text, r.cfg.MaxOutput), nil
		}
	}
	if err != nil {
		return mcp.NewToolResultError(header + err.Error()), nil
	}
	return textResult(header+body(res), r.cfg.MaxOutput), nil
}

// doctorReport is `agent-browser doctor --json` output, which is its own
// shape rather than the usual {success, data, error} envelope.
type doctorReport struct {
	Checks  []doctorCheck `json:"checks"`
	Fixed   []any         `json:"fixed"`
	Summary struct {
		Pass, Warn, Fail int
	} `json:"summary"`
}

type doctorCheck struct {
	Category string `json:"category"`
	Message  string `json:"message"`
	Status   string `json:"status"` // pass, info, warn or fail
	Fix      string `json:"fix"`
}

// formatDoctor renders a doctor report as one line per check, problems
// first; ok is false when stdout is not a doctor report.
func formatDoctor(stdout string) (string, bool) {
	var d doctorReport
	if json.Unmarshal([]byte(stdout), &d) != nil || len(d.Checks) == 0 {
		return "", false
	}
	checks := slices.Clone(d.Checks)
	slices.SortStableFunc(checks, func(a, b doctorCheck) int {
		return cmp.Compare(statusOrder(a.Status), statusOrder(b.Status))
	})
	lines := []string{fmt.Sprintf("%d pass, %d warn, %d fail", d.Summary.Pass, d.Summary.Warn, d.Summary.Fail)}
	for _, c := range checks {
		line := fmt.Sprintf("[%s] %s: %s", c.Status, c.Category, c.Message)
		if c.Fix != "" {
			line += " — fix: " + c.Fix
		}
		lines = append(lines, line)
	}
	for _, f := range d.Fixed {
		lines = append(lines, "fixed: "+scalarText(f))
	}
	return strings.Join(lines, "\n"), true
}

// statusOrder puts failures, then warnings, before everything else.
func statusOrder(status string) int {
	switch status {
	case "fail":
		return 0
	case "warn":
		return 1
	}
	return 2
}
