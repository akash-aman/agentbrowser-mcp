package tools

import (
	"cmp"
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/vercel-labs/agent-browser-mcp/internal/config"
	"github.com/vercel-labs/agent-browser-mcp/internal/devtools"
)

func (r *Registry) registerEmulation() {
	r.add(config.ToolsetEmulation, mcp.NewTool("emulate",
		mcp.WithDescription("Set one or more emulation settings in one call: viewport, device, geolocation, offline, extra headers, HTTP auth, color scheme, reduced motion, user agent, and network/CPU throttling. Use it to check mobile and responsive layouts, dark mode, and the page on slow networks or CPUs."),
		mcp.WithNumber("width", mcp.Description("Viewport width (with height).")),
		mcp.WithNumber("height", mcp.Description("Viewport height (with width).")),
		mcp.WithNumber("scale", mcp.Description("Device pixel ratio, e.g. 2.")),
		mcp.WithString("device", mcp.Description("Device name, e.g. iPhone 14.")),
		mcp.WithNumber("latitude", mcp.Description("Geolocation latitude (with longitude).")),
		mcp.WithNumber("longitude", mcp.Description("Geolocation longitude.")),
		mcp.WithBoolean("offline", mcp.Description("Turn offline mode on or off.")),
		mcp.WithString("headers", mcp.Description("Extra HTTP headers as a JSON object string.")),
		mcp.WithString("username", mcp.Description("HTTP basic auth username (with password).")),
		mcp.WithString("password", mcp.Description("HTTP basic auth password.")),
		mcp.WithString("colorScheme", mcp.Enum("dark", "light", "no-preference")),
		mcp.WithBoolean("reducedMotion", mcp.Description("Prefer reduced motion.")),
		mcp.WithString("userAgent", mcp.Description("User-Agent; reloads the current page to apply it.")),
		mcp.WithString("networkProfile", mcp.Enum(devtools.NetworkProfileNames()...), mcp.Description("Network throttling preset for the current tab; none removes it.")),
		mcp.WithNumber("latencyMs", mcp.Description("Custom network latency (overrides the preset).")),
		mcp.WithNumber("downloadKbps", mcp.Description("Custom download limit in kbps.")),
		mcp.WithNumber("uploadKbps", mcp.Description("Custom upload limit in kbps.")),
		mcp.WithNumber("cpuSlowdown", mcp.Description("CPU slowdown factor: 1 none, 4 mid-tier mobile, 6 low-end.")),
		sessionParam(), mutating(),
	), r.handleEmulate)
}

// emulateSteps maps every supplied setting to a CLI call. userAgent is handled
// separately because it needs the current URL.
func emulateSteps(req mcp.CallToolRequest) ([][]string, error) {
	b := newArgv(req)
	var steps [][]string
	for _, step := range []func() []string{b.viewport, b.device, b.geolocation, b.offline, b.headers, b.credentials, b.media} {
		if s := step(); s != nil {
			steps = append(steps, s)
		}
	}
	if _, err := b.done(); err != nil {
		return nil, err
	}
	return steps, nil
}

// pair reports whether either key is set, failing unless both are.
func (b *argv) pair(first, second string) bool {
	if !b.has(first) && !b.has(second) {
		return false
	}
	if !b.has(first) || !b.has(second) {
		b.fail(fmt.Errorf("%s and %s must be set together", first, second))
	}
	return true
}

func (b *argv) float(key string) string {
	return strconv.FormatFloat(b.req.GetFloat(key, 0), 'f', -1, 64)
}

func (b *argv) viewport() []string {
	if !b.pair("width", "height") {
		return nil
	}
	step := []string{"set", "viewport", b.int("width"), b.int("height")}
	if b.has("scale") {
		step = append(step, b.float("scale"))
	}
	return step
}

func (b *argv) device() []string {
	if d := b.str("device"); d != "" {
		return []string{"set", "device", d}
	}
	return nil
}

func (b *argv) geolocation() []string {
	if !b.pair("latitude", "longitude") {
		return nil
	}
	return []string{"set", "geo", b.float("latitude"), b.float("longitude")}
}

func (b *argv) offline() []string {
	if !b.has("offline") {
		return nil
	}
	if b.boolean("offline") {
		return []string{"set", "offline", "on"}
	}
	return []string{"set", "offline", "off"}
}

func (b *argv) headers() []string {
	if h := b.str("headers"); h != "" {
		return []string{"set", "headers", h}
	}
	return nil
}

func (b *argv) credentials() []string {
	if !b.has("username") && !b.has("password") {
		return nil
	}
	return []string{"set", "credentials", b.required("username"), b.required("password")}
}

func (b *argv) media() []string {
	step := []string{"set", "media"}
	if scheme := b.str("colorScheme"); scheme != "" {
		step = append(step, b.enum("colorScheme", "", "dark", "light", "no-preference"))
	}
	if b.boolean("reducedMotion") {
		step = append(step, "reduced-motion")
	}
	if len(step) == 2 {
		return nil
	}
	return step
}

func (r *Registry) handleEmulate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	steps, err := emulateSteps(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	throttle, err := throttleArgs(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	ua := req.GetString("userAgent", "")
	if len(steps) == 0 && ua == "" && throttle == nil {
		return mcp.NewToolResultError("set at least one emulation setting"), nil
	}

	lines := make([]string, 0, len(steps)+2)
	for _, args := range steps {
		res := r.run(ctx, req, args...)
		if res.IsError {
			return res, nil
		}
		lines = append(lines, args[1]+": "+resultText(res))
	}
	if ua != "" {
		res := r.applyUserAgent(ctx, req, ua)
		if res.IsError {
			return res, nil
		}
		lines = append(lines, "userAgent: "+resultText(res))
	}
	if throttle != nil {
		text, err := r.applyThrottle(ctx, req, *throttle)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		lines = append(lines, text)
	}
	return mcp.NewToolResultText(strings.Join(lines, "\n")), nil
}

// throttleRequest is the throttling asked for in one emulate call.
type throttleRequest struct {
	network *devtools.Throttle
	cpu     float64
}

// throttleArgs reads the throttling params; nil means none were given.
func throttleArgs(req mcp.CallToolRequest) (*throttleRequest, error) {
	b := newArgv(req)
	var t throttleRequest
	if b.has("networkProfile") || b.has("latencyMs") || b.has("downloadKbps") || b.has("uploadKbps") {
		n := devtools.NetworkProfiles[b.enum("networkProfile", "none", devtools.NetworkProfileNames()...)]
		n.LatencyMs = req.GetFloat("latencyMs", n.LatencyMs)
		n.DownloadKbps = req.GetFloat("downloadKbps", n.DownloadKbps)
		n.UploadKbps = req.GetFloat("uploadKbps", n.UploadKbps)
		t.network = &n
	}
	if b.has("cpuSlowdown") {
		if t.cpu = req.GetFloat("cpuSlowdown", 1); t.cpu < 1 {
			b.fail(fmt.Errorf("cpuSlowdown must be >= 1, got %g", t.cpu))
		}
	}
	if _, err := b.done(); err != nil {
		return nil, err
	}
	if t.network == nil && t.cpu == 0 {
		return nil, nil
	}
	return &t, nil
}

// applyThrottle sets throttling over CDP. It lasts while this server runs and
// the tab stays open, because it is tied to the server's CDP session.
func (r *Registry) applyThrottle(ctx context.Context, req mcp.CallToolRequest, t throttleRequest) (string, error) {
	page, err := r.livePage(ctx, req)
	if err != nil {
		return "", err
	}
	if t.network != nil {
		if err := page.SetNetwork(ctx, *t.network); err != nil {
			return "", err
		}
	}
	if t.cpu > 0 {
		if err := page.SetCPU(ctx, t.cpu); err != nil {
			return "", err
		}
	}
	return "throttling: " + page.Throttle().String(), nil
}

// applyUserAgent reopens the current page with the new User-Agent, because the
// CLI only applies --user-agent when opening a page.
func (r *Registry) applyUserAgent(ctx context.Context, req mcp.CallToolRequest, ua string) *mcp.CallToolResult {
	url := "about:blank"
	if cur, err := r.mgr.Run(ctx, getSession(req), "get", "url"); err == nil {
		url = cmp.Or(dataField(cur.Data, "url"), url)
	}
	res := r.run(ctx, req, "--user-agent", ua, "open", url)
	if res.IsError {
		return res
	}
	return mcp.NewToolResultText("applied, reloaded " + url)
}
