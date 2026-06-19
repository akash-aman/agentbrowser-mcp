package tools

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/vercel-labs/agent-browser-mcp/internal/browser"
)

func registerSecurity(s *server.MCPServer, mgr *browser.Manager) {
	s.AddTool(mcp.NewTool("stream_enable",
		mcp.WithDescription("Enable WebSocket streaming of the browser viewport for live preview or pair browsing."),
		mcp.WithNumber("port", mcp.Description("Port to bind the WebSocket server.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleStreamEnable(mgr))

	s.AddTool(mcp.NewTool("stream_status",
		mcp.WithDescription("Show the current streaming state and bound port."),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleStreamStatus(mgr))

	s.AddTool(mcp.NewTool("stream_disable",
		mcp.WithDescription("Disable WebSocket streaming for the session."),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleStreamDisable(mgr))

	s.AddTool(mcp.NewTool("highlight_element",
		mcp.WithDescription("Highlight an element with a visual overlay for debugging."),
		mcp.WithString("selector", mcp.Required(), mcp.Description("Element selector to highlight.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleHighlight(mgr))

	s.AddTool(mcp.NewTool("open_devtools",
		mcp.WithDescription("Open Chrome DevTools for the active page in a new window."),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleInspect(mgr))
}

func handleStreamEnable(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		args := []string{"stream", "enable"}
		if port := request.GetFloat("port", 0); port > 0 {
			args = append(args, "--port", intToStr(int(port)))
		}
		return runCmd(ctx, mgr, session, args...)
	}
}

func handleStreamStatus(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return runCmd(ctx, mgr, getSession(request), "stream", "status")
	}
}

func handleStreamDisable(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return runCmd(ctx, mgr, getSession(request), "stream", "disable")
	}
}

func handleHighlight(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		selector, _ := request.RequireString("selector")
		return runCmd(ctx, mgr, session, "highlight", selector)
	}
}

func handleInspect(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return runCmd(ctx, mgr, getSession(request), "inspect")
	}
}
