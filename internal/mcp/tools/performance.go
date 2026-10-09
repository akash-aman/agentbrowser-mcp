package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/vercel-labs/agent-browser-mcp/internal/cdp"
	"github.com/vercel-labs/agent-browser-mcp/internal/config"
	"github.com/vercel-labs/agent-browser-mcp/internal/devtools"
)

func (r *Registry) registerPerformance() {
	r.add(config.ToolsetDevtools, mcp.NewTool("performance",
		mcp.WithDescription("Measure and profile the page: Web Vitals, navigation timing, live runtime metrics, memory and heap snapshots, JS/CSS coverage, Lighthouse audits, traces or CPU profiles (summarized on stop) and a main-thread activity heatmap of the last recording. Start with vitals or metrics; lighthouse is thorough but takes 20-60s, and a trace and a CPU profile cannot record at the same time."),
		mcp.WithString("action", mcp.Required(), mcp.Enum(performanceActions...)),
		mcp.WithString("url", mcp.Description("vitals/lighthouse: URL to load. Default current page.")),
		mcp.WithString("path", mcp.Description("trace_stop/profiler_stop/heap_snapshot/lighthouse: output file. Default temp dir. heatmap: recording to read; default the last one.")),
		mcp.WithNumber("bucketMs", mcp.Description("heatmap: column width in ms. Default fits 60 columns.")),
		mcp.WithBoolean("json", mcp.Description("heatmap: return the numbers as JSON, e.g. to draw a chart.")),
		mcp.WithNumber("limit", mcp.Description("timing/coverage_stop/heap_snapshot/profiler_stop/trace_stop: rows to show. Default 10.")),
		mcp.WithString("formFactor", mcp.Enum("mobile", "desktop"), mcp.Description("lighthouse: default mobile.")),
		mcp.WithArray("categories", mcp.WithStringItems(mcp.Enum(devtools.LighthouseCategories...)), mcp.Description("lighthouse: categories to run. Default all.")),
		sessionParam(), mutating(),
	), r.handlePerformance)
}

var performanceActions = []string{
	"vitals", "timing", "memory", "metrics", "heap_snapshot", "coverage_start", "coverage_stop", "lighthouse",
	"trace_start", "trace_stop", "profiler_start", "profiler_stop", "heatmap",
}

// timingScript reports navigation milestones and the slowest resources, read
// from the page's Performance API so no trace file is needed.
const timingScript = `(() => {
  const n = performance.getEntriesByType("navigation")[0] || {};
  const ms = v => Math.round(v || 0);
  const slowest = performance.getEntriesByType("resource")
    .map(e => ({ url: e.name.length > 120 ? e.name.slice(0, 117) + "…" : e.name, type: e.initiatorType, ms: ms(e.duration), kb: Math.round((e.transferSize || 0) / 1024) }))
    .sort((a, b) => b.ms - a.ms).slice(0, %d);
  return {
    navigation: { ttfb: ms(n.responseStart), domContentLoaded: ms(n.domContentLoadedEventEnd), load: ms(n.loadEventEnd), transferKb: Math.round((n.transferSize || 0) / 1024) },
    resources: performance.getEntriesByType("resource").length,
    slowest,
  };
})()`

// memoryScript reports JS heap use (Chromium only) and DOM size.
const memoryScript = `(() => {
  const m = performance.memory || {};
  const mb = v => v ? Math.round(v / 1048576 * 10) / 10 : null;
  return { jsHeapUsedMb: mb(m.usedJSHeapSize), jsHeapTotalMb: mb(m.totalJSHeapSize), jsHeapLimitMb: mb(m.jsHeapSizeLimit), domNodes: document.getElementsByTagName("*").length, iframes: document.querySelectorAll("iframe").length };
})()`

// performanceArgv maps the actions the CLI handles; the rest use CDP.
func performanceArgv(req mcp.CallToolRequest) ([]string, error) {
	b := newArgv(req)
	switch b.enum("action", "", performanceActions...) {
	case "vitals":
		b.add("vitals").opt("url")
	case "timing":
		b.add("eval", fmt.Sprintf(timingScript, max(1, int(req.GetFloat("limit", 10)))))
	case "memory":
		b.add("eval", memoryScript)
	case "trace_start":
		b.add("trace", "start")
	case "trace_stop":
		b.add("trace", "stop").opt("path")
	case "profiler_start":
		b.add("profiler", "start")
	case "profiler_stop":
		b.add("profiler", "stop").opt("path")
	}
	return b.done()
}

type performanceAction func(r *Registry, ctx context.Context, req mcp.CallToolRequest, page *devtools.Page) (string, error)

var cdpPerformance = map[string]performanceAction{
	"metrics": func(_ *Registry, ctx context.Context, _ mcp.CallToolRequest, p *devtools.Page) (string, error) {
		return p.Metrics(ctx)
	},
	"heap_snapshot":  (*Registry).heapSnapshot,
	"coverage_start": coverageStart,
	"coverage_stop":  coverageStop,
	"lighthouse":     (*Registry).lighthouse,
}

func (r *Registry) handlePerformance(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, err := performanceArgv(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	action := req.GetString("action", "")
	if action == "heatmap" {
		return r.heatmap(req), nil
	}
	cdpAction, viaCDP := cdpPerformance[action]
	if !viaCDP {
		return r.runAndSummarize(ctx, req, action, args), nil
	}
	page, err := r.livePage(ctx, req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	text, err := cdpAction(r, ctx, req, page)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	return textResult(text, r.cfg.MaxOutput), nil
}

// runAndSummarize runs a CLI action and, for trace and profile files,
// appends a summary so the model does not need to open them.
func (r *Registry) runAndSummarize(ctx context.Context, req mcp.CallToolRequest, action string, args []string) *mcp.CallToolResult {
	summarize := map[string]func(*os.File, int) (string, error){
		"trace_stop":    func(f *os.File, n int) (string, error) { return devtools.SummarizeTrace(f, n) },
		"profiler_stop": func(f *os.File, n int) (string, error) { return devtools.SummarizeCPUProfile(f, n) },
	}[action]
	if summarize == nil {
		return r.run(ctx, req, args...)
	}
	res, err := r.mgr.Run(ctx, getSession(req), args...)
	if err != nil {
		return mcp.NewToolResultError(err.Error())
	}
	path := dataField(res.Data, "path")
	if path != "" {
		r.rememberRecording(getSession(req), path)
	}
	text := body(res)
	if f, err := os.Open(path); err == nil {
		defer f.Close()
		if summary, err := summarize(f, int(req.GetFloat("limit", 10))); err == nil {
			text = "file: " + path + "\n" + summary
		}
	}
	return textResult(text, r.cfg.MaxOutput)
}

// heatmap renders main-thread activity over time for a recording, by default
// the session's last trace or CPU profile.
func (r *Registry) heatmap(req mcp.CallToolRequest) *mcp.CallToolResult {
	path := req.GetString("path", "")
	if path == "" {
		path = r.lastRecording(getSession(req))
	}
	if path == "" {
		return mcp.NewToolResultError("no recording yet: run trace_start/trace_stop or profiler_start/profiler_stop first, or pass path")
	}
	f, err := os.Open(path)
	if err != nil {
		return mcp.NewToolResultError(err.Error())
	}
	defer f.Close()
	h, err := devtools.BuildHeatmap(f, req.GetFloat("bucketMs", 0))
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("%s: %v", path, err))
	}
	if req.GetBool("json", false) {
		return textResult(h.JSON(), r.cfg.MaxOutput)
	}
	return textResult("recording: "+path+"\n"+h.Text(), r.cfg.MaxOutput)
}

func (r *Registry) heapSnapshot(ctx context.Context, req mcp.CallToolRequest, page *devtools.Page) (string, error) {
	path := req.GetString("path", tempPath("heap", ".heapsnapshot"))
	s, err := page.HeapSnapshot(ctx, path, int(req.GetFloat("limit", 10)))
	if err != nil {
		return "", err
	}
	lines := []string{
		fmt.Sprintf("heap snapshot: %s (%s file, opens in Chrome DevTools > Memory)", s.Path, size(float64(s.FileBytes))),
		fmt.Sprintf("%d objects, %s self size, %d detached DOM nodes", s.Nodes, size(float64(s.TotalSelfSize)), s.DetachedDOMNodes),
		"Top by self size:",
	}
	for _, g := range s.Top {
		lines = append(lines, fmt.Sprintf("  %9s %7d  %s", size(float64(g.SelfSize)), g.Count, g.Name))
	}
	return strings.Join(lines, "\n"), nil
}

func coverageStart(_ *Registry, ctx context.Context, _ mcp.CallToolRequest, page *devtools.Page) (string, error) {
	if err := page.StartCoverage(ctx); err != nil {
		return "", err
	}
	return "recording JS and CSS coverage; interact with the page (or reload it), then call coverage_stop", nil
}

func coverageStop(_ *Registry, ctx context.Context, req mcp.CallToolRequest, page *devtools.Page) (string, error) {
	files, err := page.StopCoverage(ctx)
	if err != nil {
		return "", err
	}
	if len(files) == 0 {
		return "no scripts or stylesheets with a URL ran", nil
	}
	total, unused := 0, 0
	for _, f := range files {
		total += f.Total
		unused += f.Unused()
	}
	lines := []string{fmt.Sprintf("%s of %s unused (%.0f%%) across %d files; most unused first:",
		size(float64(unused)), size(float64(total)), percent(unused, total), len(files))}
	for _, f := range files[:min(int(req.GetFloat("limit", 10)), len(files))] {
		lines = append(lines, fmt.Sprintf("  %3s %9s unused of %9s (%3.0f%%)  %s",
			f.Kind, size(float64(f.Unused())), size(float64(f.Total)), percent(f.Unused(), f.Total), f.URL))
	}
	return strings.Join(lines, "\n"), nil
}

func (r *Registry) lighthouse(ctx context.Context, req mcp.CallToolRequest, page *devtools.Page) (string, error) {
	session := getSession(req)
	wsURL, err := r.dt.BrowserURL(ctx, session)
	if err != nil {
		return "", err
	}
	port, err := cdp.Port(wsURL)
	if err != nil {
		return "", err
	}
	url := req.GetString("url", "")
	if url == "" {
		if res, err := r.mgr.Run(ctx, session, "get", "url"); err == nil {
			url = dataField(res.Data, "url")
		}
	}
	if url == "" {
		return "", fmt.Errorf("url is required when no page is open")
	}
	ctx, cancel := context.WithTimeout(ctx, max(time.Duration(r.cfg.DefaultTimeout)*time.Millisecond, 3*time.Minute))
	defer cancel()
	return devtools.RunLighthouse(ctx, devtools.LighthouseOptions{
		Binary:     r.cfg.LighthousePath,
		URL:        url,
		Port:       port,
		OutputPath: req.GetString("path", tempPath("lighthouse", ".json")),
		FormFactor: req.GetString("formFactor", "mobile"),
		Categories: req.GetStringSlice("categories", nil),
	})
}

func tempPath(prefix, ext string) string {
	return filepath.Join(os.TempDir(), fmt.Sprintf("%s-%d%s", prefix, time.Now().UnixNano(), ext))
}

func size(bytes float64) string {
	switch {
	case bytes >= 1<<20:
		return fmt.Sprintf("%.1f MB", bytes/(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%.1f KB", bytes/(1<<10))
	}
	return fmt.Sprintf("%.0f B", bytes)
}

func percent(part, total int) float64 {
	if total == 0 {
		return 0
	}
	return 100 * float64(part) / float64(total)
}
