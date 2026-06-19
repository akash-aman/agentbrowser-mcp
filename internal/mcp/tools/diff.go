package tools

import (
	"context"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/vercel-labs/agent-browser-mcp/internal/browser"
)

func registerDiff(s *server.MCPServer, mgr *browser.Manager) {
	s.AddTool(mcp.NewTool("diff_snapshot",
		mcp.WithDescription("Compare the current snapshot with the last one or a saved baseline file. Shows added/removed/changed elements."),
		mcp.WithString("baseline", mcp.Description("Path to a baseline snapshot file. Compares against last snapshot if omitted.")),
		mcp.WithString("selector", mcp.Description("Scope diff to a CSS selector.")),
		mcp.WithBoolean("compact", mcp.Description("Compact diff output.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleDiffSnapshot(mgr))

	s.AddTool(mcp.NewTool("diff_screenshot",
		mcp.WithDescription("Visual pixel diff of the current screenshot against a saved baseline image."),
		mcp.WithString("baseline", mcp.Required(), mcp.Description("Path to baseline screenshot file.")),
		mcp.WithString("output", mcp.Description("Path to save the diff image.")),
		mcp.WithNumber("threshold", mcp.Description("Color difference threshold (0-1, default 0.1).")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleDiffScreenshot(mgr))

	s.AddTool(mcp.NewTool("diff_urls",
		mcp.WithDescription("Compare two URLs by taking snapshots of each and showing differences. Optionally also compare screenshots."),
		mcp.WithString("urlA", mcp.Required(), mcp.Description("First URL to compare.")),
		mcp.WithString("urlB", mcp.Required(), mcp.Description("Second URL to compare.")),
		mcp.WithBoolean("screenshot", mcp.Description("Also do a visual diff of screenshots.")),
		mcp.WithString("selector", mcp.Description("Scope snapshots to a CSS selector.")),
		mcp.WithString("waitUntil", mcp.Description("Wait strategy: load, domcontentloaded, networkidle.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleDiffURLs(mgr))
}

func handleDiffSnapshot(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		args := []string{"diff", "snapshot"}
		if baseline := request.GetString("baseline", ""); baseline != "" {
			args = append(args, "--baseline", baseline)
		}
		if sel := request.GetString("selector", ""); sel != "" {
			args = append(args, "--selector", sel)
		}
		if request.GetBool("compact", false) {
			args = append(args, "--compact")
		}
		return runCmd(ctx, mgr, session, args...)
	}
}

func handleDiffScreenshot(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		baseline, _ := request.RequireString("baseline")
		args := []string{"diff", "screenshot", "--baseline", baseline}
		if output := request.GetString("output", ""); output != "" {
			args = append(args, "-o", output)
		}
		if t := request.GetFloat("threshold", 0); t > 0 {
			args = append(args, "-t", fmt.Sprintf("%f", t))
		}
		return runCmd(ctx, mgr, session, args...)
	}
}

func handleDiffURLs(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		urlA, _ := request.RequireString("urlA")
		urlB, _ := request.RequireString("urlB")
		args := []string{"diff", "url", urlA, urlB}
		if request.GetBool("screenshot", false) {
			args = append(args, "--screenshot")
		}
		if sel := request.GetString("selector", ""); sel != "" {
			args = append(args, "--selector", sel)
		}
		if wait := request.GetString("waitUntil", ""); wait != "" {
			args = append(args, "--wait-until", wait)
		}
		return runCmd(ctx, mgr, session, args...)
	}
}
