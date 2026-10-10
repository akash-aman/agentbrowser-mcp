package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// healthShown is how many errors, and how many failed requests, a page
// health note lists.
const healthShown = 3

// navigation is action for tools that load a page. It also reports the
// uncaught JS errors and failed requests the load caused, so the model
// notices problems without being asked to look for them.
func (r *Registry) navigation(fn argvFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, err := fn(req)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		health := r.watchHealth(ctx, req)
		res := r.run(ctx, req, args...)
		if res.IsError {
			return res, nil
		}
		if note := health(ctx); note != "" {
			appendText(res, note)
		}
		r.appendSnapshot(ctx, req, res)
		return res, nil
	}
}

// watchHealth notes the page errors so far and returns a function that,
// healthSettle after the navigation, describes the errors and failed
// requests since, or returns "" when there were none. It does not watch
// when a breakpoint or exception pause could stop the load, since a paused
// page could hold the extra commands. The debugger being on is not enough:
// edit_source turns it on, and the check stayed off for the whole session.
func (r *Registry) watchHealth(ctx context.Context, req mcp.CallToolRequest) func(context.Context) string {
	page := r.dt.Existing(getSession(req))
	if page != nil && page.CanPause() {
		return func(context.Context) string { return "" }
	}
	before, errsKnown := r.pageErrors(ctx, req)
	start := time.Now().UnixMilli()
	return func(ctx context.Context) string {
		select {
		case <-ctx.Done():
			return ""
		case <-time.After(r.healthSettle):
		}
		if page != nil && page.Paused() != nil {
			return ""
		}
		var errs []string
		if after, ok := r.pageErrors(ctx, req); ok && errsKnown {
			if len(after) >= len(before) {
				errs = after[len(before):]
			} else {
				errs = after // the log was cleared in between
			}
		}
		return healthNote(errs, r.failedRequests(ctx, req, start))
	}
}

// pageErrors returns the session's uncaught page errors, one line each, and
// whether the CLI could list them.
func (r *Registry) pageErrors(ctx context.Context, req mcp.CallToolRequest) ([]string, bool) {
	out, err := r.mgr.Run(ctx, getSession(req), "errors")
	if err != nil {
		return nil, false
	}
	var data consoleData
	if json.Unmarshal(out.Data, &data) != nil {
		return nil, false
	}
	lines := make([]string, 0, len(data.Errors))
	for _, e := range data.Errors {
		line, frame, _ := strings.Cut(e.Text, "\n")
		switch frame = strings.TrimSpace(frame); {
		case e.URL != "":
			line += fmt.Sprintf(" (%s:%d:%d)", e.URL, e.Line, e.Column)
		case strings.HasPrefix(frame, "at "):
			line += " (" + frame + ")"
		}
		lines = append(lines, line)
	}
	return lines, true
}

// failedRequests lists the requests since start (Unix ms) that got an HTTP
// error status, as "status type url". The favicon Chrome asks for on its
// own is left out.
func (r *Registry) failedRequests(ctx context.Context, req mcp.CallToolRequest, start int64) []string {
	out, err := r.mgr.Run(ctx, getSession(req), "network", "requests", "--status", "400-599")
	if err != nil {
		return nil
	}
	var data struct {
		Requests []struct {
			Status       int     `json:"status"`
			ResourceType string  `json:"resourceType"`
			URL          string  `json:"url"`
			Timestamp    float64 `json:"timestamp"`
		} `json:"requests"`
	}
	if json.Unmarshal(out.Data, &data) != nil {
		return nil
	}
	var lines []string
	for _, q := range data.Requests {
		if q.Status < 400 || int64(q.Timestamp) < start || strings.HasSuffix(q.URL, "/favicon.ico") {
			continue
		}
		lines = append(lines, fmt.Sprintf("%d %s %s", q.Status, q.ResourceType, q.URL))
	}
	return lines
}

// healthNote describes what went wrong during a page load, or "" if nothing did.
func healthNote(errs, failed []string) string {
	if len(errs) == 0 && len(failed) == 0 {
		return ""
	}
	var counts []string
	if n := len(errs); n > 0 {
		counts = append(counts, plural(n, "uncaught JS error"))
	}
	if n := len(failed); n > 0 {
		counts = append(counts, plural(n, "failed request"))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Page problems during this load: %s. Look into them unless they are expected for this page:", strings.Join(counts, " and "))
	list := func(prefix string, lines []string, more string) {
		for _, l := range lines[:min(len(lines), healthShown)] {
			b.WriteString("\n- " + prefix + clip(l, 300))
		}
		if n := len(lines) - healthShown; n > 0 {
			fmt.Fprintf(&b, "\n- … %d more: %s", n, more)
		}
	}
	list("JS error: ", errs, `console kind:"errors"`)
	list("", failed, `network status:"400-599"`)
	return b.String()
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// clip cuts s to n runes, marking the cut.
func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}
