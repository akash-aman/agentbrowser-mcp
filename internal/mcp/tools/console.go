package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/xcode-studio/agentbrowser-mcp/internal/config"
)

func (r *Registry) registerConsole() {
	r.add(config.ToolsetCore, mcp.NewTool("console",
		mcp.WithDescription("Console messages (kind=log), uncaught page errors (errors), or DevTools Issues such as CORS, mixed content, cookie and deprecation problems (issues), newest last. Check errors and issues first when something is broken, blank or not loading; filter with pattern and limit instead of reading everything."),
		mcp.WithString("kind", mcp.Enum("log", "errors", "issues"), mcp.Description("Default log.")),
		mcp.WithString("pattern", mcp.Description("Case-insensitive regex; only matching messages are returned.")),
		mcp.WithNumber("limit", mcp.Description("Return at most the last N messages. Default 50.")),
		mcp.WithBoolean("clear", mcp.Description("Clear the buffer after reading.")),
		sessionParam(), mutating(),
	), r.handleConsole)
}

type consoleData struct {
	Messages []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"messages"`
	Errors []struct {
		Text   string `json:"text"`
		URL    string `json:"url"`
		Line   int    `json:"line"`
		Column int    `json:"column"`
	} `json:"errors"`
}

func (r *Registry) handleConsole(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	b := newArgv(req)
	kind := b.enum("kind", "log", "log", "errors", "issues")
	if _, err := b.done(); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	filter, err := newLineFilter(req, kind)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if kind == "issues" {
		return r.issues(ctx, req, filter), nil
	}
	cmd := map[string]string{"log": "console", "errors": "errors"}[kind]

	out, err := r.mgr.Run(ctx, getSession(req), cmd)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	var data consoleData
	if err := json.Unmarshal(out.Data, &data); err != nil {
		return textResult(formatData(out.Data), r.cfg.MaxOutput), nil
	}
	text := filter.apply(data.lines())
	if req.GetBool("clear", false) {
		text += r.clearBuffer(ctx, req, cmd, "--clear")
	}
	return textResult(text, r.cfg.MaxOutput), nil
}

// issues reads the DevTools Issues panel over CDP.
func (r *Registry) issues(ctx context.Context, req mcp.CallToolRequest, filter lineFilter) *mcp.CallToolResult {
	page, err := r.livePage(ctx, req)
	if err != nil {
		return mcp.NewToolResultError(err.Error())
	}
	issues, err := page.Issues(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error())
	}
	return textResult(filter.apply(issues), r.cfg.MaxOutput)
}

func (d consoleData) lines() []string {
	var lines []string
	for _, m := range d.Messages {
		lines = append(lines, fmt.Sprintf("[%s] %s", m.Type, m.Text))
	}
	for _, e := range d.Errors {
		line := e.Text
		if e.URL != "" {
			line += fmt.Sprintf(" (%s:%d:%d)", e.URL, e.Line, e.Column)
		}
		lines = append(lines, line)
	}
	return lines
}

// clearBuffer runs a separate clear command, since the CLI's --clear only
// clears and does not return what it cleared. It returns a note on failure.
func (r *Registry) clearBuffer(ctx context.Context, req mcp.CallToolRequest, args ...string) string {
	if _, err := r.mgr.Run(ctx, getSession(req), args...); err != nil {
		return "\nclear failed: " + err.Error()
	}
	return ""
}

// lineFilter keeps lines matching pattern, then the newest limit of them.
type lineFilter struct {
	pattern *regexp.Regexp
	limit   int
	noun    string
}

func newLineFilter(req mcp.CallToolRequest, noun string) (lineFilter, error) {
	f := lineFilter{limit: int(req.GetFloat("limit", 50)), noun: noun}
	p := req.GetString("pattern", "")
	if p == "" {
		return f, nil
	}
	re, err := regexp.Compile("(?i)" + p)
	if err != nil {
		return f, fmt.Errorf("invalid pattern: %w", err)
	}
	f.pattern = re
	return f, nil
}

// apply filters lines and says how many were dropped, so the model knows the
// view is partial.
func (f lineFilter) apply(lines []string) string {
	total := len(lines)
	if total == 0 {
		return "(no " + f.noun + ")"
	}
	matched := f.match(lines)
	shown := matched
	if f.limit > 0 && len(shown) > f.limit {
		shown = shown[len(shown)-f.limit:]
	}
	header := fmt.Sprintf("%d of %d %s", len(shown), total, f.noun)
	if f.pattern != nil {
		header += fmt.Sprintf(" (%d matched pattern)", len(matched))
	}
	if len(shown) == 0 {
		return header
	}
	return header + "\n" + strings.Join(shown, "\n")
}

func (f lineFilter) match(lines []string) []string {
	if f.pattern == nil {
		return lines
	}
	var kept []string
	for _, l := range lines {
		// "[log] price 5": ^price should match the message, and \[error\]
		// the level.
		message := l
		if strings.HasPrefix(l, "[") {
			if _, rest, ok := strings.Cut(l, "] "); ok {
				message = rest
			}
		}
		if f.pattern.MatchString(l) || f.pattern.MatchString(message) {
			kept = append(kept, l)
		}
	}
	return kept
}
