package tools

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/vercel-labs/agent-browser-mcp/internal/browser"
)

func registerState(s *server.MCPServer, mgr *browser.Manager) {
	s.AddTool(mcp.NewTool("state_save",
		mcp.WithDescription("Save the current session state (cookies, localStorage) to a file for later reuse."),
		mcp.WithString("path", mcp.Required(), mcp.Description("File path to save state JSON.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleStateSave(mgr))

	s.AddTool(mcp.NewTool("state_load",
		mcp.WithDescription("Load a previously saved state file into the current session."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Path to the state JSON file.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleStateLoad(mgr))

	s.AddTool(mcp.NewTool("state_list",
		mcp.WithDescription("List all saved state files."),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleStateList(mgr))

	s.AddTool(mcp.NewTool("state_show",
		mcp.WithDescription("Show a summary of a saved state file."),
		mcp.WithString("file", mcp.Required(), mcp.Description("State file name.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleStateShow(mgr))

	s.AddTool(mcp.NewTool("state_clear",
		mcp.WithDescription("Clear saved states for a session or all states."),
		mcp.WithBoolean("all", mcp.Description("Clear all saved states, not just the current session.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleStateClear(mgr))
}

func handleStateSave(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		path, _ := request.RequireString("path")
		return runCmd(ctx, mgr, session, "state", "save", path)
	}
}

func handleStateLoad(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		path, _ := request.RequireString("path")
		return runCmd(ctx, mgr, session, "state", "load", path)
	}
}

func handleStateList(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return runCmd(ctx, mgr, getSession(request), "state", "list")
	}
}

func handleStateShow(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		file, _ := request.RequireString("file")
		return runCmd(ctx, mgr, session, "state", "show", file)
	}
}

func handleStateClear(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		if request.GetBool("all", false) {
			return runCmd(ctx, mgr, session, "state", "clear", "--all")
		}
		return runCmd(ctx, mgr, session, "state", "clear")
	}
}
