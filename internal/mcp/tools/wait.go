package tools

import (
	"cmp"
	"fmt"
	"strconv"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/vercel-labs/agent-browser-mcp/internal/config"
)

func (r *Registry) registerWait() {
	r.add(config.ToolsetCore, mcp.NewTool("wait",
		mcp.WithDescription("Wait for an element, its disappearance, text, URL, load state, JS condition, or a download. Prefer these over for=time fixed sleeps."),
		mcp.WithString("for", mcp.Required(), mcp.Enum(waitFors...)),
		mcp.WithString("value", mcp.Description("Selector (element/hidden), text, URL glob, load state (load|domcontentloaded|networkidle, default load), JS expression, milliseconds, or download save path.")),
		sessionParam(), readOnly(),
	), r.cli(waitArgv))
}

var waitFors = []string{"element", "hidden", "text", "url", "load", "function", "time", "download"}

func waitArgv(req mcp.CallToolRequest) ([]string, error) {
	b := newArgv(req, "wait")
	switch kind := b.enum("for", "", waitFors...); kind {
	case "load":
		b.add("--load", cmp.Or(b.str("value"), "load"))
	case "download":
		b.add("--download").opt("value")
	case "element":
		b.add(b.requiredFor("value", "for=element"))
	case "hidden":
		b.add(b.requiredFor("value", "for=hidden"), "--state", "hidden")
	case "text", "url":
		b.add("--"+kind, b.requiredFor("value", "for="+kind))
	case "function":
		b.add("--fn", b.requiredFor("value", "for=function"))
	case "time":
		b.add(b.milliseconds())
	}
	return b.done()
}

func (b *argv) milliseconds() string {
	ms := b.requiredFor("value", "for=time")
	if _, err := strconv.Atoi(ms); err != nil && ms != "" {
		b.fail(fmt.Errorf("value must be milliseconds for for=time, got %q", ms))
	}
	return ms
}
