package devtools

import (
	"context"
	"fmt"
	"time"
)

// DevToolsPanels are the panels OpenDevTools can show first; resources is
// the Application panel.
var DevToolsPanels = []string{"elements", "console", "network", "sources", "resources", "performance"}

// DevToolsOpened says what opening DevTools did to the page's width.
type DevToolsOpened struct {
	WidthBefore int // page viewport in CSS px before; 0 when unknown
	WidthAfter  int // page viewport in CSS px after
	Widened     int // px the window grew to make room for docked DevTools
}

// String is a note for the model, or "" when the page kept its width.
func (o DevToolsOpened) String() string {
	switch {
	case o.WidthBefore == 0 || o.WidthAfter >= o.WidthBefore && o.Widened == 0:
		return ""
	case o.WidthAfter >= o.WidthBefore:
		return fmt.Sprintf("DevTools docked beside the page, so the window was widened by %dpx to keep the page %dpx wide.", o.Widened, o.WidthAfter)
	case o.Widened > 0:
		return fmt.Sprintf("DevTools docked beside the page; the window was widened by %dpx, as far as the screen allows, but the page is %dpx wide instead of %dpx, so the site may use a narrower (e.g. mobile) layout and hide elements. Take a fresh snapshot before acting; dragging the DevTools divider or undocking DevTools gives the page its width back.", o.Widened, o.WidthAfter, o.WidthBefore)
	}
	return fmt.Sprintf("DevTools docked beside the page and narrowed it from %dpx to %dpx; the site may switch to a narrower (e.g. mobile) layout and hide elements, so take a fresh snapshot before acting.", o.WidthBefore, o.WidthAfter)
}

// maxWidenSteps bounds how often the window is grown: docked DevTools keeps a
// share of the window, so each step gives the page only part of the growth.
const maxWidenSteps = 4

// OpenDevTools opens Chrome's own DevTools window for this page's tab in the
// same browser, on panel if given. Unlike a docked launch flag, this leaves
// agent-browser's tab tracking alone.
//
// Chrome docks DevTools inside the page's window, which narrows the page and
// can flip a responsive site into its mobile layout. So it watches the page
// width for up to settle and, while the page is narrower than before, widens
// the window, never beyond the screen.
func (p *Page) OpenDevTools(ctx context.Context, panel string, settle time.Duration) (DevToolsOpened, error) {
	before := p.viewportWidth(ctx)
	params := map[string]any{"targetId": p.targetID}
	if panel != "" {
		params["panelId"] = panel
	}
	err := p.browserCall(ctx, "Target.openDevTools", params, nil)
	if isMethodNotFound(err) {
		return DevToolsOpened{}, fmt.Errorf("this browser cannot open DevTools in place; use external:true to get a DevTools URL instead")
	}
	if err != nil || before == 0 {
		return DevToolsOpened{}, err
	}

	o := DevToolsOpened{WidthBefore: before, WidthAfter: p.waitForWidth(ctx, before, settle)}
	scr := p.screen(ctx)
	steps := maxWidenSteps
	if !scr.known {
		steps = 1 // without the screen size, one step cannot overshoot by much
	}
	grown, gained := 0, 0 // the last step: window growth and page gain
	for range steps {
		short := before - o.WidthAfter
		if o.WidthAfter <= 0 || short <= 0 {
			break
		}
		want := short // first assume DevTools keeps its width
		if grown > 0 {
			want = (short*grown + gained - 1) / gained // then scale by the share the page got
		}
		var err error
		if grown, err = p.widenWindow(ctx, want, scr); err != nil || grown <= 0 {
			break
		}
		prev := o.WidthAfter
		o.Widened += grown
		o.WidthAfter = p.waitForWidth(ctx, prev, settle)
		if gained = o.WidthAfter - prev; gained <= 0 {
			break // growing the window does not reach the page
		}
	}
	return o, nil
}

// viewportWidth is the page's layout viewport width in CSS px, or 0 if unknown.
func (p *Page) viewportWidth(ctx context.Context) int {
	var m struct {
		CSSLayoutViewport struct {
			ClientWidth int `json:"clientWidth"`
		} `json:"cssLayoutViewport"`
	}
	if p.call(ctx, "Page.getLayoutMetrics", nil, &m) != nil {
		return 0
	}
	return m.CSSLayoutViewport.ClientWidth
}

// widthPoll is how often waitForWidth samples the page width.
const widthPoll = 50 * time.Millisecond

// waitForWidth waits up to settle for the viewport width to move away from
// width and then stop changing (a resize can take several frames), and
// returns the last width seen.
func (p *Page) waitForWidth(ctx context.Context, width int, settle time.Duration) int {
	deadline := time.Now().Add(settle)
	last, steady := width, 0
	for {
		w := p.viewportWidth(ctx)
		switch {
		case w != last:
			last, steady = w, 0
		case w != width:
			steady++
		}
		if steady >= 2 || time.Now().After(deadline) {
			return last
		}
		select {
		case <-ctx.Done():
			return last
		case <-time.After(widthPoll):
		}
	}
}

// widenWindow grows the window that holds this page by up to px and returns
// how much it grew. With a known screen it never grows past the screen width
// and moves left to stay on it.
func (p *Page) widenWindow(ctx context.Context, px int, scr screenSpan) (int, error) {
	var w struct {
		WindowID int `json:"windowId"`
		Bounds   struct {
			Left        int    `json:"left"`
			Width       int    `json:"width"`
			WindowState string `json:"windowState"`
		} `json:"bounds"`
	}
	if err := p.browserCall(ctx, "Browser.getWindowForTarget", map[string]any{"targetId": p.targetID}, &w); err != nil {
		return 0, err
	}
	if s := w.Bounds.WindowState; s != "" && s != "normal" {
		return 0, fmt.Errorf("window is %s", s)
	}
	width := w.Bounds.Width + px
	bounds := map[string]any{}
	if scr.known {
		width = min(width, scr.right-scr.left)
		if w.Bounds.Left+width > scr.right {
			bounds["left"] = max(scr.left, scr.right-width)
		}
	}
	if width <= w.Bounds.Width {
		return 0, nil
	}
	bounds["width"] = width
	if err := p.browserCall(ctx, "Browser.setWindowBounds", map[string]any{"windowId": w.WindowID, "bounds": bounds}, nil); err != nil {
		return 0, err
	}
	return width - w.Bounds.Width, nil
}

// screenSpan is the horizontal extent of the screen the page is on, in the
// same units as window bounds; known is false if the page cannot say (e.g. it
// is paused in the debugger).
type screenSpan struct {
	left, right int
	known       bool
}

func (p *Page) screen(ctx context.Context) screenSpan {
	var res struct {
		Result struct {
			Value struct{ Left, Width int } `json:"value"`
		} `json:"result"`
	}
	err := p.call(ctx, "Runtime.evaluate", map[string]any{
		"expression":    "({left: screen.availLeft, width: screen.availWidth})",
		"returnByValue": true,
	}, &res)
	if err != nil || res.Result.Value.Width == 0 {
		return screenSpan{}
	}
	v := res.Result.Value
	return screenSpan{left: v.Left, right: v.Left + v.Width, known: true}
}

// browserCall sends a browser-level command (not scoped to the page session).
func (p *Page) browserCall(ctx context.Context, method string, params, result any) error {
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	return p.conn.Call(ctx, "", method, params, result)
}
