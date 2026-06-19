package tools

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/vercel-labs/agent-browser-mcp/internal/browser"
)

func registerNavigation(s *server.MCPServer, mgr *browser.Manager) {
	s.AddTool(mcp.NewTool("navigate_back",
		mcp.WithDescription("Navigate back in browser history."),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleBack(mgr))

	s.AddTool(mcp.NewTool("navigate_forward",
		mcp.WithDescription("Navigate forward in browser history."),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleForward(mgr))

	s.AddTool(mcp.NewTool("reload_page",
		mcp.WithDescription("Reload the current page."),
		mcp.WithString("session", mcp.Description("Session name.")),
		mcp.WithBoolean("ignoreCache", mcp.Description("Ignore browser cache on reload.")),
	), handleReload(mgr))

	s.AddTool(mcp.NewTool("spa_navigate",
		mcp.WithDescription("SPA client-side navigation using pushState. Auto-detects window.next.router.push for Next.js, falls back to history.pushState."),
		mcp.WithString("url", mcp.Required(), mcp.Description("URL to navigate to (client-side).")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handlePushState(mgr))
}

func handleBack(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return runCmd(ctx, mgr, getSession(mcp.CallToolRequest{}), "back")
	}
}

func handleForward(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return runCmd(ctx, mgr, getSession(mcp.CallToolRequest{}), "forward")
	}
}

func handleReload(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		args := []string{"reload"}
		if request.GetBool("ignoreCache", false) {
			args = append(args, "--ignore-cache")
		}
		return runCmd(ctx, mgr, session, args...)
	}
}

func handlePushState(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		url, _ := request.RequireString("url")
		return runCmd(ctx, mgr, session, "pushstate", url)
	}
}
