package tools

import (
	"context"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/vercel-labs/agent-browser-mcp/internal/config"
)

func (r *Registry) registerStorage() {
	st := config.ToolsetStorage

	r.add(st, mcp.NewTool("cookies",
		mcp.WithDescription("List, set, clear, or import cookies (import reads a JSON, cURL or Cookie-header file)."),
		mcp.WithString("action", mcp.Enum("list", "set", "clear", "import"), mcp.Description("Default list.")),
		mcp.WithString("name", mcp.Description("set: cookie name.")),
		mcp.WithString("value", mcp.Description("set: cookie value.")),
		mcp.WithString("url", mcp.Description("set: URL the cookie belongs to; lets you set it before loading the page.")),
		mcp.WithString("domain", mcp.Description("set/import: cookie domain.")),
		mcp.WithString("path", mcp.Description("set: cookie path.")),
		mcp.WithBoolean("httpOnly", mcp.Description("set: HttpOnly.")),
		mcp.WithBoolean("secure", mcp.Description("set: Secure.")),
		mcp.WithString("sameSite", mcp.Enum("Strict", "Lax", "None")),
		mcp.WithNumber("expires", mcp.Description("set: Unix timestamp in seconds.")),
		mcp.WithString("source", mcp.Description("import: file path.")),
		sessionParam(), destructive(),
	), r.handleCookies)

	r.add(st, mcp.NewTool("storage",
		mcp.WithDescription("Read, set, or clear localStorage or sessionStorage for the current page."),
		mcp.WithString("area", mcp.Enum("local", "session"), mcp.Description("Default local.")),
		mcp.WithString("action", mcp.Enum("get", "set", "clear"), mcp.Description("Default get.")),
		mcp.WithString("key", mcp.Description("get: one key (default all). set: key.")),
		mcp.WithString("value", mcp.Description("set: value.")),
		sessionParam(), destructive(),
	), r.cli(storageArgv))

	r.add(st, mcp.NewTool("state",
		mcp.WithDescription("Save or load cookies + storage to a file to reuse logins, or list, show, rename, clear, or clean up old saved states. load visits each saved origin to restore its storage, so take a new snapshot after it."),
		mcp.WithString("action", mcp.Required(), mcp.Enum(stateActions...)),
		mcp.WithString("path", mcp.Description("save/load: file path. show/rename: state file name.")),
		mcp.WithString("newName", mcp.Description("rename: new name.")),
		mcp.WithBoolean("all", mcp.Description("clear: every saved state, not just this session's.")),
		mcp.WithNumber("olderThanDays", mcp.Description("clean: delete states older than this many days.")),
		sessionParam(), destructive(),
	), r.handleState)

	r.add(st, mcp.NewTool("clipboard",
		mcp.WithDescription("Read or write the browser clipboard, or copy/paste the current selection."),
		mcp.WithString("action", mcp.Required(), mcp.Enum("read", "write", "copy", "paste")),
		mcp.WithString("text", mcp.Description("write: text.")),
		sessionParam(), mutating(),
	), r.handleClipboard)
}

// handleClipboard grants clipboard permission and makes the page act focused
// for the call: the clipboard API waits for focus, which a background window
// never gets (writes hung until they timed out), and a fresh profile denies
// it (headless). Copy and paste run Chrome's editing commands over CDP; the
// CLI's reported success but left the clipboard and the field unchanged.
func (r *Registry) handleClipboard(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if _, err := clipboardArgv(req); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	page, err := r.livePage(ctx, req)
	if err == nil {
		page.GrantClipboard(ctx)
		if restore, err := page.EnsureFocused(ctx); err == nil {
			defer restore()
		}
	}
	if action := req.GetString("action", ""); action == "copy" || action == "paste" {
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		text, err := page.EditCommand(ctx, action)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return mcp.NewToolResultText(text), nil
	}
	return r.cli(clipboardArgv)(ctx, req)
}

// handleCookies imports a "Cookie: a=1; b=2" header line as the cookies it
// names: agent-browser parses the bare value, a cURL command or JSON, but
// kept the header name in the first cookie ("Cookie: a").
func (r *Registry) handleCookies(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	source := req.GetString("source", "")
	if req.GetString("action", "") != "import" || source == "" {
		return r.cli(cookiesArgv)(ctx, req)
	}
	data, err := os.ReadFile(source)
	text := strings.TrimSpace(string(data))
	if err != nil || len(text) < 7 || !strings.EqualFold(text[:7], "cookie:") {
		return r.cli(cookiesArgv)(ctx, req)
	}
	f, err := os.CreateTemp("", "agent-browser-mcp-cookies-*.txt")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	defer os.Remove(f.Name())
	_, err = f.WriteString(strings.TrimSpace(text[7:]))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	args := maps.Clone(req.GetArguments())
	args["source"] = f.Name()
	req.Params.Arguments = args
	return r.cli(cookiesArgv)(ctx, req)
}

// handleState renames saved states itself: agent-browser 0.38's "state
// rename" fails with "Missing 'path' parameter" even for a file that exists.
func (r *Registry) handleState(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if req.GetString("action", "") != "rename" {
		return r.cli(stateArgv)(ctx, req)
	}
	args, err := stateArgv(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	out, err := r.mgr.Run(ctx, getSession(req), "state", "list")
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	dir := dataField(out.Data, "directory")
	if dir == "" {
		return mcp.NewToolResultError("agent-browser did not say where saved states are kept"), nil
	}
	name := func(s string) string { return strings.TrimSuffix(filepath.Base(s), ".json") + ".json" }
	from, to := filepath.Join(dir, name(args[2])), filepath.Join(dir, name(args[3]))
	if _, err := os.Stat(from); err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("no saved state %s in %s (state list shows them)", name(args[2]), dir)), nil
	}
	if _, err := os.Stat(to); err == nil {
		return mcp.NewToolResultError(fmt.Sprintf("a saved state %s already exists", name(args[3]))), nil
	}
	if err := os.Rename(from, to); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return mcp.NewToolResultText(fmt.Sprintf("renamed %s to %s in %s", name(args[2]), name(args[3]), dir)), nil
}

var stateActions = []string{"save", "load", "list", "show", "rename", "clear", "clean"}

func cookiesArgv(req mcp.CallToolRequest) ([]string, error) {
	b := newArgv(req, "cookies")
	switch b.enum("action", "list", "list", "set", "clear", "import") {
	case "clear":
		b.add("clear")
	case "import":
		b.add("set", "--curl", b.requiredFor("source", "import")).flag("--domain", "domain")
	case "set":
		b.add("set", b.requiredFor("name", "set"), b.str("value")).
			flag("--url", "url").flag("--domain", "domain").flag("--path", "path").
			flag("--sameSite", "sameSite").boolFlag("--httpOnly", "httpOnly").boolFlag("--secure", "secure")
		if b.has("expires") {
			b.add("--expires", b.int("expires"))
		}
	default:
		b.add("get")
	}
	return b.done()
}

func storageArgv(req mcp.CallToolRequest) ([]string, error) {
	b := newArgv(req, "storage")
	b.add(b.enum("area", "local", "local", "session"))
	switch b.enum("action", "get", "get", "set", "clear") {
	case "clear":
		b.add("clear")
	case "set":
		b.add("set", b.requiredFor("key", "set"), b.str("value"))
	default:
		if b.str("key") != "" {
			b.add("get").opt("key")
		}
	}
	return b.done()
}

func stateArgv(req mcp.CallToolRequest) ([]string, error) {
	b := newArgv(req, "state")
	switch action := b.enum("action", "", stateActions...); action {
	case "list":
		b.add("list")
	case "clear":
		b.add("clear").boolFlag("--all", "all")
	case "clean":
		if !b.has("olderThanDays") {
			b.fail(errRequired("olderThanDays", "clean"))
		}
		b.add("clean", "--older-than", b.int("olderThanDays"))
	case "rename":
		b.add("rename", b.requiredFor("path", action), b.requiredFor("newName", action))
	default:
		b.add(action, b.requiredFor("path", action))
	}
	return b.done()
}

func clipboardArgv(req mcp.CallToolRequest) ([]string, error) {
	b := newArgv(req, "clipboard")
	action := b.enum("action", "", "read", "write", "copy", "paste")
	b.add(action)
	if action == "write" {
		if !b.has("text") {
			b.fail(errRequired("text", "write"))
		}
		b.add(b.str("text"))
	}
	return b.done()
}
