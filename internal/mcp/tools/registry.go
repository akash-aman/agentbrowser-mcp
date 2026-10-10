// Package tools registers all agent-browser MCP tool handlers.
package tools

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/vercel-labs/agent-browser-mcp/internal/browser"
	"github.com/vercel-labs/agent-browser-mcp/internal/config"
	"github.com/vercel-labs/agent-browser-mcp/internal/devtools"
)

// Registry holds every registered tool and its handler so batch can dispatch
// to them in-process.
type Registry struct {
	cfg *config.Config
	mgr *browser.Manager
	dt  *devtools.Pool
	// pausedWait is how long a command may wait on an already-paused page
	// before the tool returns and lets it finish in the background.
	pausedWait time.Duration
	// pauseGrace is how long an action that finished while debugging still
	// watches for a pause: handlers often defer work to a timer or promise,
	// which reached its breakpoint just after the click returned.
	pauseGrace time.Duration
	// macEditing says whether editing shortcuts (Meta+a, Meta+c...) need
	// their commands sent: on macOS they come from the app menu, so key
	// events alone do nothing.
	macEditing bool
	// snapBase is the last accessibility tree seen per session and scope,
	// without refs, for snapshot diffs.
	snapBase map[string][]string
	// devtoolsSettle is how long open_devtools watches the page width for
	// DevTools docking beside it.
	devtoolsSettle time.Duration
	// healthSettle is how long navigate waits after the load before checking
	// for JS errors and failed requests, so errors thrown just after load
	// are counted.
	healthSettle time.Duration
	// waitHiddenFor bounds wait for=hidden, like the CLI's own waits.
	waitHiddenFor time.Duration

	mu         sync.Mutex
	recordings map[string]string   // session -> last trace or CPU profile file
	heapSnaps  map[string][]string // session -> heap snapshot files, oldest first
	flows      map[string]*flow    // session -> flow being recorded
	tools      []server.ServerTool
	toolsets   map[string]string
	handlers   map[string]server.ToolHandlerFunc
}

// RegisterAll builds the registry for the enabled toolsets and adds it to s.
func RegisterAll(s *server.MCPServer, cfg *config.Config, mgr *browser.Manager) *Registry {
	r := NewRegistry(cfg, mgr)
	s.AddTools(r.tools...)
	return r
}

// NewRegistry builds the registry without attaching it to a server.
func NewRegistry(cfg *config.Config, mgr *browser.Manager) *Registry {
	r := &Registry{
		cfg:            cfg,
		mgr:            mgr,
		dt:             devtools.NewPool(mgr),
		pausedWait:     3 * time.Second,
		pauseGrace:     300 * time.Millisecond,
		macEditing:     runtime.GOOS == "darwin",
		snapBase:       map[string][]string{},
		devtoolsSettle: 1500 * time.Millisecond,
		healthSettle:   300 * time.Millisecond,
		waitHiddenFor:  25 * time.Second,
		recordings:     map[string]string{},
		heapSnaps:      map[string][]string{},
		flows:          map[string]*flow{},
		toolsets:       make(map[string]string),
		handlers:       make(map[string]server.ToolHandlerFunc),
	}
	r.registerCore()
	r.registerInfo()
	r.registerFind()
	r.registerWait()
	r.registerCapture()
	r.registerMouse()
	r.registerTabs()
	r.registerDialog()
	r.registerConsole()
	r.registerBatch()
	r.registerHelp()
	r.registerNetwork()
	r.registerPerformance()
	r.registerDevtools()
	r.registerEmulation()
	r.registerStorage()
	r.registerSession()
	r.registerDebugger()
	r.registerApplication()
	r.registerElements()
	r.registerCDP()
	return r
}

func (r *Registry) rememberRecording(session, path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.recordings[r.mgr.ResolveSession(session)] = path
}

// heapSnapshots returns the session's heap snapshot files, oldest first.
func (r *Registry) heapSnapshots(session string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.heapSnaps[r.mgr.ResolveSession(session)]...)
}

func (r *Registry) rememberHeapSnapshot(session, path string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := r.mgr.ResolveSession(session)
	r.heapSnaps[key] = append(r.heapSnaps[key], path)
}

func (r *Registry) lastRecording(session string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.recordings[r.mgr.ResolveSession(session)]
}

// Close releases CDP connections held for throttling, coverage and debugging.
func (r *Registry) Close() {
	r.dt.Close()
}

// Tools returns the registered tools in registration order.
func (r *Registry) Tools() []server.ServerTool {
	return r.tools
}

// Toolset returns the toolset a registered tool belongs to.
func (r *Registry) Toolset(name string) string {
	return r.toolsets[name]
}

// Handler returns the handler for a registered tool.
func (r *Registry) Handler(name string) (server.ToolHandlerFunc, bool) {
	h, ok := r.handlers[name]
	return h, ok
}

func (r *Registry) add(toolset string, tool mcp.Tool, h server.ToolHandlerFunc) {
	if !r.cfg.HasToolset(toolset) {
		return
	}
	h = r.noteRestart(r.recordFlow(tool.Name, h))
	r.tools = append(r.tools, server.ServerTool{Tool: tool, Handler: h})
	r.toolsets[tool.Name] = toolset
	r.handlers[tool.Name] = h
}

// noteRestart tells the model when the call found the session's browser
// gone, e.g. closed after the idle timeout, and ran in a fresh one.
func (r *Registry) noteRestart(h server.ToolHandlerFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		res, err := h(ctx, req)
		if res != nil && r.mgr.Restarted(getSession(req)) {
			res.Content = append([]mcp.Content{mcp.NewTextContent(restartNote(r.cfg.IdleTimeout))}, res.Content...)
		}
		return res, err
	}
}

// Annotation presets. mcp.NewTool defaults every tool to destructive, so each
// tool sets one of these explicitly.
func readOnly() mcp.ToolOption {
	return mcp.WithToolAnnotation(mcp.ToolAnnotation{ReadOnlyHint: mcp.ToBoolPtr(true)})
}

func mutating() mcp.ToolOption {
	return mcp.WithToolAnnotation(mcp.ToolAnnotation{DestructiveHint: mcp.ToBoolPtr(false)})
}

func destructive() mcp.ToolOption {
	return mcp.WithToolAnnotation(mcp.ToolAnnotation{DestructiveHint: mcp.ToBoolPtr(true)})
}

// Shared parameters.
func sessionParam() mcp.ToolOption {
	return mcp.WithString("session", mcp.Description("Omit to use the current browser; a new name opens another window."))
}

func selectorParam(required bool) mcp.ToolOption {
	opts := []mcp.PropertyOption{mcp.Description("@ref from snapshot (preferred) or CSS selector.")}
	if required {
		opts = append(opts, mcp.Required())
	}
	return mcp.WithString("selector", opts...)
}

func humanParam() mcp.ToolOption {
	return mcp.WithBoolean("human", mcp.Description("Approach along a human-like pointer curve (hover effects, bot checks)."))
}

func snapshotParam() mcp.ToolOption {
	return mcp.WithString("snapshot", mcp.Enum("none", "delta", "diff", "full"),
		mcp.Description("After acting, also return: delta = changed @refs (cheapest), diff = line diff, full = interactive snapshot. Default none."))
}

// argvFunc maps a tool request to agent-browser arguments.
type argvFunc func(req mcp.CallToolRequest) ([]string, error)

// cli returns a handler that runs the argv from fn and formats the result.
func (r *Registry) cli(fn argvFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args, err := fn(req)
		if err != nil {
			return mcp.NewToolResultError(err.Error()), nil
		}
		return r.run(ctx, req, args...), nil
	}
}

// action is cli plus the optional snapshot follow-up for tools that change the page.
func (r *Registry) action(fn argvFunc) server.ToolHandlerFunc {
	return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		res, _ := r.cli(fn)(ctx, req)
		if res.IsError {
			return res, nil
		}
		r.appendSnapshot(ctx, req, res)
		return res, nil
	}
}

// appendSnapshot adds the page state requested by the snapshot param.
func (r *Registry) appendSnapshot(ctx context.Context, req mcp.CallToolRequest, res *mcp.CallToolResult) {
	switch req.GetString("snapshot", "none") {
	case "delta":
		out, err := r.mgr.Run(ctx, getSession(req), "snapshot", "-i", "-c", "--delta")
		if err != nil {
			appendText(res, "snapshot delta failed: "+err.Error())
			return
		}
		appendText(res, truncate(formatData(out.Data), r.cfg.MaxOutput))
	case "diff":
		text, err := r.diffSnapshot(ctx, req, "")
		if err != nil {
			appendText(res, "snapshot diff failed: "+err.Error())
			return
		}
		appendText(res, "Changes:\n"+truncate(text, r.cfg.MaxOutput))
	case "full":
		out, err := r.mgr.Run(ctx, getSession(req), "snapshot", "-i", "-c")
		if err != nil {
			appendText(res, "snapshot failed: "+err.Error())
			return
		}
		appendText(res, "Snapshot:\n"+truncate(formatData(out.Data), r.cfg.MaxOutput))
	}
}

// run executes args and returns a formatted, size-capped result. CLI
// failures become tool errors so the model can react to them.
func (r *Registry) run(ctx context.Context, req mcp.CallToolRequest, args ...string) *mcp.CallToolResult {
	if page := r.dt.Existing(getSession(req)); page != nil && page.DebuggerOn() {
		return r.runWhileDebugging(ctx, req, page, args)
	}
	return r.runCLI(ctx, req, args...)
}

// runWhileDebugging returns as soon as the page pauses: a paused page blocks
// the command (e.g. a click whose handler hit a breakpoint) until the debugger
// resumes, so the command finishes in the background instead.
func (r *Registry) runWhileDebugging(ctx context.Context, req mcp.CallToolRequest, page *devtools.Page, args []string) *mcp.CallToolResult {
	next, stop := page.NextPause()
	defer stop()
	background := context.WithoutCancel(ctx)
	done := make(chan *mcp.CallToolResult, 1)
	go func() { done <- r.runCLI(background, req, args...) }()

	var stillPaused <-chan time.Time
	if page.Paused() != nil {
		stillPaused = time.After(r.pausedWait)
	}
	select {
	case res := <-done:
		select {
		case pause := <-next:
			appendText(res, "Then the page paused:\n"+page.Describe(background, pause))
		case <-time.After(r.pauseGrace):
		case <-ctx.Done():
		}
		return res
	case <-ctx.Done(): // the call was cancelled or the server is stopping
		return mcp.NewToolResultError(fmt.Sprintf("%s was cancelled: %v", args[0], ctx.Err()))
	case pause := <-next:
		return mcp.NewToolResultText(fmt.Sprintf("%s\n(%s finishes after you resume with the debugger tool)", page.Describe(background, pause), args[0]))
	case <-stillPaused:
		return mcp.NewToolResultText(fmt.Sprintf("The page is paused in the debugger, so %s is waiting; it finishes after you resume.", args[0]))
	}
}

// livePage returns the session's CDP page for features other than the
// debugger, failing fast while the page is paused at a breakpoint.
func (r *Registry) livePage(ctx context.Context, req mcp.CallToolRequest) (*devtools.Page, error) {
	page, err := r.dt.Page(ctx, getSession(req))
	if err != nil {
		return nil, err
	}
	if page.Paused() != nil {
		return nil, devtools.ErrPaused
	}
	return page, nil
}

// runCLI runs args through agent-browser and formats the result.
func (r *Registry) runCLI(ctx context.Context, req mcp.CallToolRequest, args ...string) *mcp.CallToolResult {
	res, err := r.mgr.Run(ctx, getSession(req), args...)
	if err != nil {
		return mcp.NewToolResultError(err.Error())
	}
	return textResult(body(res), r.cfg.MaxOutput)
}

// body renders a CLI result: formatted data for JSON output, raw text otherwise.
func body(res *browser.Result) string {
	if !res.JSON {
		if text := strings.TrimSpace(res.RawStdout); text != "" {
			return text
		}
		return "ok"
	}
	return formatData(res.Data)
}

// getSession extracts the optional "session" parameter from a request.
func getSession(req mcp.CallToolRequest) string {
	return req.GetString("session", "")
}
