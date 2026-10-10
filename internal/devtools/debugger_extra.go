package devtools

import (
	"cmp"
	"context"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"time"
)

// loadedMap is a script's source map, or why it could not be loaded.
type loadedMap struct {
	m   *SourceMap
	err error
}

// maxSourceMap bounds a source map download; large apps ship maps of tens of MB.
const maxSourceMap = 64 << 20

// sourceMap returns the script's source map, fetching it on first use. A
// script without one returns nil, nil.
func (p *Page) sourceMap(ctx context.Context, scriptID string) (*SourceMap, error) {
	p.mu.Lock()
	if p.dbg == nil {
		p.mu.Unlock()
		return nil, nil
	}
	mapURL := p.dbg.mapURLs[scriptID]
	cached := p.dbg.maps[scriptID]
	p.mu.Unlock()
	if mapURL == "" {
		return nil, nil
	}
	if cached != nil {
		return cached.m, cached.err
	}
	data, err := fetchSourceMap(ctx, mapURL)
	var m *SourceMap
	if err == nil {
		m, err = ParseSourceMap(data, mapURL)
	}
	p.mu.Lock()
	if p.dbg != nil {
		p.dbg.maps[scriptID] = &loadedMap{m: m, err: err}
	}
	p.mu.Unlock()
	return m, err
}

// fetchSourceMap reads a data: URL or downloads the map.
func fetchSourceMap(ctx context.Context, u string) ([]byte, error) {
	if data, ok, err := sourceMapData(u); ok {
		return data, err
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("source map %s: HTTP %d", u, res.StatusCode)
	}
	return io.ReadAll(io.LimitReader(res.Body, maxSourceMap))
}

// withOriginal adds the authored file and position to a location in a
// script that has a source map.
func (p *Page) withOriginal(ctx context.Context, loc Location) Location {
	if loc.ScriptID == "" || loc.Original != "" {
		return loc
	}
	m, err := p.sourceMap(ctx, loc.ScriptID)
	if err != nil || m == nil {
		return loc
	}
	if src, line, col, ok := m.Original(loc.Line-1, loc.Column-1); ok {
		loc.Original = fmt.Sprintf("%s:%d:%d", ShortSource(src), line+1, col+1)
	}
	return loc
}

// originalContext is the authored source around loc, as DevTools shows a
// pause in a bundle with a source map that embeds its sources; empty when
// there is none.
func (p *Page) originalContext(ctx context.Context, loc Location) string {
	m, err := p.sourceMap(ctx, loc.ScriptID)
	if err != nil || m == nil {
		return ""
	}
	src, line, col, ok := m.Original(loc.Line-1, loc.Column-1)
	if !ok {
		return ""
	}
	i := slices.Index(m.Sources, src)
	if i < 0 || i >= len(m.Content) || m.Content[i] == nil {
		return ""
	}
	ctxt := sourceContext(*m.Content[i], line+1, col+1, 1, 0)
	if ctxt == "" {
		return ""
	}
	return ShortSource(src) + ":\n" + ctxt
}

// HasSourceMap reports whether a script names a source map.
func (p *Page) HasSourceMap(scriptID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.dbg != nil && p.dbg.mapURLs[scriptID] != ""
}

// mappedScripts lists the loaded scripts that have source maps.
func (p *Page) mappedScripts() []Location {
	var out []Location
	for _, s := range p.Scripts("") {
		if p.HasSourceMap(s.ScriptID) {
			out = append(out, s)
		}
	}
	return out
}

// authoredBreakpoint maps a breakpoint on an authored file, such as
// src/cart.ts:12, to the bundle position it compiled to. It only applies when
// spec names a path that is not a loaded script URL.
func (p *Page) authoredBreakpoint(ctx context.Context, spec BreakpointSpec) (Location, bool) {
	if spec.URL == "" || spec.URLRegex != "" || strings.Contains(spec.URL, "://") {
		return Location{}, false
	}
	if _, err := p.scriptByURL(spec.URL); err == nil {
		return Location{}, false
	}
	for _, s := range p.mappedScripts() {
		m, err := p.sourceMap(ctx, s.ScriptID)
		if err != nil || m == nil {
			continue
		}
		src, ok := m.FindSource(spec.URL)
		if !ok {
			continue
		}
		if line, col, ok := m.Generated(src, spec.Line-1); ok {
			return Location{URL: s.URL, ScriptID: s.ScriptID, Line: line + 1, Column: col + 1}, true
		}
	}
	return Location{}, false
}

// authoredSource returns an authored file's source from a source map that
// embeds it.
func (p *Page) authoredSource(ctx context.Context, file string) (string, string, bool) {
	for _, s := range p.mappedScripts() {
		m, err := p.sourceMap(ctx, s.ScriptID)
		if err != nil || m == nil {
			continue
		}
		if i, ok := m.FindSource(file); ok && m.Content[i] != nil {
			return *m.Content[i], ShortSource(m.Sources[i]), true
		}
	}
	return "", "", false
}

// RestartFrame re-runs a paused frame from its first line, as DevTools'
// "Restart frame" does.
func (p *Page) RestartFrame(ctx context.Context, frame int) error {
	pause := p.Paused()
	if pause == nil {
		return ErrNotPaused
	}
	if frame < 0 || frame >= len(pause.Frames) {
		return fmt.Errorf("frame %d out of range (0-%d)", frame, len(pause.Frames)-1)
	}
	// StepInto runs on to the first line of the restarted frame and pauses there.
	return p.call(ctx, "Debugger.restartFrame", map[string]any{"callFrameId": pause.Frames[frame].ID, "mode": "StepInto"}, nil)
}

// SetFunctionBreakpoint pauses whenever the function expression evaluates to
// is called, like debug(fn) in the console; with logMessage it logs instead
// of pausing, like monitor(fn). It does not survive a reload.
func (p *Page) SetFunctionBreakpoint(ctx context.Context, expression, condition, logMessage string) (Breakpoint, error) {
	fn, err := p.Evaluate(ctx, expression, 0)
	if err != nil {
		return Breakpoint{}, err
	}
	if fn.Type != "function" || fn.ObjectID == "" {
		return Breakpoint{}, fmt.Errorf("%s is %s, not a function", expression, cmp.Or(fn.Description, fn.Type))
	}
	if logMessage != "" {
		condition = fmt.Sprintf("console.log(%s), false", logMessage)
	}
	params := map[string]any{"objectId": fn.ObjectID}
	if condition != "" {
		params["condition"] = condition
	}
	var res struct {
		BreakpointID string `json:"breakpointId"`
	}
	if err := p.call(ctx, "Debugger.setBreakpointOnFunctionCall", params, &res); err != nil {
		return Breakpoint{}, err
	}
	bp := &Breakpoint{ID: res.BreakpointID, Kind: "function", Where: "calls to " + expression, Condition: condition}
	p.addBreakpoint(bp)
	return *bp, nil
}

// SetCSPBreakpoint pauses on Trusted Types violations of the page's Content
// Security Policy, as Sources' "CSP Violation Breakpoints" do.
func (p *Page) SetCSPBreakpoint(ctx context.Context) (Breakpoint, error) {
	types := []string{"trustedtype-sink-violation", "trustedtype-policy-violation"}
	if err := p.call(ctx, "DOMDebugger.setBreakOnCSPViolation", map[string]any{"violationTypes": types}, nil); err != nil {
		return Breakpoint{}, err
	}
	bp := &Breakpoint{ID: "csp", Kind: "csp", Where: "Trusted Types CSP violations"}
	p.addBreakpoint(bp)
	return *bp, nil
}

// SetBlackbox makes stepping and pausing skip scripts whose URL matches any
// of the patterns (regular expressions), like DevTools' ignore list. An
// empty list clears it.
func (p *Page) SetBlackbox(ctx context.Context, patterns []string) error {
	if patterns == nil {
		patterns = []string{}
	}
	if err := p.call(ctx, "Debugger.setBlackboxPatterns", map[string]any{"patterns": patterns}, nil); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dbg != nil {
		p.dbg.blackbox = patterns
	}
	return nil
}

// Blackbox returns the ignore-list patterns.
func (p *Page) Blackbox() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.dbg == nil {
		return nil
	}
	return append([]string(nil), p.dbg.blackbox...)
}
