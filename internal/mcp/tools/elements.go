package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/xcode-studio/agentbrowser-mcp/internal/config"
	"github.com/xcode-studio/agentbrowser-mcp/internal/devtools"
)

var elementsActions = []string{
	"styles", "computed", "box", "a11y", "contrast", "force_state",
	"set_style", "set_attribute", "set_html", "remove", "hide",
	"overlay", "overlay_off", "selector", "search", "css_overview",
}

func (r *Registry) registerElements() {
	r.add(config.ToolsetDevtools, mcp.NewTool("elements",
		mcp.WithDescription("DevTools Elements panel for one element: the CSS rules that apply and which are overridden (styles), computed values, box model, accessibility name and role, text contrast against WCAG, forced :hover/:focus states, grid/flex overlays, selectors for tests, and live edits that last until reload. Use it when something looks wrong: misplaced, hidden, unstyled, unreadable or inaccessible. search finds elements by text, CSS selector or XPath, inside shadow DOM too; css_overview summarizes the page's colors, fonts and media queries."),
		mcp.WithString("action", mcp.Required(), mcp.Enum(elementsActions...)),
		selectorParam(false),
		mcp.WithArray("properties", mcp.WithStringItems(), mcp.Description("computed: property names or prefixes such as font. Default the common layout and text ones.")),
		mcp.WithArray("states", mcp.WithStringItems(mcp.Enum(devtools.PseudoStates...)), mcp.Description("force_state: pseudo-classes to hold; empty releases.")),
		mcp.WithString("name", mcp.Description("set_attribute: attribute name.")),
		mcp.WithString("value", mcp.Description("set_style: CSS declarations to add; set_attribute: value; set_html: new outer HTML.")),
		mcp.WithString("query", mcp.Description("search: text, CSS selector or XPath.")),
		mcp.WithNumber("limit", mcp.Description("search: max matches. Default 20.")),
		sessionParam(), mutating(),
	), r.handleElements)
}

// pageWide are the elements actions that do not act on one element.
var pageWide = map[string]bool{"search": true, "css_overview": true, "overlay_off": true}

func (r *Registry) handleElements(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	b := newArgv(req)
	action := b.enum("action", "", elementsActions...)
	switch action {
	case "set_attribute":
		b.requiredFor("name", action)
		b.provided("value")
	case "set_style", "set_html":
		b.requiredFor("value", action)
	case "search":
		b.requiredFor("query", action)
	}
	if !pageWide[action] && action != "" {
		b.requiredFor("selector", action)
	}
	if _, err := b.done(); err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	page, err := r.livePage(ctx, req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	text, err := r.elementsAction(ctx, req, b, page, action)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return textResult(text, r.cfg.MaxOutput), nil
}

func (r *Registry) elementsAction(ctx context.Context, req mcp.CallToolRequest, b *argv, page *devtools.Page, action string) (string, error) {
	switch action {
	case "search":
		return page.SearchDOM(ctx, b.str("query"), max(1, int(req.GetFloat("limit", 20))))
	case "css_overview":
		return page.CSSOverview(ctx)
	case "overlay_off":
		return page.OverlayOff(ctx)
	}
	target, err := r.elementTarget(ctx, req)
	if err != nil {
		return "", err
	}
	el, err := page.Element(ctx, target)
	if err != nil {
		return "", err
	}
	switch action {
	case "styles":
		return page.Styles(ctx, el)
	case "computed":
		return page.Computed(ctx, el, req.GetStringSlice("properties", nil))
	case "box":
		return page.BoxModel(ctx, el)
	case "a11y":
		return page.Accessibility(ctx, el)
	case "contrast":
		return page.Contrast(ctx, el)
	case "force_state":
		return page.ForceState(ctx, el, req.GetStringSlice("states", nil))
	case "overlay":
		return page.Overlay(ctx, el)
	case "selector":
		return page.Selector(ctx, el)
	}
	return page.Edit(ctx, el, action, b.str("name"), b.str("value"))
}

// elementTarget turns the selector param into a devtools target. An @ref is
// scrolled into view and its box looked up, because only agent-browser knows
// which element a ref names.
func (r *Registry) elementTarget(ctx context.Context, req mcp.CallToolRequest) (devtools.Target, error) {
	sel := req.GetString("selector", "")
	if !strings.HasPrefix(sel, "@") {
		return devtools.Target{Selector: sel, Label: sel}, nil
	}
	session := getSession(req)
	if _, err := r.mgr.Run(ctx, session, "scrollintoview", sel); err != nil {
		return devtools.Target{}, err
	}
	out, err := r.mgr.Run(ctx, session, "get", "box", sel)
	if err != nil {
		return devtools.Target{}, err
	}
	var box devtools.Rect
	if err := json.Unmarshal(out.Data, &box); err != nil {
		return devtools.Target{}, fmt.Errorf("agent-browser get box: unexpected output %s", out.Data)
	}
	if box.Width <= 0 || box.Height <= 0 {
		return devtools.Target{}, fmt.Errorf("%s has no box: it is hidden or not rendered", sel)
	}
	return devtools.Target{Box: &box, Label: sel}, nil
}
