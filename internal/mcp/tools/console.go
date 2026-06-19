package tools

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/vercel-labs/agent-browser-mcp/internal/browser"
)

func registerConsole(s *server.MCPServer, mgr *browser.Manager) {
	s.AddTool(mcp.NewTool("console_messages",
		mcp.WithDescription("View browser console messages (log, error, warn, info). Essential for debugging JavaScript output without visual inspection."),
		mcp.WithString("session", mcp.Description("Session name.")),
		mcp.WithBoolean("clear", mcp.Description("Clear the console buffer after reading.")),
	), handleConsole(mgr))

	s.AddTool(mcp.NewTool("page_errors",
		mcp.WithDescription("View uncaught JavaScript exceptions on the page. Use for detecting page-level errors."),
		mcp.WithString("session", mcp.Description("Session name.")),
		mcp.WithBoolean("clear", mcp.Description("Clear the error buffer after reading.")),
	), handleErrors(mgr))

	s.AddTool(mcp.NewTool("react_tree",
		mcp.WithDescription("View the full React component tree. Requires React DevTools hook (launch with --enable react-devtools). Works on any React app."),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleReactTree(mgr))

	s.AddTool(mcp.NewTool("react_inspect",
		mcp.WithDescription("Inspect a React component: props, hooks, state, and source location."),
		mcp.WithString("fiberId", mcp.Required(), mcp.Description("Fiber ID from react_tree output.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleReactInspect(mgr))

	s.AddTool(mcp.NewTool("react_renders",
		mcp.WithDescription("Profile React component renders. Start recording, then stop to see render counts and timing."),
		mcp.WithString("action", mcp.Required(), mcp.Description("'start' to begin recording or 'stop' to stop and view results.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleReactRenders(mgr))

	s.AddTool(mcp.NewTool("web_vitals",
		mcp.WithDescription("Get Web Vitals metrics (LCP, CLS, TTFB, FCP, INP) for the current page. Works on any website, not just React."),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleVitals(mgr))
}

func handleConsole(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		result, err := mgr.Run(ctx, session, "console")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		if request.GetBool("clear", false) {
			mgr.Run(ctx, session, "console", "--clear")
		}

		return mcp.NewToolResultText(result.Text()), nil
	}
}

func handleErrors(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		result, err := mgr.Run(ctx, session, "errors")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}

		if request.GetBool("clear", false) {
			mgr.Run(ctx, session, "errors", "--clear")
		}

		return mcp.NewToolResultText(result.Text()), nil
	}
}

func handleReactTree(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		result, err := mgr.Run(ctx, getSession(request), "react", "tree")
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		if result.Error != "" {
			return mcp.NewToolResultText(fmt.Sprintf("React DevTools may not be enabled. Error: %s\n\nTip: Use --enable react-devtools flag to enable React introspection.", result.Error)), nil
		}
		return mcp.NewToolResultText(result.Text()), nil
	}
}

func handleReactInspect(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		fiberID, _ := request.RequireString("fiberId")
		return runCmd(ctx, mgr, getSession(request), "react", "inspect", fiberID)
	}
}

func handleReactRenders(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		action, _ := request.RequireString("action")
		switch action {
		case "start":
			return runCmd(ctx, mgr, session, "react", "renders", "start")
		case "stop":
			return runCmd(ctx, mgr, session, "react", "renders", "stop")
		default:
			return mcp.NewToolResultError("action must be 'start' or 'stop'"), nil
		}
	}
}

func handleVitals(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return runCmd(ctx, mgr, getSession(request), "vitals", "--json")
	}
}
