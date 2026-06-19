package tools

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/vercel-labs/agent-browser-mcp/internal/browser"
)

func registerDialog(s *server.MCPServer, mgr *browser.Manager) {
	s.AddTool(mcp.NewTool("dialog_accept",
		mcp.WithDescription("Accept a JavaScript dialog (alert, confirm, prompt) with optional prompt text."),
		mcp.WithString("text", mcp.Description("Text to enter into a prompt dialog.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleDialogAccept(mgr))

	s.AddTool(mcp.NewTool("dialog_dismiss",
		mcp.WithDescription("Dismiss a JavaScript dialog."),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleDialogDismiss(mgr))

	s.AddTool(mcp.NewTool("dialog_status",
		mcp.WithDescription("Check if a JavaScript dialog is currently open. Returns dialog type and message."),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleDialogStatus(mgr))
}

func handleDialogAccept(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		text := request.GetString("text", "")
		if text != "" {
			return runCmd(ctx, mgr, session, "dialog", "accept", text)
		}
		return runCmd(ctx, mgr, session, "dialog", "accept")
	}
}

func handleDialogDismiss(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return runCmd(ctx, mgr, getSession(request), "dialog", "dismiss")
	}
}

func handleDialogStatus(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return runCmd(ctx, mgr, getSession(request), "dialog", "status")
	}
}
