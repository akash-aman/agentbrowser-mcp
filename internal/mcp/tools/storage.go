package tools

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/vercel-labs/agent-browser-mcp/internal/browser"
)

func registerStorage(s *server.MCPServer, mgr *browser.Manager) {
	s.AddTool(mcp.NewTool("local_storage_list",
		mcp.WithDescription("Get all or a specific key from localStorage."),
		mcp.WithString("key", mcp.Description("Optional specific key to retrieve.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleLocalStorage(mgr))

	s.AddTool(mcp.NewTool("local_storage_set",
		mcp.WithDescription("Set a value in localStorage."),
		mcp.WithString("key", mcp.Required(), mcp.Description("Storage key.")),
		mcp.WithString("value", mcp.Required(), mcp.Description("Value to store.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleLocalStorageSet(mgr))

	s.AddTool(mcp.NewTool("local_storage_clear",
		mcp.WithDescription("Clear all localStorage for the current page."),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleLocalStorageClear(mgr))

	s.AddTool(mcp.NewTool("session_storage_list",
		mcp.WithDescription("Get all or a specific key from sessionStorage."),
		mcp.WithString("key", mcp.Description("Optional specific key to retrieve.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleSessionStorage(mgr))

	s.AddTool(mcp.NewTool("session_storage_set",
		mcp.WithDescription("Set a value in sessionStorage."),
		mcp.WithString("key", mcp.Required(), mcp.Description("Storage key.")),
		mcp.WithString("value", mcp.Required(), mcp.Description("Value to store.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleSessionStorageSet(mgr))

	s.AddTool(mcp.NewTool("session_storage_clear",
		mcp.WithDescription("Clear all sessionStorage for the current page."),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleSessionStorageClear(mgr))
}

func handleLocalStorage(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		key := request.GetString("key", "")
		if key != "" {
			return runCmd(ctx, mgr, session, "storage", "local", key)
		}
		return runCmd(ctx, mgr, session, "storage", "local")
	}
}

func handleLocalStorageSet(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		key, _ := request.RequireString("key")
		value, _ := request.RequireString("value")
		return runCmd(ctx, mgr, session, "storage", "local", "set", key, value)
	}
}

func handleLocalStorageClear(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return runCmd(ctx, mgr, getSession(request), "storage", "local", "clear")
	}
}

func handleSessionStorage(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		key := request.GetString("key", "")
		if key != "" {
			return runCmd(ctx, mgr, session, "storage", "session", key)
		}
		return runCmd(ctx, mgr, session, "storage", "session")
	}
}

func handleSessionStorageSet(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		key, _ := request.RequireString("key")
		value, _ := request.RequireString("value")
		return runCmd(ctx, mgr, session, "storage", "session", "set", key, value)
	}
}

func handleSessionStorageClear(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return runCmd(ctx, mgr, getSession(request), "storage", "session", "clear")
	}
}
