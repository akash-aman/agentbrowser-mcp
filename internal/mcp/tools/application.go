package tools

import (
	"context"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/vercel-labs/agent-browser-mcp/internal/config"
	"github.com/vercel-labs/agent-browser-mcp/internal/devtools"
)

func (r *Registry) registerApplication() {
	r.add(config.ToolsetStorage, mcp.NewTool("application",
		mcp.WithDescription("DevTools Application panel for the page's origin: IndexedDB databases and records, Cache Storage, service workers, web app manifest, storage usage/quota, or clear all site data."),
		mcp.WithString("action", mcp.Required(), mcp.Enum(applicationActions...)),
		mcp.WithString("database", mcp.Description("indexeddb_read: database name.")),
		mcp.WithString("store", mcp.Description("indexeddb_read: object store name.")),
		mcp.WithNumber("skip", mcp.Description("indexeddb_read: records to skip.")),
		mcp.WithNumber("limit", mcp.Description("indexeddb_read/cache_storage: max entries. Default 20.")),
		sessionParam(), destructive(),
	), r.handleApplication)
}

var applicationActions = []string{"indexeddb", "indexeddb_read", "cache_storage", "service_workers", "manifest", "storage_usage", "clear_site_data"}

func (r *Registry) handleApplication(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	b := newArgv(req)
	action := b.enum("action", "", applicationActions...)
	if action == "indexeddb_read" {
		b.requiredFor("database", action)
		b.requiredFor("store", action)
	}
	if _, err := b.done(); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	page, err := r.livePage(ctx, req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	text, err := applicationAction(ctx, b, page, action)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return textResult(text, r.cfg.MaxOutput), nil
}

func applicationAction(ctx context.Context, b *argv, page *devtools.Page, action string) (string, error) {
	limit := int(b.req.GetFloat("limit", 20))
	switch action {
	case "indexeddb":
		return page.IndexedDB(ctx)
	case "indexeddb_read":
		return page.IndexedDBRead(ctx, b.str("database"), b.str("store"), int(b.req.GetFloat("skip", 0)), limit)
	case "cache_storage":
		return page.CacheStorage(ctx, limit)
	case "service_workers":
		return page.ServiceWorkers(ctx)
	case "manifest":
		return page.Manifest(ctx)
	case "storage_usage":
		return page.StorageUsage(ctx)
	}
	return page.ClearSiteData(ctx)
}
