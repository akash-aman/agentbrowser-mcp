package tools

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/vercel-labs/agent-browser-mcp/internal/browser"
)

func registerWait(s *server.MCPServer, mgr *browser.Manager) {
	s.AddTool(mcp.NewTool("wait_for_element",
		mcp.WithDescription("Wait for an element to appear (selector) or disappear (--state hidden)."),
		mcp.WithString("selector", mcp.Description("Element selector to wait for.")),
		mcp.WithString("state", mcp.Description("Wait state: 'visible' (default) or 'hidden'.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleWaitElement(mgr))

	s.AddTool(mcp.NewTool("wait_for_text",
		mcp.WithDescription("Wait for text to appear on the page (substring match)."),
		mcp.WithString("text", mcp.Required(), mcp.Description("Text to wait for.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleWaitText(mgr))

	s.AddTool(mcp.NewTool("wait_for_url",
		mcp.WithDescription("Wait for the URL to match a glob pattern (e.g., '**/dashboard')."),
		mcp.WithString("pattern", mcp.Required(), mcp.Description("Glob pattern to match URL against.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleWaitURL(mgr))

	s.AddTool(mcp.NewTool("wait_for_load",
		mcp.WithDescription("Wait for a specific load state: 'load', 'domcontentloaded', or 'networkidle'."),
		mcp.WithString("state", mcp.Required(), mcp.Description("Load state: load, domcontentloaded, networkidle.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleWaitLoad(mgr))

	s.AddTool(mcp.NewTool("wait_for_time",
		mcp.WithDescription("Wait for a specific number of milliseconds."),
		mcp.WithNumber("milliseconds", mcp.Required(), mcp.Description("Time to wait in milliseconds.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleWaitTime(mgr))

	s.AddTool(mcp.NewTool("wait_for_function",
		mcp.WithDescription("Wait for a JavaScript condition to be true (e.g., 'window.ready === true')."),
		mcp.WithString("script", mcp.Required(), mcp.Description("JavaScript expression that returns true when ready.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleWaitFn(mgr))
}

func handleWaitElement(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		selector := request.GetString("selector", "")
		state := request.GetString("state", "visible")
		args := []string{"wait"}
		if state == "hidden" {
			args = append(args, selector, "--state", "hidden")
		} else if selector != "" {
			args = append(args, selector)
		}
		return runCmd(ctx, mgr, session, args...)
	}
}

func handleWaitText(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		text, _ := request.RequireString("text")
		return runCmd(ctx, mgr, session, "wait", "--text", text)
	}
}

func handleWaitURL(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		pattern, _ := request.RequireString("pattern")
		return runCmd(ctx, mgr, session, "wait", "--url", pattern)
	}
}

func handleWaitLoad(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		state, _ := request.RequireString("state")
		return runCmd(ctx, mgr, session, "wait", "--load", state)
	}
}

func handleWaitTime(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		ms := request.GetFloat("milliseconds", 0)
		return runCmd(ctx, mgr, session, "wait", intToStr(int(ms)))
	}
}

func handleWaitFn(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		script, _ := request.RequireString("script")
		return runCmd(ctx, mgr, session, "wait", "--fn", script)
	}
}
