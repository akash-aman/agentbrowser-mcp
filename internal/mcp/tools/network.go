package tools

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/xcode-studio/agentbrowser-mcp/internal/config"
)

func (r *Registry) registerNetwork() {
	r.add(config.ToolsetNetwork, mcp.NewTool("network",
		mcp.WithDescription("Trace or mock network traffic: requests lists one line per request (id method status type url), detail shows headers and body, route mocks or blocks requests. capture then records each request's initiator and cookie decisions and WebSocket/EventSource messages, for initiator, cookies, curl/fetch (copy as), replay and search across responses. Use it when data is missing, an API call fails, a cookie is not sent or an image or script 404s (status:\"400-599\"); narrow with filter/type/method/status and limit."),
		mcp.WithString("action", mcp.Enum(networkActions...), mcp.Description("Default requests.")),
		mcp.WithString("filter", mcp.Description("requests/websockets: URL substring; initiator/cookies/curl/fetch/replay: picks the newest captured request whose URL contains it.")),
		mcp.WithString("type", mcp.Description("requests: resource types, e.g. xhr,fetch,document.")),
		mcp.WithString("method", mcp.Description("requests: HTTP method.")),
		mcp.WithString("status", mcp.Description("requests: status, e.g. 200, 2xx, 400-499.")),
		mcp.WithNumber("limit", mcp.Description("requests: at most the last N. Default 50.")),
		mcp.WithBoolean("clear", mcp.Description("requests: clear the log after reading.")),
		mcp.WithString("requestId", mcp.Description("detail/initiator/cookies/curl/fetch/replay: request id.")),
		mcp.WithString("query", mcp.Description("search: text to find in captured response bodies.")),
		mcp.WithString("url", mcp.Description("route/unroute: URL pattern, * for all.")),
		mcp.WithBoolean("abort", mcp.Description("route: block matching requests.")),
		mcp.WithString("body", mcp.Description("route: JSON body to respond with.")),
		mcp.WithString("resourceType", mcp.Description("route: only these resource types.")),
		mcp.WithString("path", mcp.Description("har_stop: output file.")),
		sessionParam(), mutating(),
	), r.handleNetwork)
}

var networkActions = []string{"requests", "detail", "route", "unroute", "har_start", "har_stop",
	"capture", "websockets", "initiator", "cookies", "curl", "fetch", "replay", "search"}

// captureActions are answered from the server's own CDP capture.
var captureActions = map[string]bool{"capture": true, "websockets": true, "initiator": true, "cookies": true, "curl": true, "fetch": true, "replay": true, "search": true}

func networkArgv(req mcp.CallToolRequest) ([]string, error) {
	b := newArgv(req, "network")
	switch b.enum("action", "requests", networkActions...) {
	case "detail":
		b.add("request", b.requiredFor("requestId", "detail"))
	case "route":
		b.add("route", b.requiredFor("url", "route")).
			boolFlag("--abort", "abort").flag("--body", "body").flag("--resource-type", "resourceType")
	case "unroute":
		b.add("unroute").opt("url")
	case "har_start":
		b.add("har", "start")
	case "har_stop":
		b.add("har", "stop").opt("path")
	default:
		b.add("requests").flag("--filter", "filter").flag("--type", "type").
			flag("--method", "method").flag("--status", "status")
	}
	return b.done()
}

type networkData struct {
	Requests []struct {
		RequestID    string `json:"requestId"`
		Method       string `json:"method"`
		Status       any    `json:"status"`
		ResourceType string `json:"resourceType"`
		URL          string `json:"url"`
	} `json:"requests"`
}

func (r *Registry) handleNetwork(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	if action := req.GetString("action", ""); captureActions[action] {
		return r.handleCapture(ctx, req, action), nil
	}
	args, err := networkArgv(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	if args[1] == "request" {
		out, err := r.mgr.Run(ctx, getSession(req), args...)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return textResult(requestDetail(out.Data), r.cfg.MaxOutput), nil
	}
	if args[1] != "requests" {
		return r.run(ctx, req, args...), nil
	}

	out, err := r.mgr.Run(ctx, getSession(req), args...)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	var data networkData
	if err := json.Unmarshal(out.Data, &data); err != nil {
		return textResult(formatData(out.Data), r.cfg.MaxOutput), nil
	}
	filter := lineFilter{limit: int(req.GetFloat("limit", 50)), noun: "requests"}
	text := filter.apply(data.lines())
	if req.GetBool("clear", false) {
		text += r.clearBuffer(ctx, req, "network", "requests", "--clear")
	}
	return textResult(text, r.cfg.MaxOutput), nil
}

// lines renders one request per line: id method status type url.
func (d networkData) lines() []string {
	lines := make([]string, 0, len(d.Requests))
	for _, q := range d.Requests {
		status := "pending"
		if q.Status != nil {
			status = fmt.Sprint(q.Status)
		}
		lines = append(lines, fmt.Sprintf("%s %s %s %s %s", q.RequestID, q.Method, status, q.ResourceType, q.URL))
	}
	return lines
}

// handleCapture answers from the CDP capture, starting it on first use.
func (r *Registry) handleCapture(ctx context.Context, req mcp.CallToolRequest, action string) *mcp.CallToolResult {
	b := newArgv(req)
	which := cmp.Or(b.str("requestId"), b.str("filter"))
	switch action {
	case "initiator", "cookies", "curl", "fetch", "replay":
		if which == "" {
			b.fail(fmt.Errorf("requestId or filter is required for %s", action))
		}
	case "search":
		b.requiredFor("query", action)
	}
	if _, err := b.done(); err != nil {
		return mcp.NewToolResultError(err.Error())
	}
	page, err := r.livePage(ctx, req)
	if err != nil {
		return mcp.NewToolResultError(err.Error())
	}
	started, err := page.StartCapture(ctx)
	if err != nil {
		return mcp.NewToolResultError(err.Error())
	}
	if action == "capture" {
		if !started {
			return mcp.NewToolResultText("already capturing")
		}
		return mcp.NewToolResultText("capturing request initiators, cookie decisions and WebSocket/EventSource messages from now on, until the browser closes or this server restarts; reload to capture the page load")
	}
	var text string
	limit := max(1, int(req.GetFloat("limit", 50)))
	switch action {
	case "websockets":
		text = page.SocketMessages(b.str("filter"), limit)
	case "initiator":
		text, err = page.Initiator(which)
	case "cookies":
		text, err = page.RequestCookies(which)
	case "curl", "fetch":
		text, err = page.CopyRequest(which, action)
	case "replay":
		text, err = page.ReplayRequest(ctx, which)
	case "search":
		text, err = page.SearchResponses(ctx, b.str("query"), min(limit, 20))
	}
	if err != nil {
		if started {
			err = fmt.Errorf("%w (capture started just now)", err)
		}
		return mcp.NewToolResultError(err.Error())
	}
	return textResult(text, r.cfg.MaxOutput)
}

// requestDetail renders one request as its line, headers and bodies.
func requestDetail(data json.RawMessage) string {
	var d struct {
		URL             string            `json:"url"`
		Method          string            `json:"method"`
		Status          any               `json:"status"`
		ResourceType    string            `json:"resourceType"`
		MimeType        string            `json:"mimeType"`
		Headers         map[string]string `json:"headers"`
		PostData        string            `json:"postData"`
		ResponseHeaders map[string]string `json:"responseHeaders"`
		ResponseBody    string            `json:"responseBody"`
	}
	if json.Unmarshal(data, &d) != nil || d.URL == "" {
		return formatData(data)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s → %v (%s, %s)", d.Method, d.URL, cmp.Or[any](d.Status, "pending"), d.ResourceType, cmp.Or(d.MimeType, "no type"))
	headers := func(title string, h map[string]string) {
		if len(h) == 0 {
			return
		}
		keys := make([]string, 0, len(h))
		for k := range h {
			keys = append(keys, k)
		}
		slices.Sort(keys)
		b.WriteString("\n" + title + ":")
		for _, k := range keys {
			b.WriteString("\n  " + k + ": " + h[k])
		}
	}
	headers("request headers", d.Headers)
	if d.PostData != "" {
		b.WriteString("\nrequest body: " + truncate(d.PostData, 2000))
	}
	headers("response headers", d.ResponseHeaders)
	if d.ResponseBody != "" {
		b.WriteString("\nresponse body: " + truncate(d.ResponseBody, 4000))
	}
	return b.String()
}
