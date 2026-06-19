package tools

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/vercel-labs/agent-browser-mcp/internal/browser"
)

func registerNetwork(s *server.MCPServer, mgr *browser.Manager) {
	s.AddTool(mcp.NewTool("network_requests",
		mcp.WithDescription("View tracked network requests. Use filters to narrow results. CRUCIAL for non-vision models to trace API calls, request/response headers, and status codes."),
		mcp.WithString("filter", mcp.Description("Text filter (matches in URL).")),
		mcp.WithString("type", mcp.Description("Filter by resource type: xhr, fetch, document, script, stylesheet, image, etc. Comma-separated.")),
		mcp.WithString("method", mcp.Description("Filter by HTTP method: GET, POST, PUT, DELETE, PATCH.")),
		mcp.WithString("status", mcp.Description("Filter by status: e.g., 200, 2xx, 400, 400-499.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleNetworkRequests(mgr))

	s.AddTool(mcp.NewTool("network_request_detail",
		mcp.WithDescription("Get full request/response detail for a specific network request by its requestId (from network_requests). Shows headers, body, timing."),
		mcp.WithString("requestId", mcp.Required(), mcp.Description("The request ID from network_requests output.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleNetworkRequest(mgr))

	s.AddTool(mcp.NewTool("network_route",
		mcp.WithDescription("Intercept and optionally modify or block network requests. Use '*' for all URLs."),
		mcp.WithString("url", mcp.Required(), mcp.Description("URL pattern to intercept. Use '*' for all.")),
		mcp.WithBoolean("abort", mcp.Description("Block matching requests.")),
		mcp.WithString("body", mcp.Description("JSON body to respond with (mock).")),
		mcp.WithString("resourceType", mcp.Description("Only intercept specific resource types: script, stylesheet, image, etc.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleNetworkRoute(mgr))

	s.AddTool(mcp.NewTool("network_unroute",
		mcp.WithDescription("Remove network route/block rules. If no URL given, removes all."),
		mcp.WithString("url", mcp.Description("URL pattern to unroute. Removes all if omitted.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleNetworkUnroute(mgr))

	s.AddTool(mcp.NewTool("network_har_start",
		mcp.WithDescription("Start recording network traffic in HAR format for detailed analysis."),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleHARStart(mgr))

	s.AddTool(mcp.NewTool("network_har_stop",
		mcp.WithDescription("Stop HAR recording and save to a file. Output path shows where to find the HAR file."),
		mcp.WithString("path", mcp.Description("Output file path. Saves to temp if omitted.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleHARStop(mgr))
}

func handleNetworkRequests(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		args := []string{"network", "requests"}
		if filter := request.GetString("filter", ""); filter != "" {
			args = append(args, "--filter", filter)
		}
		if typ := request.GetString("type", ""); typ != "" {
			args = append(args, "--type", typ)
		}
		if method := request.GetString("method", ""); method != "" {
			args = append(args, "--method", method)
		}
		if status := request.GetString("status", ""); status != "" {
			args = append(args, "--status", status)
		}
		return runCmd(ctx, mgr, session, args...)
	}
}

func handleNetworkRequest(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		reqID, _ := request.RequireString("requestId")
		return runCmd(ctx, mgr, session, "network", "request", reqID)
	}
}

func handleNetworkRoute(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		url, _ := request.RequireString("url")
		args := []string{"network", "route", url}
		if request.GetBool("abort", false) {
			args = append(args, "--abort")
		}
		if body := request.GetString("body", ""); body != "" {
			args = append(args, "--body", body)
		}
		if rt := request.GetString("resourceType", ""); rt != "" {
			args = append(args, "--resource-type", rt)
		}
		return runCmd(ctx, mgr, session, args...)
	}
}

func handleNetworkUnroute(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		url := request.GetString("url", "")
		if url != "" {
			return runCmd(ctx, mgr, session, "network", "unroute", url)
		}
		return runCmd(ctx, mgr, session, "network", "unroute")
	}
}

func handleHARStart(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return runCmd(ctx, mgr, getSession(request), "network", "har", "start")
	}
}

func handleHARStop(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		path := request.GetString("path", "")
		if path != "" {
			return runCmd(ctx, mgr, session, "network", "har", "stop", path)
		}
		return runCmd(ctx, mgr, session, "network", "har", "stop")
	}
}
