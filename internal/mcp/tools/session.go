package tools

import (
	"context"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/vercel-labs/agent-browser-mcp/internal/browser"
)

func registerSession(s *server.MCPServer, mgr *browser.Manager) {
	s.AddTool(mcp.NewTool("session_list",
		mcp.WithDescription("List all active browser sessions."),
	), handleSessionList(mgr))

	s.AddTool(mcp.NewTool("session_info",
		mcp.WithDescription("Show details about a specific session or the current one."),
		mcp.WithString("session", mcp.Description("Session name. Shows current if omitted.")),
	), handleSessionInfo(mgr))

	s.AddTool(mcp.NewTool("profiles_list",
		mcp.WithDescription("List available Chrome profiles for reuse."),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleProfiles(mgr))
}

func handleSessionList(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := mgr.Run(ctx, "", "session", "list")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(result.Text()), nil
	}
}

func handleSessionInfo(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		args := []string{"session"}
		result, err := mgr.Run(ctx, session, args...)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		var lines []string
		lines = append(lines, result.Text())
		lines = append(lines, "")

		r2, _ := mgr.Run(ctx, session, "get", "url")
		if r2 != nil && r2.Error == "" {
			lines = append(lines, "Current URL: "+r2.Text())
		}

		return mcp.NewToolResultText(strings.Join(lines, "\n")), nil
	}
}

func handleProfiles(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return runCmd(ctx, mgr, "", "profiles")
	}
}
