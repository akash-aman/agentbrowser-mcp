package tools

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/vercel-labs/agent-browser-mcp/internal/browser"
)

func registerCapture(s *server.MCPServer, mgr *browser.Manager) {
	s.AddTool(mcp.NewTool("take_screenshot",
		mcp.WithDescription("Take a screenshot of the page or a specific element. Use --full for full page, --annotate for numbered labels that match snapshot refs. Screenshots are saved to the system temp dir if no path given."),
		mcp.WithString("path", mcp.Description("File path to save screenshot. Saves to temp dir if omitted.")),
		mcp.WithBoolean("fullPage", mcp.Description("Take a screenshot of the full page instead of viewport.")),
		mcp.WithBoolean("annotate", mcp.Description("Overlay numbered labels on interactive elements that correspond to snapshot refs.")),
		mcp.WithString("selector", mcp.Description("CSS selector to screenshot a specific element.")),
		mcp.WithString("format", mcp.Description("Image format: 'png' (default), 'jpeg', 'webp'.")),
		mcp.WithNumber("quality", mcp.Description("JPEG/WebP quality 0-100.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handleScreenshot(mgr))

	s.AddTool(mcp.NewTool("save_pdf",
		mcp.WithDescription("Save the current page as a PDF file."),
		mcp.WithString("path", mcp.Required(), mcp.Description("File path to save the PDF.")),
		mcp.WithString("session", mcp.Description("Session name.")),
	), handlePDF(mgr))
}

func handleScreenshot(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		args := []string{"screenshot"}

		if request.GetBool("fullPage", false) {
			args = append(args, "--full")
		}
		if request.GetBool("annotate", false) {
			args = append(args, "--annotate")
		}
		if sel := request.GetString("selector", ""); sel != "" {
			args = append(args, "--selector", sel)
		}
		if fmt := request.GetString("format", ""); fmt != "" {
			args = append(args, "--screenshot-format", fmt)
		}
		if q := request.GetFloat("quality", 0); q > 0 {
			args = append(args, "--screenshot-quality", intToStr(int(q)))
		}
		if path := request.GetString("path", ""); path != "" {
			args = append(args, path)
		}

		return runCmd(ctx, mgr, session, args...)
	}
}

func handlePDF(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		session := getSession(request)
		path, _ := request.RequireString("path")
		return runCmd(ctx, mgr, session, "pdf", path)
	}
}
