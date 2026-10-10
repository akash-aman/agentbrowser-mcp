package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/xcode-studio/agentbrowser-mcp/internal/config"
)

func (r *Registry) registerTabs() {
	r.add(config.ToolsetCore, mcp.NewTool("tabs",
		mcp.WithDescription("List, open, switch, or close tabs, or open a new window, which has its own cookies and storage (e.g. a second user). Tab ids (t1, t2) and labels are interchangeable."),
		mcp.WithString("action", mcp.Enum(tabActions...), mcp.Description("Default list.")),
		mcp.WithString("tab", mcp.Description("Tab id or label for switch/close. close defaults to the active tab.")),
		mcp.WithString("url", mcp.Description("URL for new and new_window.")),
		mcp.WithString("label", mcp.Description("Label for new, e.g. docs.")),
		sessionParam(), mutating(),
	), r.handleTabs)
}

// handleTabs lists tabs one per line, and opens new_window's URL, which the
// CLI's "window new" does not take.
func (r *Registry) handleTabs(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, err := tabsArgv(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	switch args[1] {
	case "list":
		out, err := r.mgr.Run(ctx, getSession(req), args...)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return textResult(tabLines(out.Data, r.tabTitles(ctx, req)), r.cfg.MaxOutput), nil
	case "new":
		if args[0] == "window" {
			res := r.run(ctx, req, args...)
			if url := req.GetString("url", ""); url != "" && !res.IsError {
				opened := r.run(ctx, req, "open", url)
				appendText(res, resultText(opened))
				res.IsError = opened.IsError
			}
			return res, nil
		}
	}
	return r.run(ctx, req, args...), nil
}

// tabTitles reads each tab's current title over CDP, by target id:
// agent-browser's tab list keeps the title a tab had before its page loaded.
func (r *Registry) tabTitles(ctx context.Context, req mcp.CallToolRequest) map[string]string {
	page := r.dt.Existing(getSession(req))
	if page == nil {
		var err error
		if page, err = r.dt.Page(ctx, getSession(req)); err != nil {
			return nil
		}
	}
	titles, _ := page.TabTitles(ctx)
	return titles
}

// tabLines renders tab list data as "* t1 [label] title  url", the active
// tab starred, with titles from titles where it has them. A title that is
// just the URL, as Chrome shows for a page without one, is left out.
func tabLines(data json.RawMessage, titles map[string]string) string {
	var d struct {
		Tabs []struct {
			Active   bool    `json:"active"`
			TabID    string  `json:"tabId"`
			TargetID string  `json:"targetId"`
			Label    *string `json:"label"`
			Title    string  `json:"title"`
			URL      string  `json:"url"`
		} `json:"tabs"`
	}
	if json.Unmarshal(data, &d) != nil || len(d.Tabs) == 0 {
		return formatData(data)
	}
	lines := make([]string, 0, len(d.Tabs))
	for _, t := range d.Tabs {
		mark := " "
		if t.Active {
			mark = "*"
		}
		label := ""
		if t.Label != nil && *t.Label != "" {
			label = " [" + *t.Label + "]"
		}
		title := t.Title
		if live, ok := titles[t.TargetID]; ok {
			title = live
		}
		_, bare, _ := strings.Cut(t.URL, "://")
		if title != "" && title != t.URL && title != bare {
			label += " " + title
		}
		lines = append(lines, fmt.Sprintf("%s %s%s  %s", mark, t.TabID, label, t.URL))
	}
	return strings.Join(lines, "\n")
}

var tabActions = []string{"list", "new", "switch", "close", "new_window"}

func tabsArgv(req mcp.CallToolRequest) ([]string, error) {
	b := newArgv(req)
	switch b.enum("action", "list", tabActions...) {
	case "new":
		b.add("tab", "new").flag("--label", "label").opt("url")
	case "switch":
		b.add("tab", b.requiredFor("tab", "switch"))
	case "close":
		b.add("tab", "close").opt("tab")
	case "new_window":
		b.add("window", "new")
	default:
		b.add("tab", "list")
	}
	return b.done()
}
