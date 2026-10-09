// Package devtools adds browser features the agent-browser CLI does not have —
// network and CPU throttling, JS/CSS coverage, heap snapshots and Lighthouse —
// by talking to the browser over the Chrome DevTools Protocol.
//
// Throttling and coverage only last as long as the CDP session that set them,
// so the Pool keeps one connection per agent-browser session open across tool
// calls.
package devtools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/vercel-labs/agent-browser-mcp/internal/browser"
	"github.com/vercel-labs/agent-browser-mcp/internal/cdp"
)

// Pool holds a CDP connection to the active tab of each browser session.
type Pool struct {
	mgr   *browser.Manager
	mu    sync.Mutex
	pages map[string]*Page
	// notices says, per session, why the page being debugged was dropped,
	// until a tool result reports it.
	notices map[string]string
}

// NewPool creates an empty pool.
func NewPool(mgr *browser.Manager) *Pool {
	return &Pool{mgr: mgr, pages: make(map[string]*Page), notices: make(map[string]string)}
}

// Notice returns, once, a note that the tab being debugged in session was
// closed and its breakpoints dropped; "" if there is nothing to report.
func (pl *Pool) Notice(session string) string {
	key := pl.mgr.ResolveSession(session)
	pl.mu.Lock()
	defer pl.mu.Unlock()
	n := pl.notices[key]
	delete(pl.notices, key)
	return n
}

// Page is a CDP session attached to one tab.
type Page struct {
	conn      *cdp.Conn
	sessionID string
	targetID  string
	timeout   time.Duration
	// detached is set when Chrome ends the session, e.g. the tab was closed.
	detached atomic.Bool

	mu       sync.Mutex
	coverage *coverageRun
	throttle Throttle
	dbg      *debuggerState
	issues   *issueLog
	// perfSince is when Performance metrics started counting.
	perfSince time.Time
	rendering map[string]bool // overlay label -> on
}

// ErrPaused is returned by non-debugger features while the page is paused,
// because Chrome holds most commands until the page resumes.
var ErrPaused = errors.New("the page is paused in the debugger; resume or step with the debugger tool first")

// call sends a command to this page's target. It gives up after the server's
// command timeout so a paused or hung page cannot block a tool forever.
func (p *Page) call(ctx context.Context, method string, params, result any) error {
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	err := p.conn.Call(ctx, p.sessionID, method, params, result)
	switch {
	case errors.Is(err, context.DeadlineExceeded) && p.Paused() != nil:
		return fmt.Errorf("%w (%s timed out)", ErrPaused, method)
	case err != nil && strings.Contains(err.Error(), "Session with given id not found"):
		p.detached.Store(true)
		return ErrTabClosed
	}
	return err
}

// ErrTabClosed is returned when the tab this page was attached to is gone.
var ErrTabClosed = errors.New("the tab was closed or replaced; run the command again to use the session's current tab")

// alive reports whether the page's tab can still take commands.
func (p *Page) alive() bool {
	return p.conn.Alive() && !p.detached.Load()
}

// Page returns a CDP session on the session's active tab, reusing the open
// connection while the same tab is still active.
func (pl *Pool) Page(ctx context.Context, session string) (*Page, error) {
	// While debugging, stay on the tab being debugged: it may pause at any
	// moment, and agent-browser commands to a paused page block until resume.
	if p := pl.Existing(session); p != nil && p.DebuggerOn() {
		return p, nil
	}
	targetID, cliErr := pl.activeTarget(ctx, session)
	if cliErr != nil {
		// Keep the tab we are attached to, or look for it over CDP below.
		if p := pl.Existing(session); p != nil {
			return p, nil
		}
	}
	key := pl.mgr.ResolveSession(session)

	pl.mu.Lock()
	defer pl.mu.Unlock()
	if p := pl.pages[key]; p != nil {
		if p.alive() && p.targetID == targetID {
			return p, nil
		}
		if !p.alive() && p.DebuggerOn() {
			pl.notices[key] = "Note: the tab being debugged was closed, so its breakpoints, watches and pause are gone; now attached to the session's current tab."
		}
		p.conn.Close()
		delete(pl.pages, key)
	}
	p, err := pl.attach(ctx, session, targetID)
	if err != nil {
		if cliErr != nil {
			return nil, fmt.Errorf("%w; agent-browser could not name the active tab either (%v)", err, cliErr)
		}
		return nil, err
	}
	pl.pages[key] = p
	return p, nil
}

// Existing returns the open page for a session without contacting the
// browser, or nil if there is none.
func (pl *Pool) Existing(session string) *Page {
	key := pl.mgr.ResolveSession(session)
	pl.mu.Lock()
	defer pl.mu.Unlock()
	if p := pl.pages[key]; p != nil && p.alive() {
		return p
	}
	return nil
}

// Forget closes the connection for a session, e.g. after its browser closed.
func (pl *Pool) Forget(session string) {
	key := pl.mgr.ResolveSession(session)
	pl.mu.Lock()
	defer pl.mu.Unlock()
	if p := pl.pages[key]; p != nil {
		p.conn.Close()
		delete(pl.pages, key)
	}
}

// Close closes every connection.
func (pl *Pool) Close() {
	pl.mu.Lock()
	defer pl.mu.Unlock()
	for key, p := range pl.pages {
		p.conn.Close()
		delete(pl.pages, key)
	}
}

// BrowserURL returns the CDP WebSocket URL of a session's browser.
func (pl *Pool) BrowserURL(ctx context.Context, session string) (string, error) {
	return pl.cliField(ctx, session, "cdpUrl", "get", "cdp-url")
}

// activeTarget returns the CDP target ID of the session's active tab. Unlike
// the page URL, agent-browser can report it while the page is paused.
func (pl *Pool) activeTarget(ctx context.Context, session string) (string, error) {
	res, err := pl.mgr.Run(ctx, session, "tab", "list")
	if err != nil {
		return "", err
	}
	var data struct {
		Tabs []struct {
			Active   bool   `json:"active"`
			TargetID string `json:"targetId"`
		} `json:"tabs"`
	}
	if err := json.Unmarshal(res.Data, &data); err != nil {
		return "", fmt.Errorf("agent-browser tab list: unexpected output %s", res.Data)
	}
	for _, t := range data.Tabs {
		if t.Active && t.TargetID != "" {
			return t.TargetID, nil
		}
	}
	return "", fmt.Errorf("agent-browser tab list names no active tab")
}

// cliField runs an agent-browser command and returns one string field of its data.
func (pl *Pool) cliField(ctx context.Context, session, field string, args ...string) (string, error) {
	res, err := pl.mgr.Run(ctx, session, args...)
	if err != nil {
		return "", err
	}
	var data map[string]any
	if err := json.Unmarshal(res.Data, &data); err != nil {
		return "", fmt.Errorf("agent-browser %s: unexpected output %s", args[0], res.Data)
	}
	v, _ := data[field].(string)
	if v == "" {
		return "", fmt.Errorf("agent-browser %s returned no %s", args[0], field)
	}
	return v, nil
}

// attach connects to the browser and attaches to the active tab.
func (pl *Pool) attach(ctx context.Context, session, targetID string) (*Page, error) {
	wsURL, err := pl.BrowserURL(ctx, session)
	if err != nil {
		return nil, err
	}
	conn, err := cdp.Dial(ctx, wsURL)
	if err != nil {
		return nil, err
	}
	target, err := findTab(ctx, conn, targetID)
	if err == nil {
		var sessionID string
		if sessionID, err = conn.Attach(ctx, target.TargetID); err == nil {
			timeout := time.Duration(pl.mgr.Config().DefaultTimeout) * time.Millisecond
			p := &Page{conn: conn, sessionID: sessionID, targetID: target.TargetID, timeout: timeout}
			conn.On("Target.detachedFromTarget", func(ev cdp.Event) {
				var e struct {
					SessionID string `json:"sessionId"`
				}
				if json.Unmarshal(ev.Params, &e) == nil && e.SessionID == sessionID {
					p.detached.Store(true)
				}
			})
			return p, nil
		}
	}
	conn.Close()
	return nil, err
}

// findTab returns the page target with targetID or, when targetID is ""
// because agent-browser could not say which tab is active, the browser's only
// tab; DevTools windows are pages too but never the tab being worked on.
func findTab(ctx context.Context, conn *cdp.Conn, targetID string) (cdp.TargetInfo, error) {
	pages, err := conn.Pages(ctx)
	if err != nil {
		return cdp.TargetInfo{}, err
	}
	var tabs []cdp.TargetInfo
	for _, p := range pages {
		if targetID != "" && p.TargetID == targetID {
			return p, nil
		}
		if !strings.HasPrefix(p.URL, "devtools://") {
			tabs = append(tabs, p)
		}
	}
	switch {
	case targetID != "":
		return cdp.TargetInfo{}, fmt.Errorf("the active tab %s is not among the browser's tabs", targetID)
	case len(tabs) != 1:
		return cdp.TargetInfo{}, fmt.Errorf("cannot tell which of %d tabs is active", len(tabs))
	}
	return tabs[0], nil
}
