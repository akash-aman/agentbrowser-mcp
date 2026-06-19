package tools

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/vercel-labs/agent-browser-mcp/internal/browser"
)

func registerTab(s *server.MCPServer, mgr *browser.Manager) {
	s.AddTool(mcp.NewTool("list_tabs",
		mcp.WithDescription("List all open browser tabs with their tab IDs and labels."),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleListTabs(mgr))

	s.AddTool(mcp.NewTool("new_tab",
		mcp.WithDescription("Open a new browser tab, optionally navigating to a URL."),
		mcp.WithString("url", mcp.Description("URL to navigate the new tab to.")),
		mcp.WithString("label", mcp.Description("User-assigned label for the tab (e.g., 'docs', 'admin').")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleNewTab(mgr))

	s.AddTool(mcp.NewTool("switch_tab",
		mcp.WithDescription("Switch to a specific tab by ID (e.g., 't1') or label."),
		mcp.WithString("tab", mcp.Required(), mcp.Description("Tab ID (t1, t2) or label.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleSwitchTab(mgr))

	s.AddTool(mcp.NewTool("close_tab",
		mcp.WithDescription("Close a specific tab by ID or label. Defaults to active tab if not specified."),
		mcp.WithString("tab", mcp.Description("Tab ID (t1, t2) or label to close. Closes active tab if omitted.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleCloseTab(mgr))

	s.AddTool(mcp.NewTool("new_window",
		mcp.WithDescription("Open a new browser window."),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleNewWindow(mgr))
}

func handleListTabs(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return runCmd(ctx, mgr, getSession(request), "tab")
	}
}

func handleNewTab(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		args := []string{"tab", "new"}
		if label := request.GetString("label", ""); label != "" {
			args = append(args, "--label", label)
		}
		if url := request.GetString("url", ""); url != "" {
			args = append(args, url)
		}
		return runCmd(ctx, mgr, session, args...)
	}
}

func handleSwitchTab(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		tab, _ := request.RequireString("tab")
		return runCmd(ctx, mgr, session, "tab", tab)
	}
}

func handleCloseTab(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		tab := request.GetString("tab", "")
		if tab != "" {
			return runCmd(ctx, mgr, session, "tab", "close", tab)
		}
		return runCmd(ctx, mgr, session, "tab", "close")
	}
}

func handleNewWindow(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return runCmd(ctx, mgr, getSession(request), "window", "new")
	}
}
