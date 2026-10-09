package tools

import (
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
	), r.cli(cookiesArgv))

	r.add(st, mcp.NewTool("storage",
		mcp.WithDescription("Read, set, or clear localStorage or sessionStorage for the current page."),
		mcp.WithString("area", mcp.Enum("local", "session"), mcp.Description("Default local.")),
		mcp.WithString("action", mcp.Enum("get", "set", "clear"), mcp.Description("Default get.")),
		mcp.WithString("key", mcp.Description("get: one key (default all). set: key.")),
		mcp.WithString("value", mcp.Description("set: value.")),
		sessionParam(), destructive(),
	), r.cli(storageArgv))

	r.add(st, mcp.NewTool("state",
		mcp.WithDescription("Save or load cookies + storage to a file to reuse logins, or list, show, rename, clear, or clean up old saved states."),
		mcp.WithString("action", mcp.Required(), mcp.Enum(stateActions...)),
		mcp.WithString("path", mcp.Description("save/load: file path. show/rename: state file name.")),
		mcp.WithString("newName", mcp.Description("rename: new name.")),
		mcp.WithBoolean("all", mcp.Description("clear: every saved state, not just this session's.")),
		mcp.WithNumber("olderThanDays", mcp.Description("clean: delete states older than this many days.")),
		sessionParam(), destructive(),
	), r.cli(stateArgv))

	r.add(st, mcp.NewTool("clipboard",
		mcp.WithDescription("Read or write the browser clipboard, or copy/paste the current selection."),
		mcp.WithString("action", mcp.Required(), mcp.Enum("read", "write", "copy", "paste")),
		mcp.WithString("text", mcp.Description("write: text.")),
		sessionParam(), mutating(),
	), r.cli(clipboardArgv))
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
