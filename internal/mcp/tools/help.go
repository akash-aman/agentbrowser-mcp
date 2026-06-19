package tools

import (
	"context"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/vercel-labs/agent-browser-mcp/internal/browser"
)

func registerHelp(s *server.MCPServer, mgr *browser.Manager) {
	s.AddTool(mcp.NewTool("help_discover",
		mcp.WithDescription("Discover agent-browser CLI commands, subcommands, and flags by running --help. Use this to find exact flag names or available subcommands. Pass a command path like 'network' or 'tab new' to see subcommand help."),
		mcp.WithString("command", mcp.Description("Command path to inspect (e.g., 'network', 'tab new', 'find'). Leave empty for root help.")),
	), handleHelpDiscover(mgr))

	s.AddTool(mcp.NewTool("help_doctor",
		mcp.WithDescription("Diagnose the agent-browser installation. Checks environment, Chrome install, daemon state, config. Also available with --fix to auto-repair."),
		mcp.WithBoolean("fix", mcp.Description("Run destructive repairs if issues found.")),
	), handleDoctor(mgr))

	s.AddTool(mcp.NewTool("help_skills",
		mcp.WithDescription("List or retrieve bundled agent-browser skill content. Skills provide AI agents with current instructions for specific tasks (core usage, Electron apps, Slack, exploratory testing, cloud browsers)."),
		mcp.WithString("name", mcp.Description("Skill name to retrieve (e.g., 'core'). Leave empty to list all skills.")),
		mcp.WithBoolean("full", mcp.Description("Include full command reference and templates.")),
	), handleSkills(mgr))
}

func handleHelpDiscover(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		cmd := request.GetString("command", "")
		args := []string{"--help"}
		if cmd != "" {
			// agent-browser <command> --help shows subcommand-specific help
			// First try: agent-browser --help will show top-level; we need subcommand help
			// For subcommand help: agent-browser <subcommand> --help
			parts := strings.Fields(cmd)
			args = append(parts, "--help")
		}

		result, err := mgr.Run(ctx, "", args...)
		if err != nil {
			// agent-browser --help returns non-zero sometimes, just capture the output
			if result != nil && result.RawStdout != "" {
				return mcp.NewToolResultText(result.RawStdout), nil
			}
			if result != nil && result.RawStderr != "" {
				return mcp.NewToolResultText(result.RawStderr), nil
			}
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(result.Text()), nil
	}
}

func handleDoctor(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		if request.GetBool("fix", false) {
			return runCmd(ctx, mgr, "", "doctor", "--fix")
		}
		return runCmd(ctx, mgr, "", "doctor")
	}
}

func handleSkills(mgr *browser.Manager) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		name := request.GetString("name", "")
		if name == "" {
			return runCmd(ctx, mgr, "", "skills", "list")
		}
		args := []string{"skills", "get", name}
		if request.GetBool("full", false) {
			args = append(args, "--full")
		}
		return runCmd(ctx, mgr, "", args...)
	}
}
