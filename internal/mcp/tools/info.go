package tools

import (
	"fmt"
	"slices"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/vercel-labs/agent-browser-mcp/internal/config"
)

func (r *Registry) registerInfo() {
	core := config.ToolsetCore

	r.add(core, mcp.NewTool("snapshot",
		mcp.WithDescription("Accessibility tree with @refs — the primary way to see page structure and get targets for click/fill. Cheaper than screenshot; scope with selector or interactive:true on big pages, and re-read with delta:true to get only what changed."),
		mcp.WithBoolean("interactive", mcp.Description("Only buttons, inputs, links.")),
		mcp.WithBoolean("compact", mcp.Description("Drop empty structural nodes. Default true.")),
		mcp.WithNumber("depth", mcp.Description("Limit tree depth.")),
		mcp.WithString("selector", mcp.Description("Scope to a CSS selector or @ref.")),
		mcp.WithBoolean("urls", mcp.Description("Include link hrefs.")),
		mcp.WithBoolean("delta", mcp.Description("Only @refs added/changed/removed since the last delta snapshot; full tree on the first call, after navigation or when options change.")),
		mcp.WithBoolean("full", mcp.Description("With delta: return the full tree and reset the baseline.")),
		sessionParam(), readOnly(),
	), r.cli(snapshotArgv))

	r.add(core, mcp.NewTool("page_text",
		mcp.WithDescription("Readable text of the page or one element. Prefer over snapshot when you only need to read content."),
		mcp.WithString("selector", mcp.Description("@ref or CSS selector. Default body.")),
		sessionParam(), readOnly(),
	), r.cli(pageTextArgv))

	r.add(core, mcp.NewTool("get",
		mcp.WithDescription("Read one property of an element or the page: text, html, value, attr, count, box, styles, visible/enabled/checked, or page title/url/cdp_url."),
		mcp.WithString("what", mcp.Required(), mcp.Enum(getWhats...)),
		selectorParam(false),
		mcp.WithString("attribute", mcp.Description("Attribute name for what=attr.")),
		sessionParam(), readOnly(),
	), r.cli(getArgv))
}

var getWhats = []string{"text", "html", "value", "attr", "title", "url", "count", "box", "styles", "cdp_url", "visible", "enabled", "checked"}

func snapshotArgv(req mcp.CallToolRequest) ([]string, error) {
	b := newArgv(req, "snapshot").boolFlag("-i", "interactive")
	if req.GetBool("compact", true) {
		b.add("-c")
	}
	b.intFlag("-d", "depth").flag("-s", "selector").boolFlag("--urls", "urls")
	if b.boolean("delta") || b.boolean("full") {
		b.add("--delta").boolFlag("--full", "full")
	}
	return b.done()
}

func pageTextArgv(req mcp.CallToolRequest) ([]string, error) {
	return []string{"get", "text", req.GetString("selector", "body")}, nil
}

func getArgv(req mcp.CallToolRequest) ([]string, error) {
	b := newArgv(req)
	switch what := b.enum("what", "", getWhats...); what {
	case "title", "url":
		b.add("get", what)
	case "cdp_url":
		b.add("get", "cdp-url")
	case "visible", "enabled", "checked":
		b.add("is", what, b.requiredFor("selector", "what="+what))
	case "attr":
		b.add("get", "attr", b.requiredFor("selector", "what=attr"), b.requiredFor("attribute", "what=attr"))
	default:
		b.add("get", what, b.requiredFor("selector", "what="+what))
	}
	return b.done()
}

func (r *Registry) registerFind() {
	r.add(config.ToolsetCore, mcp.NewTool("find",
		mcp.WithDescription("Locate an element semantically (role, text, label, placeholder, alt, title, testid, first/last/nth CSS match) and act on it in one call. Use when you know what the element says but have no @ref; by=all returns text of every CSS match."),
		mcp.WithString("by", mcp.Required(), mcp.Enum(findBys...)),
		mcp.WithString("value", mcp.Required(), mcp.Description("Role, text, label, placeholder, alt, title, test id, or CSS selector for first/last/nth/all.")),
		mcp.WithString("action", mcp.Enum(findActions...), mcp.Description("Default click. text returns the element's text.")),
		mcp.WithString("input", mcp.Description("Text for fill/type.")),
		mcp.WithString("name", mcp.Description("Accessible name filter for by=role.")),
		mcp.WithBoolean("exact", mcp.Description("Exact text match.")),
		mcp.WithNumber("index", mcp.Description("0-based index for by=nth.")),
		snapshotParam(), sessionParam(), mutating(),
	), r.action(findArgv))
}

var (
	findBys     = []string{"role", "text", "label", "placeholder", "alt", "title", "testid", "first", "last", "nth", "all"}
	findActions = []string{"click", "fill", "type", "hover", "focus", "check", "uncheck", "text"}
	exactBys    = []string{"role", "text", "label", "placeholder", "alt", "title"}
)

func findArgv(req mcp.CallToolRequest) ([]string, error) {
	b := newArgv(req)
	by := b.enum("by", "", findBys...)
	value := b.required("value")
	if by == "all" {
		// The CLI has no "find all"; read every match's text in one eval.
		return b.add("eval", fmt.Sprintf("Array.from(document.querySelectorAll(%s), e => e.innerText.trim())", jsString(value))).done()
	}
	action := b.enum("action", "click", findActions...)

	b.add("find", by)
	if by == "nth" {
		b.add(b.indexArg())
	}
	b.add(value, action)
	if action == "fill" || action == "type" {
		b.add(b.inputArg(action))
	}
	if by == "role" {
		b.flag("--name", "name")
	}
	if slices.Contains(exactBys, by) {
		b.boolFlag("--exact", "exact")
	}
	return b.done()
}

func (b *argv) indexArg() string {
	if !b.has("index") {
		b.fail(fmt.Errorf("index is required for by=nth"))
	}
	return b.int("index")
}

func (b *argv) inputArg(action string) string {
	if !b.has("input") {
		b.fail(fmt.Errorf("input is required for action %s", action))
	}
	return b.str("input")
}

// jsString quotes s as a JavaScript string literal.
func jsString(s string) string {
	return compactJSON(s)
}
