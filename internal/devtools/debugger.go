package devtools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/xcode-studio/agentbrowser-mcp/internal/cdp"
)

// ErrNotPaused is returned by operations that need a paused page.
var ErrNotPaused = errors.New("the page is not paused; set a breakpoint and trigger it, or pause first")

// Location is a 1-based position in a script. Original, when set, is the
// same place in the file the author wrote, from the script's source map.
type Location struct {
	URL      string
	ScriptID string
	Line     int
	Column   int
	Original string
}

func (l Location) String() string {
	s := fmt.Sprintf("%s:%d:%d", l.URL, l.Line, l.Column)
	if l.Original != "" {
		s += " → " + l.Original
	}
	return s
}

// cdpLocation is CDP's 0-based location.
type cdpLocation struct {
	ScriptID     string `json:"scriptId"`
	LineNumber   int    `json:"lineNumber"`
	ColumnNumber int    `json:"columnNumber"`
}

// Breakpoint is a breakpoint set through this server.
type Breakpoint struct {
	ID        string
	Kind      string // line, dom, xhr, event
	Where     string
	Condition string
	Resolved  []Location
	nodeID    int
	domType   string
}

// Frame is one call frame of a paused stack.
type Frame struct {
	ID       string
	Function string
	Location Location
	Scopes   []FrameScope
}

// FrameScope is one scope in a frame's scope chain.
type FrameScope struct {
	Type   string
	Name   string
	Object RemoteObject
}

// Pause describes where and why the page stopped.
type Pause struct {
	Reason         string
	HitBreakpoints []string
	Frames         []Frame
	Data           *RemoteObject
	Async          []AsyncTrace // what scheduled this code: await, setTimeout, a promise…
}

// AsyncTrace is one async segment of a paused stack, newest first.
type AsyncTrace struct {
	Description string
	Frames      []Frame
}

type debuggerState struct {
	enabled     bool
	paused      *Pause
	waiters     []chan Pause
	scripts     map[string]Location // by script ID; Line holds the line count
	breakpoints map[string]*Breakpoint
	exceptions  string
	watches     []string
	unsubscribe []func()
	mapURLs     map[string]string     // script ID -> absolute source map URL
	starts      map[string][2]int     // script ID -> 0-based line and column where it starts in its resource, e.g. an inline <script>
	maps        map[string]*loadedMap // script ID -> source map, loaded on first use
	blackbox    []string              // patterns stepping skips
	csp         bool                  // pause on Trusted Types CSP violations
	// The page's main JavaScript context per frame, and each script's, to
	// forget a frame's scripts once it loads a new document: a reload left
	// the old script IDs behind, and "source" read one Chrome had dropped.
	frameContext  map[string]int
	scriptContext map[string]int
}

// forgetContext drops the scripts of a JavaScript context that is gone.
func (d *debuggerState) forgetContext(id int) {
	for script, ctx := range d.scriptContext {
		if ctx != id {
			continue
		}
		delete(d.scriptContext, script)
		delete(d.scripts, script)
		delete(d.mapURLs, script)
		delete(d.starts, script)
		delete(d.maps, script)
	}
}

// DebuggerOn reports whether the debugger is enabled on this page.
func (p *Page) DebuggerOn() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.dbg != nil && p.dbg.enabled
}

// EnableDebugger turns on the Debugger domain and starts tracking scripts and
// pauses. It is idempotent.
func (p *Page) EnableDebugger(ctx context.Context) error {
	p.mu.Lock()
	if p.dbg != nil && p.dbg.enabled {
		p.mu.Unlock()
		return nil
	}
	d := &debuggerState{scripts: map[string]Location{}, breakpoints: map[string]*Breakpoint{}, exceptions: "none",
		mapURLs: map[string]string{}, maps: map[string]*loadedMap{}, starts: map[string][2]int{},
		frameContext: map[string]int{}, scriptContext: map[string]int{}}
	p.dbg = d
	p.mu.Unlock()

	d.unsubscribe = []func(){
		p.onSession("Debugger.scriptParsed", p.scriptParsed),
		p.onSession("Debugger.paused", func(params json.RawMessage) {
			p.pausedEvent(params)
			go p.syncPausedBanner()
		}),
		p.onSession("Debugger.resumed", func(json.RawMessage) {
			p.setPaused(nil)
			go p.syncPausedBanner()
		}),
	}
	// The "Paused in debugger" banner needs the Overlay domain, which needs
	// DOM. They go first so a page that is already paused gets the banner too,
	// and a browser without them still gets the debugger.
	for _, m := range []string{"DOM.enable", "Overlay.enable"} {
		p.call(ctx, m, nil, nil)
	}
	if err := p.call(ctx, "Debugger.enable", map[string]any{"maxScriptsCacheSize": 100_000_000}, nil); err != nil {
		p.dropDebugger()
		return err
	}
	// Async stacks show what scheduled the paused code (await, timers,
	// promises), as DevTools does by default.
	p.call(ctx, "Debugger.setAsyncCallStackDepth", map[string]any{"maxDepth": 32}, nil)
	p.mu.Lock()
	d.enabled = true
	p.mu.Unlock()
	return nil
}

// DisableDebugger resumes the page, removes all breakpoints and turns the
// debugger off. Resuming first matters: if another client such as DevTools
// also has the debugger on, disabling alone leaves the page paused.
func (p *Page) DisableDebugger(ctx context.Context) error {
	if p.Paused() != nil {
		if err := p.call(ctx, "Debugger.resume", nil, nil); err != nil {
			return err
		}
	}
	for _, bp := range p.Breakpoints() {
		if bp.Kind != "line" {
			p.RemoveBreakpoint(ctx, bp.ID)
		}
	}
	err := p.call(ctx, "Debugger.disable", nil, nil)
	p.dropDebugger()
	return err
}

func (p *Page) dropDebugger() {
	p.mu.Lock()
	d := p.dbg
	p.dbg = nil
	p.mu.Unlock()
	if d != nil {
		for _, u := range d.unsubscribe {
			u()
		}
	}
}

// onSession subscribes to an event from this page's session only.
func (p *Page) onSession(method string, fn func(json.RawMessage)) func() {
	return p.conn.On(method, func(ev cdp.Event) {
		if ev.SessionID == p.sessionID {
			fn(ev.Params)
		}
	})
}

func (p *Page) scriptParsed(params json.RawMessage) {
	var e struct {
		ScriptID     string `json:"scriptId"`
		URL          string `json:"url"`
		StartLine    int    `json:"startLine"`
		StartColumn  int    `json:"startColumn"`
		EndLine      int    `json:"endLine"`
		SourceMapURL string `json:"sourceMapURL"`
		ContextID    int    `json:"executionContextId"`
		AuxData      struct {
			Type      string `json:"type"`
			FrameID   string `json:"frameId"`
			IsDefault bool   `json:"isDefault"`
		} `json:"executionContextAuxData"`
	}
	if json.Unmarshal(params, &e) != nil {
		return
	}
	if e.AuxData.Type == "isolated" {
		// Isolated worlds run extensions' and agent-browser's own scripts
		// (its Web Vitals input listeners), never the page's. DevTools
		// ignores them by default; a click event breakpoint stopped in them
		// before the page's handler. The read loop cannot wait for a reply.
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
			defer cancel()
			p.conn.Call(ctx, p.sessionID, "Debugger.setBlackboxedRanges", map[string]any{
				"scriptId": e.ScriptID, "positions": []map[string]int{{"lineNumber": 0, "columnNumber": 0}}}, nil)
		}()
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dbg != nil {
		if e.AuxData.IsDefault && e.AuxData.FrameID != "" {
			if old, ok := p.dbg.frameContext[e.AuxData.FrameID]; ok && old != e.ContextID {
				p.dbg.forgetContext(old)
			}
			p.dbg.frameContext[e.AuxData.FrameID] = e.ContextID
		}
		p.dbg.scriptContext[e.ScriptID] = e.ContextID
		p.dbg.scripts[e.ScriptID] = Location{URL: e.URL, ScriptID: e.ScriptID, Line: e.EndLine + 1}
		if e.StartLine > 0 || e.StartColumn > 0 {
			p.dbg.starts[e.ScriptID] = [2]int{e.StartLine, e.StartColumn}
		}
		if e.SourceMapURL != "" {
			p.dbg.mapURLs[e.ScriptID] = resolveSource(e.URL, "", e.SourceMapURL)
		}
	}
}

func (p *Page) pausedEvent(params json.RawMessage) {
	var e struct {
		Reason         string        `json:"reason"`
		HitBreakpoints []string      `json:"hitBreakpoints"`
		Data           *RemoteObject `json:"data"`
		CallFrames     []struct {
			CallFrameID  string      `json:"callFrameId"`
			FunctionName string      `json:"functionName"`
			URL          string      `json:"url"`
			Location     cdpLocation `json:"location"`
			ScopeChain   []struct {
				Type   string       `json:"type"`
				Name   string       `json:"name"`
				Object RemoteObject `json:"object"`
			} `json:"scopeChain"`
		} `json:"callFrames"`
		AsyncStackTrace *asyncTrace `json:"asyncStackTrace"`
	}
	if json.Unmarshal(params, &e) != nil {
		return
	}
	pause := Pause{Reason: e.Reason, HitBreakpoints: e.HitBreakpoints, Data: e.Data}
	for _, f := range e.CallFrames {
		frame := Frame{ID: f.CallFrameID, Function: f.FunctionName, Location: p.location(f.Location, f.URL)}
		for _, s := range f.ScopeChain {
			frame.Scopes = append(frame.Scopes, FrameScope{Type: s.Type, Name: s.Name, Object: s.Object})
		}
		pause.Frames = append(pause.Frames, frame)
	}
	for t := e.AsyncStackTrace; t != nil && len(pause.Async) < 8; t = t.Parent {
		seg := AsyncTrace{Description: t.Description}
		for _, f := range t.CallFrames {
			seg.Frames = append(seg.Frames, Frame{Function: f.FunctionName,
				Location: p.location(cdpLocation{ScriptID: f.ScriptID, LineNumber: f.LineNumber, ColumnNumber: f.ColumnNumber}, f.URL)})
		}
		pause.Async = append(pause.Async, seg)
	}
	p.setPaused(&pause)
}

// asyncTrace is CDP's Runtime.StackTrace for async parents.
type asyncTrace struct {
	Description string `json:"description"`
	CallFrames  []struct {
		FunctionName string `json:"functionName"`
		ScriptID     string `json:"scriptId"`
		URL          string `json:"url"`
		LineNumber   int    `json:"lineNumber"`
		ColumnNumber int    `json:"columnNumber"`
	} `json:"callFrames"`
	Parent *asyncTrace `json:"parent"`
}

// location converts a CDP location to a 1-based one with the script URL.
func (p *Page) location(l cdpLocation, url string) Location {
	if url == "" {
		p.mu.Lock()
		if p.dbg != nil {
			url = p.dbg.scripts[l.ScriptID].URL
		}
		p.mu.Unlock()
	}
	return Location{URL: url, ScriptID: l.ScriptID, Line: l.LineNumber + 1, Column: l.ColumnNumber + 1}
}

func (p *Page) setPaused(pause *Pause) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dbg == nil {
		return
	}
	p.dbg.paused = pause
	if pause == nil {
		return
	}
	for _, w := range p.dbg.waiters {
		w <- *pause
	}
	p.dbg.waiters = nil
}

// syncPausedBanner shows Chrome's "Paused in debugger" banner while the page
// is paused, as DevTools does, so someone watching the browser sees why the
// page stopped responding. Event handlers run on the connection's read loop
// and cannot wait for a reply, hence the goroutine; the lock keeps a late
// show from landing after the clear that followed it.
func (p *Page) syncPausedBanner() {
	p.bannerMu.Lock()
	defer p.bannerMu.Unlock()
	params := map[string]any{}
	if p.Paused() != nil {
		params["message"] = "Paused in debugger"
	}
	p.call(context.Background(), "Overlay.setPausedInDebuggerMessage", params, nil)
}

// Paused returns the current pause, or nil while running.
func (p *Page) Paused() *Pause {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dbg == nil || p.dbg.paused == nil {
		return nil
	}
	pause := *p.dbg.paused
	return &pause
}

// NextPause returns a channel that receives the next pause, and a function
// to stop waiting. Call it before triggering the code that should pause, so
// the pause cannot be missed.
func (p *Page) NextPause() (<-chan Pause, func()) {
	ch := make(chan Pause, 1)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dbg != nil {
		p.dbg.waiters = append(p.dbg.waiters, ch)
	}
	return ch, func() {
		p.mu.Lock()
		defer p.mu.Unlock()
		if p.dbg != nil {
			p.dbg.waiters = slices.DeleteFunc(p.dbg.waiters, func(w chan Pause) bool { return w == ch })
		}
	}
}

// WaitForPause returns the current pause, or waits up to timeout for one.
func (p *Page) WaitForPause(ctx context.Context, timeout time.Duration) (Pause, error) {
	next, stop := p.NextPause()
	defer stop()
	if pause := p.Paused(); pause != nil {
		return *pause, nil
	}
	select {
	case pause := <-next:
		return pause, nil
	case <-time.After(timeout):
		return Pause{}, fmt.Errorf("no pause within %v", timeout)
	case <-ctx.Done():
		return Pause{}, ctx.Err()
	}
}

// BreakpointSpec describes a line breakpoint or logpoint. Line is 1-based.
type BreakpointSpec struct {
	URL        string
	URLRegex   string
	Line       int
	Column     int
	Condition  string
	LogMessage string // JS expressions to console.log instead of pausing
}

// SetBreakpoint sets a line breakpoint (or logpoint) by URL; it also binds to
// scripts loaded later, e.g. after navigation.
func (p *Page) SetBreakpoint(ctx context.Context, spec BreakpointSpec) (Breakpoint, error) {
	if spec.Line < 1 {
		return Breakpoint{}, fmt.Errorf("line must be >= 1")
	}
	where, authored := spec.URL, spec.Line
	mapped := ""
	if gen, ok := p.authoredBreakpoint(ctx, spec); ok {
		mapped = fmt.Sprintf(" (in the bundle at %s:%d:%d)", gen.URL, gen.Line, gen.Column)
		spec.URL, spec.Line, spec.Column = gen.URL, gen.Line, gen.Column
	}
	params := map[string]any{"lineNumber": spec.Line - 1}
	if spec.URLRegex != "" {
		params["urlRegex"], where = spec.URLRegex, "/"+spec.URLRegex+"/"
	} else {
		params["url"] = spec.URL
	}
	if spec.Column > 0 {
		params["columnNumber"] = spec.Column - 1
	}
	condition := spec.Condition
	if spec.LogMessage != "" {
		// A condition that logs and returns false never pauses: a logpoint.
		condition = fmt.Sprintf("console.log(%s), false", spec.LogMessage)
	}
	if condition != "" {
		params["condition"] = condition
	}
	var res struct {
		BreakpointID string        `json:"breakpointId"`
		Locations    []cdpLocation `json:"locations"`
	}
	if err := p.call(ctx, "Debugger.setBreakpointByUrl", params, &res); err != nil {
		return Breakpoint{}, err
	}
	bp := &Breakpoint{ID: res.BreakpointID, Kind: "line", Where: fmt.Sprintf("%s:%d%s", where, authored, mapped), Condition: condition}
	for _, l := range res.Locations {
		bp.Resolved = append(bp.Resolved, p.withOriginal(ctx, p.location(l, "")))
	}
	p.addBreakpoint(bp)
	return *bp, nil
}

// DOMBreakpointTypes are the DOM changes that can pause the page.
var DOMBreakpointTypes = []string{"subtree-modified", "attribute-modified", "node-removed"}

// SetDOMBreakpoint pauses when the element matching selector changes.
func (p *Page) SetDOMBreakpoint(ctx context.Context, selector, kind string) (Breakpoint, error) {
	if !slices.Contains(DOMBreakpointTypes, kind) {
		return Breakpoint{}, fmt.Errorf("DOM breakpoint type must be one of %v", DOMBreakpointTypes)
	}
	nodeID, err := p.querySelector(ctx, selector)
	if err != nil {
		return Breakpoint{}, err
	}
	if err := p.call(ctx, "DOMDebugger.setDOMBreakpoint", map[string]any{"nodeId": nodeID, "type": kind}, nil); err != nil {
		return Breakpoint{}, err
	}
	bp := &Breakpoint{ID: "dom:" + kind + ":" + selector, Kind: "dom", Where: selector + " " + kind, nodeID: nodeID, domType: kind}
	p.addBreakpoint(bp)
	return *bp, nil
}

// SetXHRBreakpoint pauses when an XHR or fetch URL contains urlPart ("" = any).
func (p *Page) SetXHRBreakpoint(ctx context.Context, urlPart string) (Breakpoint, error) {
	if err := p.call(ctx, "DOMDebugger.setXHRBreakpoint", map[string]any{"url": urlPart}, nil); err != nil {
		return Breakpoint{}, err
	}
	bp := &Breakpoint{ID: "xhr:" + urlPart, Kind: "xhr", Where: "request URL contains " + fmt.Sprintf("%q", urlPart)}
	p.addBreakpoint(bp)
	return *bp, nil
}

// SetEventBreakpoint pauses when a listener for event (e.g. click) runs.
func (p *Page) SetEventBreakpoint(ctx context.Context, event string) (Breakpoint, error) {
	if err := p.call(ctx, "DOMDebugger.setEventListenerBreakpoint", map[string]any{"eventName": event}, nil); err != nil {
		return Breakpoint{}, err
	}
	bp := &Breakpoint{ID: "event:" + event, Kind: "event", Where: event + " listeners"}
	p.addBreakpoint(bp)
	return *bp, nil
}

func (p *Page) addBreakpoint(bp *Breakpoint) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dbg != nil {
		p.dbg.breakpoints[bp.ID] = bp
	}
}

// RemoveBreakpoint removes a breakpoint of any kind by ID.
func (p *Page) RemoveBreakpoint(ctx context.Context, id string) error {
	p.mu.Lock()
	var bp *Breakpoint
	if p.dbg != nil {
		bp = p.dbg.breakpoints[id]
	}
	p.mu.Unlock()
	if bp == nil {
		return fmt.Errorf("no breakpoint %q", id)
	}
	var err error
	switch bp.Kind {
	case "dom":
		err = p.call(ctx, "DOMDebugger.removeDOMBreakpoint", map[string]any{"nodeId": bp.nodeID, "type": bp.domType}, nil)
	case "xhr":
		err = p.call(ctx, "DOMDebugger.removeXHRBreakpoint", map[string]any{"url": strings.TrimPrefix(id, "xhr:")}, nil)
	case "event":
		err = p.call(ctx, "DOMDebugger.removeEventListenerBreakpoint", map[string]any{"eventName": strings.TrimPrefix(id, "event:")}, nil)
	case "csp":
		err = p.call(ctx, "DOMDebugger.setBreakOnCSPViolation", map[string]any{"violationTypes": []string{}}, nil)
	default:
		err = p.call(ctx, "Debugger.removeBreakpoint", map[string]any{"breakpointId": id}, nil)
	}
	if err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dbg != nil {
		delete(p.dbg.breakpoints, id)
	}
	return nil
}

// Breakpoints lists breakpoints set through this server, sorted by ID.
func (p *Page) Breakpoints() []Breakpoint {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dbg == nil {
		return nil
	}
	out := make([]Breakpoint, 0, len(p.dbg.breakpoints))
	for _, bp := range p.dbg.breakpoints {
		out = append(out, *bp)
	}
	slices.SortFunc(out, func(a, b Breakpoint) int { return strings.Compare(a.ID, b.ID) })
	return out
}

// CanPause reports whether something set on this page can stop it: a
// breakpoint of any kind, pausing on exceptions or a CSP breakpoint. The
// debugger being on alone (edit_source turns it on) cannot.
func (p *Page) CanPause() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dbg == nil || !p.dbg.enabled {
		return false
	}
	return p.dbg.paused != nil || len(p.dbg.breakpoints) > 0 || p.dbg.exceptions != "none" || p.dbg.csp
}

// ExceptionModes are the pause-on-exception settings.
var ExceptionModes = []string{"none", "uncaught", "caught", "all"}

// SetPauseOnExceptions sets when thrown exceptions pause the page.
func (p *Page) SetPauseOnExceptions(ctx context.Context, mode string) error {
	if !slices.Contains(ExceptionModes, mode) {
		return fmt.Errorf("exceptions must be one of %v", ExceptionModes)
	}
	if err := p.call(ctx, "Debugger.setPauseOnExceptions", map[string]any{"state": mode}, nil); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dbg != nil {
		p.dbg.exceptions = mode
	}
	return nil
}

// ExceptionMode returns the current pause-on-exception setting.
func (p *Page) ExceptionMode() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dbg == nil {
		return "none"
	}
	return p.dbg.exceptions
}

// Step runs one execution-control command: resume, pause, step_over,
// step_into or step_out.
func (p *Page) Step(ctx context.Context, action string) error {
	method, ok := map[string]string{
		"resume":    "Debugger.resume",
		"pause":     "Debugger.pause",
		"step_over": "Debugger.stepOver",
		"step_into": "Debugger.stepInto",
		"step_out":  "Debugger.stepOut",
	}[action]
	if !ok {
		return fmt.Errorf("unknown debugger step %q", action)
	}
	if action != "pause" && p.Paused() == nil {
		return ErrNotPaused
	}
	return p.call(ctx, method, nil, nil)
}

// ContinueTo resumes until execution reaches url:line (1-based).
func (p *Page) ContinueTo(ctx context.Context, url string, line int) error {
	if p.Paused() == nil {
		return ErrNotPaused
	}
	// An authored file (src/cart.ts) runs to where that line compiled to,
	// as a breakpoint on it does.
	if at, ok := p.authoredBreakpoint(ctx, BreakpointSpec{URL: url, Line: line}); ok {
		loc := map[string]any{"scriptId": at.ScriptID, "lineNumber": at.Line - 1, "columnNumber": at.Column - 1}
		return p.call(ctx, "Debugger.continueToLocation", map[string]any{"location": loc}, nil)
	}
	script, err := p.scriptByURL(url)
	if err != nil {
		return err
	}
	loc := map[string]any{"scriptId": script.ScriptID, "lineNumber": line - 1}
	return p.call(ctx, "Debugger.continueToLocation", map[string]any{"location": loc}, nil)
}

// Scripts lists parsed scripts whose URL contains filter.
func (p *Page) Scripts(filter string) []Location {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dbg == nil {
		return nil
	}
	var out []Location
	for _, s := range p.dbg.scripts {
		if s.URL != "" && strings.Contains(s.URL, filter) {
			out = append(out, s)
		}
	}
	slices.SortFunc(out, func(a, b Location) int {
		if c := strings.Compare(a.URL, b.URL); c != 0 {
			return c
		}
		return strings.Compare(a.ScriptID, b.ScriptID)
	})
	return out
}

func (p *Page) scriptByURL(url string) (Location, error) {
	for _, s := range p.Scripts("") {
		if s.URL == url {
			return s, nil
		}
	}
	return Location{}, fmt.Errorf("no loaded script with URL %s (see debugger scripts)", url)
}

// Source returns lines from..to (1-based, inclusive) of a script, numbered.
// target is a script URL or script ID.
func (p *Page) Source(ctx context.Context, target string, from, to int) (string, error) {
	scriptID := target
	if s, err := p.scriptAt(target, from); err == nil {
		scriptID = s.ScriptID
	} else if src, name, ok := p.authoredSource(ctx, target); ok {
		return name + " (from its source map):\n" + numberLines(src, from, to, 0), nil
	}
	var res struct {
		ScriptSource string `json:"scriptSource"`
	}
	if err := p.call(ctx, "Debugger.getScriptSource", map[string]any{"scriptId": scriptID}, &res); err != nil {
		return "", err
	}
	first, _ := p.scriptStart(scriptID)
	return numberLinesAt(res.ScriptSource, first+1, from, to, 0), nil
}

func (p *Page) scriptSource(ctx context.Context, scriptID string) (string, error) {
	var res struct {
		ScriptSource string `json:"scriptSource"`
	}
	err := p.call(ctx, "Debugger.getScriptSource", map[string]any{"scriptId": scriptID}, &res)
	return res.ScriptSource, err
}

// scriptStart is where a script begins in its resource, 0-based: an inline
// <script> starts partway down its HTML document, and CDP reports positions
// in the document.
func (p *Page) scriptStart(scriptID string) (line, column int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dbg == nil {
		return 0, 0
	}
	s := p.dbg.starts[scriptID]
	return s[0], s[1]
}

// scriptAt picks the script with this URL that holds 1-based resource line
// line; an HTML page can have several inline scripts.
func (p *Page) scriptAt(url string, line int) (Location, error) {
	var found []Location
	for _, s := range p.Scripts("") {
		if s.URL == url {
			found = append(found, s)
		}
	}
	if len(found) == 0 {
		return Location{}, fmt.Errorf("no loaded script with URL %s (see debugger scripts)", url)
	}
	for _, s := range found {
		start, _ := p.scriptStart(s.ScriptID)
		if line > start && line <= s.Line {
			return s, nil
		}
	}
	return found[0], nil
}

// minifiedLine is the line length past which a pause shows a window around
// the column instead of whole lines, since minified bundles are one line.
const minifiedLine = 200

// sourceContext shows the code around a 1-based resource line:column: two
// lines either side for normal code, or 80 characters either side of the
// column (marked with ▶) for minified code. first is the resource line of
// the script's first line and firstCol its starting column, for inline
// scripts.
func sourceContext(src string, line, column, first, firstCol int) string {
	lines := strings.Split(src, "\n")
	at := line - first + 1
	if at < 1 || at > len(lines) {
		return ""
	}
	text := []rune(lines[at-1])
	if len(text) <= minifiedLine {
		return markLine(numberLinesAt(src, first, line-2, line+2, 0), line)
	}
	if at == 1 {
		column -= firstCol
	}
	col := min(max(column-1, 0), len(text))
	start, end := max(0, col-80), min(len(text), col+80)
	before, after := string(text[start:col]), string(text[col:end])
	if start > 0 {
		before = "…" + before
	}
	if end < len(text) {
		after += "…"
	}
	return fmt.Sprintf("► %4d:%d  %s▶%s", line, column, before, after)
}

// numberLines renders lines from..to with line numbers, marking mark with ►.
func numberLines(src string, from, to, mark int) string {
	return numberLinesAt(src, 1, from, to, mark)
}

// numberLinesAt is numberLines for a script whose first line is line first
// of its resource; from, to and mark are resource lines.
func numberLinesAt(src string, first, from, to, mark int) string {
	lines := strings.Split(src, "\n")
	last := first + len(lines) - 1
	from = max(first, from)
	if to <= 0 || to > last {
		to = last
	}
	var b strings.Builder
	for n := from; n <= to; n++ {
		prefix := "  "
		if n == mark {
			prefix = "► "
		}
		fmt.Fprintf(&b, "%s%4d  %s\n", prefix, n, shorten(lines[n-first], 200))
	}
	return strings.TrimRight(b.String(), "\n")
}

// Describe renders a pause: reason, location, surrounding source, and stack.
func (p *Page) Describe(ctx context.Context, pause Pause) string {
	var b strings.Builder
	reason := pause.Reason
	if (reason == "other" || reason == "ambiguous") && len(pause.HitBreakpoints) > 0 {
		reason = "breakpoint"
	}
	fmt.Fprintf(&b, "Paused (%s)", reason)
	if pause.Data != nil && pause.Reason == "exception" {
		fmt.Fprintf(&b, ": %s", firstLine(pause.Data.Description))
	}
	if len(pause.HitBreakpoints) > 0 {
		fmt.Fprintf(&b, " on %s", strings.Join(p.hitLabels(pause.HitBreakpoints), ", "))
	}
	if len(pause.Frames) == 0 {
		return b.String()
	}
	top := pause.Frames[0]
	fmt.Fprintf(&b, "\nat %s (%s)\n", functionName(top.Function), p.withOriginal(ctx, top.Location))
	if ctxt := p.originalContext(ctx, top.Location); ctxt != "" {
		b.WriteString(ctxt + "\n")
	} else if src, err := p.scriptSource(ctx, top.Location.ScriptID); err == nil {
		line, col := p.scriptStart(top.Location.ScriptID)
		if ctxt := sourceContext(src, top.Location.Line, top.Location.Column, line+1, col); ctxt != "" {
			b.WriteString(ctxt + "\n")
		}
	}
	if watch := p.watchText(ctx); watch != "" {
		b.WriteString(watch + "\n")
	}
	b.WriteString(p.StackText(ctx, pause, 8))
	return strings.TrimRight(b.String(), "\n")
}

// hitLabels names the breakpoints a pause hit by what they watch, keeping
// the id for remove: Chrome's ids alone ("7:1") say nothing.
func (p *Page) hitLabels(ids []string) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id
		if p.dbg == nil {
			continue
		}
		if bp, ok := p.dbg.breakpoints[id]; ok && bp.Where != "" {
			out[i] = bp.Where + " [" + id + "]"
		}
	}
	return out
}

func markLine(numbered string, line int) string {
	want := fmt.Sprintf("  %4d  ", line)
	lines := strings.Split(numbered, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, want) {
			lines[i] = "► " + l[2:]
		}
	}
	return strings.Join(lines, "\n")
}

// StackText renders up to max frames as #N function (url:line:col), with
// original positions from source maps and the async frames that scheduled
// the code, as DevTools' Call Stack does.
func (p *Page) StackText(ctx context.Context, pause Pause, max int) string {
	var b strings.Builder
	b.WriteString("Stack:")
	n := 0
	write := func(f Frame) bool {
		if n == max {
			return false
		}
		fmt.Fprintf(&b, "\n  #%d %s (%s)", n, functionName(f.Function), p.withOriginal(ctx, f.Location))
		n++
		return true
	}
	total := len(pause.Frames)
	for _, seg := range pause.Async {
		total += len(seg.Frames)
	}
	for _, f := range pause.Frames {
		if !write(f) {
			fmt.Fprintf(&b, "\n  … %d more", total-n)
			return b.String()
		}
	}
	for _, seg := range pause.Async {
		fmt.Fprintf(&b, "\n  -- %s --", functionName(seg.Description))
		for _, f := range seg.Frames {
			if !write(f) {
				fmt.Fprintf(&b, "\n  … %d more", total-n)
				return b.String()
			}
		}
	}
	return b.String()
}

func functionName(name string) string {
	if name == "" {
		return "(anonymous)"
	}
	return name
}

// Watch adds an expression that is evaluated in the top frame and shown with
// every pause, like the DevTools Watch panel.
func (p *Page) Watch(expression string) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dbg != nil && !slices.Contains(p.dbg.watches, expression) {
		p.dbg.watches = append(p.dbg.watches, expression)
	}
	return p.watchList()
}

// Unwatch removes one watch expression, or all of them when expression is "".
func (p *Page) Unwatch(expression string) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dbg != nil {
		p.dbg.watches = slices.DeleteFunc(p.dbg.watches, func(w string) bool { return expression == "" || w == expression })
	}
	return p.watchList()
}

func (p *Page) watchList() []string {
	if p.dbg == nil {
		return nil
	}
	return slices.Clone(p.dbg.watches)
}

// watchText evaluates the watch expressions in the top frame.
func (p *Page) watchText(ctx context.Context) string {
	p.mu.Lock()
	watches := p.watchList()
	p.mu.Unlock()
	if len(watches) == 0 {
		return ""
	}
	lines := []string{"Watch:"}
	for _, w := range watches {
		v, err := p.Evaluate(ctx, w, 0)
		if err != nil {
			lines = append(lines, fmt.Sprintf("  %s: <%v>", w, err))
			continue
		}
		lines = append(lines, "  "+w+" = "+v.String())
	}
	return strings.Join(lines, "\n")
}

// Match is one occurrence of a search in a loaded script.
type Match struct {
	Location Location
	Snippet  string
}

// Search finds query in the source of loaded scripts whose URL contains
// urlFilter, like DevTools' search in all files. Columns make matches in
// minified one-line bundles usable as breakpoint positions.
func (p *Page) Search(ctx context.Context, query, urlFilter string, limit int) ([]Match, error) {
	if query == "" {
		return nil, fmt.Errorf("query must not be empty")
	}
	var matches []Match
	for _, script := range p.Scripts(urlFilter) {
		var res struct {
			ScriptSource string `json:"scriptSource"`
		}
		if err := p.call(ctx, "Debugger.getScriptSource", map[string]any{"scriptId": script.ScriptID}, &res); err != nil {
			continue
		}
		line, col := p.scriptStart(script.ScriptID)
		for _, m := range findAll(res.ScriptSource, query, limit-len(matches)) {
			if m.Location.Line == 1 {
				m.Location.Column += col
			}
			m.Location.Line += line
			m.Location.URL, m.Location.ScriptID = script.URL, script.ScriptID
			matches = append(matches, m)
		}
		if len(matches) >= limit {
			break
		}
	}
	return matches, nil
}

// findAll returns up to limit matches of query in src with 1-based positions
// and a short snippet around each.
func findAll(src, query string, limit int) []Match {
	var out []Match
	for offset := 0; len(out) < limit; {
		i := strings.Index(src[offset:], query)
		if i < 0 {
			break
		}
		at := offset + i
		line := strings.Count(src[:at], "\n") + 1
		column := at - strings.LastIndex(src[:at], "\n")
		start, end := max(0, at-40), min(len(src), at+len(query)+40)
		snippet := strings.ReplaceAll(src[start:end], "\n", "⏎")
		out = append(out, Match{Location: Location{Line: line, Column: column}, Snippet: snippet})
		offset = at + len(query)
	}
	return out
}

// Scope renders the variables visible in a paused frame, innermost scope
// first; the global scope is skipped because it is huge.
func (p *Page) Scope(ctx context.Context, frame int) (string, error) {
	pause := p.Paused()
	if pause == nil {
		return "", ErrNotPaused
	}
	if frame < 0 || frame >= len(pause.Frames) {
		return "", fmt.Errorf("frame must be 0-%d", len(pause.Frames)-1)
	}
	var b strings.Builder
	for _, s := range pause.Frames[frame].Scopes {
		if s.Type == "global" || s.Object.ObjectID == "" {
			continue
		}
		props, err := p.properties(ctx, s.Object.ObjectID, false)
		if err != nil {
			return "", err
		}
		title := s.Type
		if s.Name != "" {
			title += " " + s.Name
		}
		fmt.Fprintf(&b, "%s:\n%s\n", title, indent(props))
	}
	return strings.TrimRight(b.String(), "\n"), nil
}

// Properties renders an object's own properties as name = value lines, with
// object IDs so nested objects can be expanded.
func (p *Page) Properties(ctx context.Context, objectID string) (string, error) {
	return p.properties(ctx, objectID, true)
}

// properties lists an object's own properties and, with getters, the getters
// it inherits; scope objects have none worth reading.
func (p *Page) properties(ctx context.Context, objectID string, getters bool) (string, error) {
	var res struct {
		Result []struct {
			Name  string        `json:"name"`
			Value *RemoteObject `json:"value"`
		} `json:"result"`
	}
	params := map[string]any{"objectId": objectID, "ownProperties": true, "generatePreview": true}
	if err := p.call(ctx, "Runtime.getProperties", params, &res); err != nil {
		return "", err
	}
	lines := make([]string, 0, len(res.Result))
	for _, prop := range res.Result {
		if prop.Value == nil {
			// let/const before assignment (temporal dead zone) has no value.
			lines = append(lines, prop.Name+" = (uninitialized)")
			continue
		}
		line := prop.Name + " = " + prop.Value.String()
		if prop.Value.ObjectID != "" && prop.Value.Type == "object" {
			line += "  [" + prop.Value.ObjectID + "]"
		}
		lines = append(lines, line)
	}
	// Host objects (WebSocket, elements) and class instances keep their
	// fields as getters on the prototype; read those too, as expanding an
	// object in DevTools does.
	var inherited [][2]string
	if getters && p.callOn(ctx, objectID, prototypeGettersJS, nil, &inherited) == nil {
		for _, g := range inherited {
			lines = append(lines, g[0]+" = "+g[1]+"  (getter)")
		}
	}
	if len(lines) == 0 {
		return "(empty)", nil
	}
	return strings.Join(lines, "\n"), nil
}

// prototypeGettersJS reads the getters an object inherits, such as a
// WebSocket's url or onmessage handler, stopping at Object.prototype.
const prototypeGettersJS = `function() {
  const out = [], seen = new Set();
  for (let o = Object.getPrototypeOf(this); o && o !== Object.prototype && out.length < 40; o = Object.getPrototypeOf(o)) {
    for (const [k, d] of Object.entries(Object.getOwnPropertyDescriptors(o))) {
      if (seen.has(k) || !d.get || out.length >= 40) continue;
      seen.add(k);
      let v;
      try { v = this[k]; } catch { continue; }
      let text;
      if (typeof v === "function") text = "ƒ " + (v.name || "anonymous");
      else if (v === null || v === undefined) text = String(v);
      else if (typeof v === "string") text = JSON.stringify(v.length > 120 ? v.slice(0, 117) + "…" : v);
      else if (typeof v === "object") text = (v.constructor && v.constructor.name) || "Object";
      else text = String(v);
      out.push([k, text]);
    }
  }
  return out;
}`

func indent(s string) string {
	return "  " + strings.ReplaceAll(s, "\n", "\n  ")
}

// Evaluate runs expression in a paused frame, or globally when running.
func (p *Page) Evaluate(ctx context.Context, expression string, frame int) (RemoteObject, error) {
	var res struct {
		Result           RemoteObject      `json:"result"`
		ExceptionDetails *ExceptionDetails `json:"exceptionDetails"`
	}
	var err error
	if pause := p.Paused(); pause != nil && frame >= 0 && frame < len(pause.Frames) {
		err = p.call(ctx, "Debugger.evaluateOnCallFrame", map[string]any{
			"callFrameId": pause.Frames[frame].ID, "expression": expression, "generatePreview": true,
		}, &res)
	} else {
		err = p.call(ctx, "Runtime.evaluate", map[string]any{
			"expression": expression, "generatePreview": true, "awaitPromise": true,
		}, &res)
	}
	if err != nil {
		return RemoteObject{}, err
	}
	if res.ExceptionDetails != nil {
		return RemoteObject{}, res.ExceptionDetails
	}
	return res.Result, nil
}

// Listener is one event listener attached to an element.
type Listener struct {
	Type     string
	Capture  bool
	Once     bool
	Passive  bool
	Location Location
	Handler  string
}

// Listeners returns the event listeners attached directly to the first
// element matching selector.
func (p *Page) Listeners(ctx context.Context, selector string) ([]Listener, error) {
	// Chrome only includes each listener's handler when the element was
	// fetched into a named object group.
	var found struct {
		Result           RemoteObject      `json:"result"`
		ExceptionDetails *ExceptionDetails `json:"exceptionDetails"`
	}
	params := map[string]any{"expression": fmt.Sprintf("document.querySelector(%s)", jsQuote(selector)), "objectGroup": "agent-browser-mcp"}
	if err := p.call(ctx, "Runtime.evaluate", params, &found); err != nil {
		return nil, err
	}
	if found.ExceptionDetails != nil {
		return nil, found.ExceptionDetails
	}
	el := found.Result
	if el.ObjectID == "" {
		return nil, fmt.Errorf("no element matches %s", selector)
	}
	var res struct {
		Listeners []struct {
			Type         string        `json:"type"`
			UseCapture   bool          `json:"useCapture"`
			Passive      bool          `json:"passive"`
			Once         bool          `json:"once"`
			ScriptID     string        `json:"scriptId"`
			LineNumber   int           `json:"lineNumber"`
			ColumnNumber int           `json:"columnNumber"`
			Handler      *RemoteObject `json:"handler"`
		} `json:"listeners"`
	}
	if err := p.call(ctx, "DOMDebugger.getEventListeners", map[string]any{"objectId": el.ObjectID}, &res); err != nil {
		return nil, err
	}
	out := make([]Listener, 0, len(res.Listeners))
	for _, l := range res.Listeners {
		listener := Listener{Type: l.Type, Capture: l.UseCapture, Once: l.Once, Passive: l.Passive,
			Location: p.withOriginal(ctx, p.location(cdpLocation{ScriptID: l.ScriptID, LineNumber: l.LineNumber, ColumnNumber: l.ColumnNumber}, ""))}
		if l.Handler != nil {
			listener.Handler = l.Handler.String()
		}
		out = append(out, listener)
	}
	return out, nil
}

func (p *Page) querySelector(ctx context.Context, selector string) (int, error) {
	var doc struct {
		Root struct {
			NodeID int `json:"nodeId"`
		} `json:"root"`
	}
	if err := p.call(ctx, "DOM.getDocument", map[string]any{"depth": 0}, &doc); err != nil {
		return 0, err
	}
	var res struct {
		NodeID int `json:"nodeId"`
	}
	if err := p.call(ctx, "DOM.querySelector", map[string]any{"nodeId": doc.Root.NodeID, "selector": selector}, &res); err != nil {
		return 0, err
	}
	if res.NodeID == 0 {
		return 0, fmt.Errorf("no element matches %s", selector)
	}
	return res.NodeID, nil
}

func jsQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
