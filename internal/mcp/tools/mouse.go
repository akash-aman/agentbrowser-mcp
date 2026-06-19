package tools

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/vercel-labs/agent-browser-mcp/internal/browser"
)

func registerMouse(s *server.MCPServer, mgr *browser.Manager) {
	s.AddTool(mcp.NewTool("mouse_move",
		mcp.WithDescription("Move the mouse to specific coordinates."),
		mcp.WithNumber("x", mcp.Required(), mcp.Description("X coordinate.")),
		mcp.WithNumber("y", mcp.Required(), mcp.Description("Y coordinate.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleMouseMove(mgr))

	s.AddTool(mcp.NewTool("mouse_down",
		mcp.WithDescription("Press a mouse button at the current position."),
		mcp.WithString("button", mcp.Description("Mouse button: 'left' (default), 'right', 'middle'.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleMouseDown(mgr))

	s.AddTool(mcp.NewTool("mouse_up",
		mcp.WithDescription("Release a mouse button."),
		mcp.WithString("button", mcp.Description("Mouse button: 'left' (default), 'right', 'middle'.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleMouseUp(mgr))

	s.AddTool(mcp.NewTool("mouse_wheel",
		mcp.WithDescription("Scroll the mouse wheel."),
		mcp.WithNumber("deltaY", mcp.Required(), mcp.Description("Vertical scroll delta.")),
		mcp.WithNumber("deltaX", mcp.Description("Horizontal scroll delta.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleMouseWheel(mgr))
}

func handleMouseMove(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		x := request.GetFloat("x", 0)
		y := request.GetFloat("y", 0)
		return runCmd(ctx, mgr, session, "mouse", "move", fmt.Sprintf("%d", int(x)), fmt.Sprintf("%d", int(y)))
	}
}

func handleMouseDown(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		button := request.GetString("button", "left")
		return runCmd(ctx, mgr, session, "mouse", "down", button)
	}
}

func handleMouseUp(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		button := request.GetString("button", "left")
		return runCmd(ctx, mgr, session, "mouse", "up", button)
	}
}

func handleMouseWheel(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		dy := request.GetFloat("deltaY", 0)
		dx := request.GetFloat("deltaX", 0)
		args := []string{"mouse", "wheel", fmt.Sprintf("%d", int(dy))}
		if dx != 0 {
			args = append(args, fmt.Sprintf("%d", int(dx)))
		}
		return runCmd(ctx, mgr, session, args...)
	}
}
