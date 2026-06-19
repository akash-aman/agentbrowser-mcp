// Package tools registers all agent-browser MCP tool handlers.
package tools

import (
	"github.com/mark3labs/mcp-go/server"

	"github.com/vercel-labs/agent-browser-mcp/internal/browser"
	"github.com/vercel-labs/agent-browser-mcp/internal/config"
)

func RegisterAll(s *server.MCPServer, cfg *config.Config, mgr *browser.Manager) {
	registerCore(s, mgr)
	registerInfo(s, mgr)
	registerFind(s, mgr)
	registerWait(s, mgr)
	registerCapture(s, mgr)
	registerNavigation(s, mgr)
	registerDialog(s, mgr)
	registerTab(s, mgr)
	registerNetwork(s, mgr)
	registerConsole(s, mgr)
	registerPerformance(s, mgr)
	registerEmulation(s, mgr)
	registerCookies(s, mgr)
	registerStorage(s, mgr)
	registerSession(s, mgr)
	registerState(s, mgr)
	registerMouse(s, mgr)
	registerClipboard(s, mgr)
	registerKeyboard(s, mgr)
	registerSecurity(s, mgr)
	registerDiff(s, mgr)
	registerHelp(s, mgr)
}
