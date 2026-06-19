package tools

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/vercel-labs/agent-browser-mcp/internal/browser"
)

func registerPerformance(s *server.MCPServer, mgr *browser.Manager) {
	s.AddTool(mcp.NewTool("trace_start",
		mcp.WithDescription("Start recording a Chrome DevTools performance trace."),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleTraceStart(mgr))

	s.AddTool(mcp.NewTool("trace_stop",
		mcp.WithDescription("Stop recording the trace and save to a file. Returns the file path."),
		mcp.WithString("path", mcp.Description("Output file path. Saves to temp if omitted.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleTraceStop(mgr))

	s.AddTool(mcp.NewTool("profiler_start",
		mcp.WithDescription("Start Chrome DevTools JavaScript CPU profiling."),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleProfilerStart(mgr))

	s.AddTool(mcp.NewTool("profiler_stop",
		mcp.WithDescription("Stop profiling and save the .cpuprofile to a file."),
		mcp.WithString("path", mcp.Description("Output file path. Saves to temp if omitted.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleProfilerStop(mgr))
}

func handleTraceStart(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return runCmd(ctx, mgr, getSession(request), "trace", "start")
	}
}

func handleTraceStop(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		path := request.GetString("path", "")
		if path != "" {
			return runCmd(ctx, mgr, session, "trace", "stop", path)
		}
		return runCmd(ctx, mgr, session, "trace", "stop")
	}
}

func handleProfilerStart(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return runCmd(ctx, mgr, getSession(request), "profiler", "start")
	}
}

func handleProfilerStop(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		path := request.GetString("path", "")
		if path != "" {
			return runCmd(ctx, mgr, session, "profiler", "stop", path)
		}
		return runCmd(ctx, mgr, session, "profiler", "stop")
	}
}
