package tools

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/vercel-labs/agent-browser-mcp/internal/browser"
)

func registerCookies(s *server.MCPServer, mgr *browser.Manager) {
	s.AddTool(mcp.NewTool("cookies_list",
		mcp.WithDescription("Get all cookies for the current page."),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleCookies(mgr))

	s.AddTool(mcp.NewTool("cookies_set",
		mcp.WithDescription("Set a cookie for the current page domain."),
		mcp.WithString("name", mcp.Required(), mcp.Description("Cookie name.")),
		mcp.WithString("value", mcp.Required(), mcp.Description("Cookie value.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleCookieSet(mgr))

	s.AddTool(mcp.NewTool("cookies_import",
		mcp.WithDescription("Import cookies from a Copy-as-cURL dump, JSON array, or bare Cookie header. Auto-detects format."),
		mcp.WithString("source", mcp.Required(), mcp.Description("Copy-as-cURL file path, JSON array file, or raw Cookie header string.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleCookieImport(mgr))

	s.AddTool(mcp.NewTool("cookies_clear",
		mcp.WithDescription("Clear all cookies for the current page."),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleCookieClear(mgr))
}

func handleCookies(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return runCmd(ctx, mgr, getSession(request), "cookies")
	}
}

func handleCookieSet(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		name, _ := request.RequireString("name")
		value, _ := request.RequireString("value")
		return runCmd(ctx, mgr, session, "cookies", "set", name, value)
	}
}

func handleCookieImport(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		source, _ := request.RequireString("source")
		return runCmd(ctx, mgr, session, "cookies", "set", "--curl", source)
	}
}

func handleCookieClear(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return runCmd(ctx, mgr, getSession(request), "cookies", "clear")
	}
}
