package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/vercel-labs/agent-browser-mcp/internal/config"
)

func (r *Registry) registerNetwork() {
	r.add(config.ToolsetNetwork, mcp.NewTool("network",
		mcp.WithDescription("Trace or mock network traffic: requests lists one line per request (id method status type url), detail shows headers and body for one id. Narrow with filter/type/method/status and limit."),
		mcp.WithString("action", mcp.Enum(networkActions...), mcp.Description("Default requests.")),
		mcp.WithString("filter", mcp.Description("requests: URL substring.")),
		mcp.WithString("type", mcp.Description("requests: resource types, e.g. xhr,fetch,document.")),
		mcp.WithString("method", mcp.Description("requests: HTTP method.")),
		mcp.WithString("status", mcp.Description("requests: status, e.g. 200, 2xx, 400-499.")),
		mcp.WithNumber("limit", mcp.Description("requests: at most the last N. Default 50.")),
		mcp.WithBoolean("clear", mcp.Description("requests: clear the log after reading.")),
		mcp.WithString("requestId", mcp.Description("detail: id from requests.")),
		mcp.WithString("url", mcp.Description("route/unroute: URL pattern, * for all.")),
		mcp.WithBoolean("abort", mcp.Description("route: block matching requests.")),
		mcp.WithString("body", mcp.Description("route: JSON body to respond with.")),
		mcp.WithString("resourceType", mcp.Description("route: only these resource types.")),
		mcp.WithString("path", mcp.Description("har_stop: output file.")),
		sessionParam(), mutating(),
	), r.handleNetwork)
}

var networkActions = []string{"requests", "detail", "route", "unroute", "har_start", "har_stop"}

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
	args, err := networkArgv(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
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
