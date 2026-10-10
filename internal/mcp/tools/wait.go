package tools

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/xcode-studio/agentbrowser-mcp/internal/config"
)

func (r *Registry) registerWait() {
	r.add(config.ToolsetCore, mcp.NewTool("wait",
		mcp.WithDescription("Wait for an element, its disappearance, text, URL, load state, JS condition, or a download. Prefer these over for=time fixed sleeps."),
		mcp.WithString("for", mcp.Required(), mcp.Enum(waitFors...)),
		mcp.WithString("value", mcp.Description("Selector (element/hidden), text, URL glob, load state (load|domcontentloaded|networkidle, default load), JS expression, milliseconds, or download save path (for a download that starts after this call; the download tool clicks and saves in one step).")),
		sessionParam(), readOnly(),
	), r.handleWait)
}

func (r *Registry) handleWait(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	switch req.GetString("for", "") {
	case "hidden":
	case "download":
		// agent-browser waits for the next download only, so one that a
		// click just before started and finished is never seen.
		res, err := r.cli(waitArgv)(ctx, req)
		if res != nil && res.IsError && strings.Contains(resultText(res), "timed out") {
			return mcp.NewToolResultError("no download started while waiting. A download that began before this call (e.g. from the click just before) is missed: " +
				"use the download tool, which clicks and saves in one step."), nil
		}
		return res, err
	default:
		return r.cli(waitArgv)(ctx, req)
	}
	args, err := waitArgv(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return r.waitHidden(ctx, req, args[1]), nil
}

// waitHidden polls until the element is gone or not visible. agent-browser's
// own "wait <selector> --state hidden" cannot be used: its global --state
// option (a storage state file) takes the flag, and loading "state" from a
// file named hidden relaunches the browser, losing the page.
func (r *Registry) waitHidden(ctx context.Context, req mcp.CallToolRequest, sel string) *mcp.CallToolResult {
	deadline := time.Now().Add(r.waitHiddenFor)
	for {
		res, err := r.mgr.Run(ctx, getSession(req), "is", "visible", sel)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				return mcp.NewToolResultText(sel + " is gone")
			}
			return mcp.NewToolResultError(err.Error())
		}
		var data struct {
			Visible bool `json:"visible"`
		}
		if json.Unmarshal(res.Data, &data) == nil && !data.Visible {
			return mcp.NewToolResultText(sel + " is hidden")
		}
		if time.Now().After(deadline) {
			return mcp.NewToolResultError(fmt.Sprintf("%s is still visible after %v", sel, r.waitHiddenFor))
		}
		select {
		case <-ctx.Done():
			return mcp.NewToolResultError(ctx.Err().Error())
		case <-time.After(250 * time.Millisecond):
		}
	}
}

var waitFors = []string{"element", "hidden", "text", "url", "load", "function", "time", "download"}

func waitArgv(req mcp.CallToolRequest) ([]string, error) {
	b := newArgv(req, "wait")
	switch kind := b.enum("for", "", waitFors...); kind {
	case "load":
		b.add("--load", cmp.Or(b.str("value"), "load"))
	case "download":
		b.add("--download").opt("value")
	case "element":
		b.add(b.requiredFor("value", "for=element"))
	case "hidden":
		b.add(b.requiredFor("value", "for=hidden")) // polled by waitHidden
	case "text", "url":
		b.add("--"+kind, b.requiredFor("value", "for="+kind))
	case "function":
		b.add("--fn", b.requiredFor("value", "for=function"))
	case "time":
		b.add(b.milliseconds())
	}
	return b.done()
}

func (b *argv) milliseconds() string {
	ms := b.requiredFor("value", "for=time")
	if _, err := strconv.Atoi(ms); err != nil && ms != "" {
		b.fail(fmt.Errorf("value must be milliseconds for for=time, got %q", ms))
	}
	return ms
}
