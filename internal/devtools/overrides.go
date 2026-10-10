package devtools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

// override is a file served in place of the network response, as DevTools'
// Local Overrides do.
type override struct {
	body        string
	contentType string
}

// SetOverride serves body for url from now on, intercepting the request with
// the Fetch domain. It lasts as long as this CDP connection.
func (p *Page) SetOverride(ctx context.Context, url, body, contentType string) error {
	p.mu.Lock()
	first := p.overrides == nil
	if first {
		p.overrides = map[string]override{}
	}
	p.overrides[url] = override{body: body, contentType: contentType}
	p.mu.Unlock()
	if first {
		p.onSession("Fetch.requestPaused", p.fetchPaused)
	}
	return p.applyOverrides(ctx)
}

// RemoveOverride drops the override for url, or all of them when url is
// empty, and returns how many were dropped.
func (p *Page) RemoveOverride(ctx context.Context, url string) (int, error) {
	p.mu.Lock()
	n := len(p.overrides)
	if url == "" {
		clear(p.overrides)
	} else {
		delete(p.overrides, url)
	}
	n -= len(p.overrides)
	p.mu.Unlock()
	if n == 0 {
		return 0, nil
	}
	return n, p.applyOverrides(ctx)
}

// Overrides lists the overridden URLs.
func (p *Page) Overrides() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, 0, len(p.overrides))
	for u := range p.overrides {
		out = append(out, u)
	}
	slices.Sort(out)
	return out
}

// applyOverrides intercepts exactly the overridden URLs, or stops
// intercepting when there are none.
func (p *Page) applyOverrides(ctx context.Context) error {
	urls := p.Overrides()
	if len(urls) == 0 {
		return p.call(ctx, "Fetch.disable", nil, nil)
	}
	patterns := make([]map[string]any, len(urls))
	for i, u := range urls {
		patterns[i] = map[string]any{"urlPattern": escapePattern(u), "requestStage": "Request"}
	}
	return p.call(ctx, "Fetch.enable", map[string]any{"patterns": patterns}, nil)
}

// escapePattern makes a URL match itself in a Fetch pattern, where * and ?
// are wildcards.
func escapePattern(u string) string {
	return strings.NewReplacer(`\`, `\\`, `*`, `\*`, `?`, `\?`).Replace(u)
}

// fetchPaused answers an intercepted request with its override. It runs on
// the connection's read loop, which cannot wait for a reply, so the answer
// is sent from a goroutine.
func (p *Page) fetchPaused(params json.RawMessage) {
	var e struct {
		RequestID string `json:"requestId"`
		Request   struct {
			URL string `json:"url"`
		} `json:"request"`
	}
	if json.Unmarshal(params, &e) != nil {
		return
	}
	url, _, _ := strings.Cut(e.Request.URL, "#")
	p.mu.Lock()
	o, ok := p.overrides[url]
	p.mu.Unlock()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
		defer cancel()
		if !ok {
			p.conn.Call(ctx, p.sessionID, "Fetch.continueRequest", map[string]any{"requestId": e.RequestID}, nil)
			return
		}
		p.conn.Call(ctx, p.sessionID, "Fetch.fulfillRequest", map[string]any{
			"requestId": e.RequestID, "responseCode": 200,
			"responseHeaders": []map[string]string{{"name": "Content-Type", "value": o.contentType}, {"name": "Cache-Control", "value": "no-store"}},
			"body":            base64.StdEncoding.EncodeToString([]byte(o.body)),
		}, nil)
	}()
}

// EditSource replaces text in a loaded script and serves the edited script
// in place of the original from now on, like an edit saved to DevTools'
// Local Overrides. find must occur exactly once. The caller reloads the page
// so the edited code runs; Chrome no longer edits running scripts in place.
func (p *Page) EditSource(ctx context.Context, script, find, replace string) (string, error) {
	s, err := p.scriptByURL(script)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(s.URL, "http") {
		return "", fmt.Errorf("%s was not loaded over HTTP, so it cannot be overridden", s.URL)
	}
	src, err := p.scriptSource(ctx, s.ScriptID)
	if err != nil {
		return "", err
	}
	switch n := strings.Count(src, find); {
	case find == "":
		return "", fmt.Errorf("find must not be empty")
	case n == 0:
		return "", fmt.Errorf("find text not in %s (see debugger source)", s.URL)
	case n > 1:
		return "", fmt.Errorf("find text occurs %d times in %s; include more context so it matches once", n, s.URL)
	}
	if err := p.SetOverride(ctx, s.URL, strings.Replace(src, find, replace, 1), "text/javascript; charset=utf-8"); err != nil {
		return "", err
	}
	if p.Paused() != nil {
		p.call(ctx, "Debugger.resume", nil, nil)
	}
	start, _ := p.scriptStart(s.ScriptID)
	line := start + strings.Count(src[:strings.Index(src, find)], "\n") + 1
	return fmt.Sprintf("edited %s at line %d; the edited file replaces the original until the browser closes or this server restarts (revert_source drops it)", s.URL, line), nil
}
