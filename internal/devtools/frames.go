package devtools

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

type frameNode struct {
	Frame struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		URL  string `json:"url"`
	} `json:"frame"`
	ChildFrames []frameNode `json:"childFrames"`
}

// EvaluateIn runs JavaScript in an iframe or a worker, as choosing a context
// in the Console's JavaScript context menu does. where is an iframe's name or
// part of its URL, or worker:<part of URL> for a dedicated, shared or
// service worker.
func (p *Page) EvaluateIn(ctx context.Context, where, expression string) (string, error) {
	if part, ok := strings.CutPrefix(where, "worker:"); ok {
		return p.evaluateInTarget(ctx, []string{"worker", "shared_worker", "service_worker"}, part, expression)
	}
	frame, err := p.findFrame(ctx, where)
	if err != nil {
		// A cross-site iframe runs in its own process and is missing from
		// this page's frame tree; look for it among the iframe targets.
		if v, terr := p.evaluateInTarget(ctx, []string{"iframe"}, where, expression); terr == nil {
			return v, nil
		}
		return "", err
	}
	if id, ok := p.frameContext(ctx, frame.Frame.ID); ok {
		v, err := p.evaluateOn(ctx, p.sessionID, map[string]any{"contextId": id}, expression)
		if err != nil {
			return "", err
		}
		return "in " + frame.Frame.URL + ": " + v, nil
	}
	// A cross-origin iframe runs in its own process, as a target of its own.
	return p.evaluateInTarget(ctx, []string{"iframe"}, frame.Frame.URL, expression)
}

// findFrame returns the first child frame whose name is where or whose URL
// contains it.
func (p *Page) findFrame(ctx context.Context, where string) (frameNode, error) {
	var res struct {
		FrameTree frameNode `json:"frameTree"`
	}
	if err := p.call(ctx, "Page.getFrameTree", nil, &res); err != nil {
		return frameNode{}, err
	}
	var all []string
	var walk func(n frameNode) (frameNode, bool)
	walk = func(n frameNode) (frameNode, bool) {
		for _, c := range n.ChildFrames {
			all = append(all, cmpName(c))
			if c.Frame.Name == where || strings.Contains(c.Frame.URL, where) {
				return c, true
			}
			if f, ok := walk(c); ok {
				return f, true
			}
		}
		return frameNode{}, false
	}
	if f, ok := walk(res.FrameTree); ok {
		return f, nil
	}
	if len(all) == 0 {
		return frameNode{}, fmt.Errorf("the page has no iframes")
	}
	return frameNode{}, fmt.Errorf("no iframe named or at a URL containing %q; frames: %s", where, strings.Join(all, ", "))
}

func cmpName(n frameNode) string {
	if n.Frame.Name != "" {
		return n.Frame.Name + " (" + n.Frame.URL + ")"
	}
	return n.Frame.URL
}

// frameContext returns the default JavaScript context of a frame in this
// page's process, tracking contexts from the first call on.
func (p *Page) frameContext(ctx context.Context, frameID string) (int, bool) {
	p.mu.Lock()
	tracking := p.contexts != nil
	if !tracking {
		p.contexts = map[string]int{}
	}
	p.mu.Unlock()
	if !tracking {
		p.onSession("Runtime.executionContextCreated", func(params json.RawMessage) {
			var e struct {
				Context struct {
					ID      int `json:"id"`
					AuxData struct {
						FrameID   string `json:"frameId"`
						IsDefault bool   `json:"isDefault"`
					} `json:"auxData"`
				} `json:"context"`
			}
			if json.Unmarshal(params, &e) == nil && e.Context.AuxData.IsDefault {
				p.mu.Lock()
				p.contexts[e.Context.AuxData.FrameID] = e.Context.ID
				p.mu.Unlock()
			}
		})
		p.onSession("Runtime.executionContextDestroyed", func(params json.RawMessage) {
			var e struct {
				ID int `json:"executionContextId"`
			}
			if json.Unmarshal(params, &e) == nil {
				p.mu.Lock()
				for frame, id := range p.contexts {
					if id == e.ID {
						delete(p.contexts, frame)
					}
				}
				p.mu.Unlock()
			}
		})
		p.onSession("Runtime.executionContextsCleared", func(json.RawMessage) {
			p.mu.Lock()
			clear(p.contexts)
			p.mu.Unlock()
		})
		// Enabling replays a created event for every existing context.
		if p.call(ctx, "Runtime.enable", nil, nil) != nil {
			return 0, false
		}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	id, ok := p.contexts[frameID]
	return id, ok
}

// evaluateInTarget attaches to the first target of one of the types whose URL
// contains part, evaluates there and detaches.
func (p *Page) evaluateInTarget(ctx context.Context, types []string, part, expression string) (string, error) {
	var res struct {
		TargetInfos []struct {
			TargetID string `json:"targetId"`
			Type     string `json:"type"`
			URL      string `json:"url"`
		} `json:"targetInfos"`
	}
	if err := p.browserCall(ctx, "Target.getTargets", nil, &res); err != nil {
		return "", err
	}
	var seen []string
	for _, t := range res.TargetInfos {
		if !slices.Contains(types, t.Type) {
			continue
		}
		seen = append(seen, t.Type+" "+t.URL)
		if !strings.Contains(t.URL, part) {
			continue
		}
		var attached struct {
			SessionID string `json:"sessionId"`
		}
		if err := p.browserCall(ctx, "Target.attachToTarget", map[string]any{"targetId": t.TargetID, "flatten": true}, &attached); err != nil {
			return "", err
		}
		defer p.browserCall(ctx, "Target.detachFromTarget", map[string]any{"sessionId": attached.SessionID}, nil)
		v, err := p.evaluateOn(ctx, attached.SessionID, nil, expression)
		if err != nil {
			return "", err
		}
		return "in " + t.Type + " " + t.URL + ": " + v, nil
	}
	if len(seen) == 0 {
		return "", fmt.Errorf("no %s is running", strings.Join(types, " or "))
	}
	return "", fmt.Errorf("none of these matches %q: %s", part, strings.Join(seen, "; "))
}

// evaluateOn evaluates in a session, with extra params such as contextId.
func (p *Page) evaluateOn(ctx context.Context, sessionID string, extra map[string]any, expression string) (string, error) {
	params := map[string]any{"expression": expression, "generatePreview": true, "awaitPromise": true, "replMode": true}
	for k, v := range extra {
		params[k] = v
	}
	var res struct {
		Result           RemoteObject      `json:"result"`
		ExceptionDetails *ExceptionDetails `json:"exceptionDetails"`
	}
	cctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	if err := p.conn.Call(cctx, sessionID, "Runtime.evaluate", params, &res); err != nil {
		return "", err
	}
	if res.ExceptionDetails != nil {
		return "", res.ExceptionDetails
	}
	var text string
	if res.Result.Type == "string" && json.Unmarshal(res.Result.Value, &text) == nil {
		return text, nil // bare, as eval_script returns strings from the page
	}
	return res.Result.Full(), nil
}

// EvaluateScript runs a script in the page the way the Console does (REPL
// mode): a later call may declare the same let or const again, top-level
// await works, a promise value is awaited, and the page sees a user gesture. A string comes back bare and
// other values as JSON, falling back to the Console's preview for values JSON
// cannot show, such as DOM nodes.
func (p *Page) EvaluateScript(ctx context.Context, expression string) (string, error) {
	// A script that hits a breakpoint, or a pause requested earlier, stops
	// mid-run: say where at once, instead of waiting for the call to time out.
	next, stop := p.NextPause()
	defer stop()
	type result struct {
		text string
		err  error
	}
	done := make(chan result, 1)
	go func() {
		text, err := p.evaluateScript(context.WithoutCancel(ctx), expression)
		done <- result{text, err}
	}()
	select {
	case r := <-done:
		return r.text, r.err
	case pause := <-next:
		return "", fmt.Errorf("the script paused in the debugger; it finishes after you resume with the debugger tool:\n%s", p.Describe(ctx, pause))
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func (p *Page) evaluateScript(ctx context.Context, expression string) (string, error) {
	const group = "agent-browser-mcp-eval"
	defer p.call(context.WithoutCancel(ctx), "Runtime.releaseObjectGroup", map[string]any{"objectGroup": group}, nil)
	var res struct {
		Result           RemoteObject      `json:"result"`
		ExceptionDetails *ExceptionDetails `json:"exceptionDetails"`
	}
	params := map[string]any{"expression": expression, "replMode": true, "awaitPromise": true, "userGesture": true,
		"generatePreview": true, "objectGroup": group}
	if err := p.call(ctx, "Runtime.evaluate", params, &res); err != nil {
		return "", err
	}
	if res.ExceptionDetails != nil {
		return "", res.ExceptionDetails
	}
	// REPL mode awaits only top-level await; a script whose value is a
	// promise, like fetch(url).then(...), is awaited here as the CLI did.
	if res.Result.Subtype == "promise" && res.Result.ObjectID != "" {
		res.ExceptionDetails = nil
		params := map[string]any{"promiseObjectId": res.Result.ObjectID, "generatePreview": true}
		if err := p.call(ctx, "Runtime.awaitPromise", params, &res); err != nil {
			return "", err
		}
		if res.ExceptionDetails != nil {
			return "", res.ExceptionDetails
		}
	}
	v := res.Result
	var text string
	switch {
	case v.Type == "string" && json.Unmarshal(v.Value, &text) == nil:
		return text, nil
	case v.Type != "object" || v.Subtype == "null" || v.ObjectID == "":
		return v.Full(), nil
	case v.Subtype == "node": // as the Console labels it: body, div#app.main
		return v.Description, nil
	}
	var asJSON *string
	if p.callOn(ctx, v.ObjectID, toJSONJS, nil, &asJSON) == nil && asJSON != nil && (*asJSON != "{}" || v.ClassName == "Object") {
		return *asJSON, nil
	}
	return v.Full(), nil
}

// toJSONJS is the value as JSON, or null when it has none (cycles, BigInt).
const toJSONJS = `function() { try { return JSON.stringify(this) ?? null; } catch { return null; } }`
