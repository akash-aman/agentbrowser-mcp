package devtools

import (
	"bytes"
	"cmp"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Capture limits: the newest requests and socket messages are kept.
const (
	maxCapturedRequests = 500
	maxCapturedMessages = 1000
)

// netRequest is one request as the Network panel records it.
type netRequest struct {
	ID, URL, Method, Type string
	Status                int
	Initiator             string
	Headers               map[string]string // as sent, including cookies
	PostData              string
	MimeType              string
	Sent, Blocked         []string // cookies sent, and cookies withheld with why
	SetBlocked            []string // Set-Cookie headers refused, with why
	Failed                string
	Finished              bool
}

// socketMessage is one WebSocket frame or EventSource message.
type socketMessage struct {
	At   time.Time
	Dir  string // "sent", "received" or "event <name>"
	URL  string
	Data string
}

type netCapture struct {
	requests []*netRequest
	byID     map[string]*netRequest
	sockets  map[string]string // request ID -> URL
	messages []socketMessage
}

// StartCapture records requests with their initiators and cookie decisions,
// and WebSocket and EventSource messages, from now on. It is idempotent.
func (p *Page) StartCapture(ctx context.Context) (bool, error) {
	p.mu.Lock()
	started := p.capture != nil
	if !started {
		p.capture = &netCapture{byID: map[string]*netRequest{}, sockets: map[string]string{}}
	}
	p.mu.Unlock()
	if started {
		return false, nil
	}
	p.onSession("Network.requestWillBeSent", p.onRequest)
	p.onSession("Network.requestWillBeSentExtraInfo", p.onRequestExtra)
	p.onSession("Network.responseReceived", p.onResponse)
	p.onSession("Network.responseReceivedExtraInfo", p.onResponseExtra)
	p.onSession("Network.loadingFinished", p.onFinished)
	p.onSession("Network.loadingFailed", p.onFailed)
	p.onSession("Network.webSocketCreated", p.onSocket)
	p.onSession("Network.webSocketFrameSent", func(m json.RawMessage) { p.onFrame(m, "sent") })
	p.onSession("Network.webSocketFrameReceived", func(m json.RawMessage) { p.onFrame(m, "received") })
	p.onSession("Network.eventSourceMessageReceived", p.onEventSource)
	return true, p.call(ctx, "Network.enable", map[string]any{"maxPostDataSize": 65536}, nil)
}

// withCapture runs fn on the capture under the page lock.
func (p *Page) withCapture(fn func(c *netCapture)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.capture != nil {
		fn(p.capture)
	}
}

func (p *Page) onRequest(params json.RawMessage) {
	var e struct {
		RequestID string `json:"requestId"`
		Type      string `json:"type"`
		Request   struct {
			URL      string            `json:"url"`
			Method   string            `json:"method"`
			Headers  map[string]string `json:"headers"`
			PostData string            `json:"postData"`
		} `json:"request"`
		Initiator struct {
			Type       string `json:"type"`
			URL        string `json:"url"`
			LineNumber *int   `json:"lineNumber"`
			Stack      *struct {
				CallFrames []struct {
					FunctionName string `json:"functionName"`
					ScriptID     string `json:"scriptId"`
					URL          string `json:"url"`
					LineNumber   int    `json:"lineNumber"`
					ColumnNumber int    `json:"columnNumber"`
				} `json:"callFrames"`
			} `json:"stack"`
		} `json:"initiator"`
	}
	if json.Unmarshal(params, &e) != nil {
		return
	}
	init := e.Initiator.Type
	switch {
	case e.Initiator.Stack != nil && len(e.Initiator.Stack.CallFrames) > 0:
		var frames []string
		for _, f := range e.Initiator.Stack.CallFrames[:min(6, len(e.Initiator.Stack.CallFrames))] {
			frames = append(frames, fmt.Sprintf("%s (%s:%d:%d)", functionName(f.FunctionName), f.URL, f.LineNumber+1, f.ColumnNumber+1))
		}
		init += " " + strings.Join(frames, " ← ")
	case e.Initiator.URL != "":
		init += " " + e.Initiator.URL
		if e.Initiator.LineNumber != nil {
			init += fmt.Sprintf(":%d", *e.Initiator.LineNumber+1)
		}
	}
	p.withCapture(func(c *netCapture) {
		r := c.byID[e.RequestID]
		if r == nil || r.Finished { // a redirect keeps the ID; record the new hop
			r = &netRequest{ID: e.RequestID}
			c.byID[e.RequestID] = r
			c.requests = append(c.requests, r)
			if n := len(c.requests) - maxCapturedRequests; n > 0 {
				for _, old := range c.requests[:n] {
					if c.byID[old.ID] == old {
						delete(c.byID, old.ID)
					}
				}
				c.requests = slices.Delete(c.requests, 0, n)
			}
		}
		r.URL, r.Method, r.Type, r.Initiator, r.PostData = e.Request.URL, e.Request.Method, e.Type, init, e.Request.PostData
		if r.Headers == nil {
			r.Headers = e.Request.Headers
		}
	})
}

type cookieDecision struct {
	BlockedReasons []string `json:"blockedReasons"`
	Cookie         *struct {
		Name string `json:"name"`
	} `json:"cookie"`
	CookieLine string `json:"cookieLine"`
}

func (p *Page) onRequestExtra(params json.RawMessage) {
	var e struct {
		RequestID         string            `json:"requestId"`
		Headers           map[string]string `json:"headers"`
		AssociatedCookies []cookieDecision  `json:"associatedCookies"`
	}
	if json.Unmarshal(params, &e) != nil {
		return
	}
	p.withCapture(func(c *netCapture) {
		r := c.byID[e.RequestID]
		if r == nil {
			r = &netRequest{ID: e.RequestID}
			c.byID[e.RequestID] = r
			c.requests = append(c.requests, r)
		}
		if len(e.Headers) > 0 {
			r.Headers = e.Headers // the headers actually sent, with cookies
		}
		for _, d := range e.AssociatedCookies {
			name := "?"
			if d.Cookie != nil {
				name = d.Cookie.Name
			}
			if len(d.BlockedReasons) == 0 {
				r.Sent = append(r.Sent, name)
			} else {
				r.Blocked = append(r.Blocked, name+": "+strings.Join(d.BlockedReasons, ", "))
			}
		}
	})
}

func (p *Page) onResponse(params json.RawMessage) {
	var e struct {
		RequestID string `json:"requestId"`
		Response  struct {
			Status   int    `json:"status"`
			MimeType string `json:"mimeType"`
		} `json:"response"`
	}
	if json.Unmarshal(params, &e) != nil {
		return
	}
	p.withCapture(func(c *netCapture) {
		if r := c.byID[e.RequestID]; r != nil {
			r.Status, r.MimeType = e.Response.Status, e.Response.MimeType
		}
	})
}

func (p *Page) onResponseExtra(params json.RawMessage) {
	var e struct {
		RequestID      string           `json:"requestId"`
		BlockedCookies []cookieDecision `json:"blockedCookies"`
	}
	if json.Unmarshal(params, &e) != nil {
		return
	}
	p.withCapture(func(c *netCapture) {
		r := c.byID[e.RequestID]
		if r == nil {
			return
		}
		for _, d := range e.BlockedCookies {
			line, _, _ := strings.Cut(d.CookieLine, ";")
			r.SetBlocked = append(r.SetBlocked, line+": "+strings.Join(d.BlockedReasons, ", "))
		}
	})
}

func (p *Page) onFinished(params json.RawMessage) {
	var e struct {
		RequestID string `json:"requestId"`
	}
	if json.Unmarshal(params, &e) == nil {
		p.withCapture(func(c *netCapture) {
			if r := c.byID[e.RequestID]; r != nil {
				r.Finished = true
			}
		})
	}
}

func (p *Page) onFailed(params json.RawMessage) {
	var e struct {
		RequestID     string `json:"requestId"`
		ErrorText     string `json:"errorText"`
		BlockedReason string `json:"blockedReason"`
		Canceled      bool   `json:"canceled"`
	}
	if json.Unmarshal(params, &e) == nil {
		p.withCapture(func(c *netCapture) {
			if r := c.byID[e.RequestID]; r != nil {
				r.Finished, r.Failed = true, cmp.Or(e.BlockedReason, e.ErrorText)
				if e.Canceled {
					r.Failed = "canceled"
				}
			}
		})
	}
}

func (p *Page) onSocket(params json.RawMessage) {
	var e struct {
		RequestID string `json:"requestId"`
		URL       string `json:"url"`
	}
	if json.Unmarshal(params, &e) == nil {
		p.withCapture(func(c *netCapture) { c.sockets[e.RequestID] = e.URL })
	}
}

func (p *Page) onFrame(params json.RawMessage, dir string) {
	var e struct {
		RequestID string `json:"requestId"`
		Response  struct {
			Opcode      int    `json:"opcode"`
			PayloadData string `json:"payloadData"`
		} `json:"response"`
	}
	if json.Unmarshal(params, &e) != nil {
		return
	}
	data := e.Response.PayloadData
	if e.Response.Opcode == 2 {
		data = fmt.Sprintf("(binary, %d bytes base64)", len(data))
	}
	p.withCapture(func(c *netCapture) {
		c.add(socketMessage{At: time.Now(), Dir: dir, URL: c.sockets[e.RequestID], Data: data})
	})
}

func (p *Page) onEventSource(params json.RawMessage) {
	var e struct {
		RequestID string `json:"requestId"`
		EventName string `json:"eventName"`
		Data      string `json:"data"`
	}
	if json.Unmarshal(params, &e) != nil {
		return
	}
	p.withCapture(func(c *netCapture) {
		url := ""
		if r := c.byID[e.RequestID]; r != nil {
			url = r.URL
		}
		c.add(socketMessage{At: time.Now(), Dir: "event " + cmp.Or(e.EventName, "message"), URL: url, Data: e.Data})
	})
}

func (c *netCapture) add(m socketMessage) {
	c.messages = append(c.messages, m)
	if n := len(c.messages) - maxCapturedMessages; n > 0 {
		c.messages = slices.Delete(c.messages, 0, n)
	}
}

// SocketMessages lists WebSocket and EventSource messages whose URL or data
// contains pattern, the newest last.
func (p *Page) SocketMessages(pattern string, limit int) string {
	var lines []string
	total := 0
	p.withCapture(func(c *netCapture) {
		for _, m := range c.messages {
			if pattern != "" && !strings.Contains(m.URL, pattern) && !strings.Contains(m.Data, pattern) {
				continue
			}
			total++
			lines = append(lines, fmt.Sprintf("%s %-8s %s  %s", m.At.Format("15:04:05.000"), m.Dir, m.URL, shorten(m.Data, 300)))
		}
	})
	if len(lines) == 0 {
		return "no WebSocket or EventSource messages captured yet; they are recorded from the first network capture call on"
	}
	if len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return fmt.Sprintf("%d messages (showing the last %d):\n%s", total, len(lines), strings.Join(lines, "\n"))
}

// findRequest returns the captured request with this ID, or the newest one
// whose URL contains it.
func (p *Page) findRequest(q string) (netRequest, error) {
	var found *netRequest
	var none bool
	p.withCapture(func(c *netCapture) {
		none = len(c.requests) == 0
		if r := c.byID[q]; r != nil {
			found = r
			return
		}
		for i := len(c.requests) - 1; i >= 0; i-- {
			if strings.Contains(c.requests[i].URL, q) {
				found = c.requests[i]
				return
			}
		}
	})
	switch {
	case found != nil:
		return *found, nil
	case none:
		return netRequest{}, fmt.Errorf("no requests captured yet: reload the page or repeat the action, then ask again")
	}
	return netRequest{}, fmt.Errorf("no captured request with id or URL containing %q", q)
}

// Initiator says what started a request: a script stack, the HTML parser or
// something else, as the Network panel's Initiator column does.
func (p *Page) Initiator(q string) (string, error) {
	r, err := p.findRequest(q)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s %s\ninitiator: %s", r.Method, r.URL, cmp.Or(r.Initiator, "unknown")), nil
}

// RequestCookies lists the cookies a request sent, the ones withheld and why,
// and the Set-Cookie headers its response tried that were refused.
func (p *Page) RequestCookies(q string) (string, error) {
	r, err := p.findRequest(q)
	if err != nil {
		return "", err
	}
	lines := []string{fmt.Sprintf("%s %s", r.Method, r.URL)}
	list := func(label string, items []string) {
		if len(items) == 0 {
			lines = append(lines, label+": none")
			return
		}
		lines = append(lines, label+":")
		for _, it := range items {
			lines = append(lines, "  "+it)
		}
	}
	list("sent", r.Sent)
	list("not sent", r.Blocked)
	list("Set-Cookie refused", r.SetBlocked)
	return strings.Join(lines, "\n"), nil
}

// secretHeaders are left out of copied commands; replay sends them.
var secretHeaders = map[string]bool{"authorization": true, "cookie": true, "proxy-authorization": true, "x-api-key": true, "x-auth-token": true}

// CopyRequest renders a captured request as a curl command or a fetch() call,
// like "Copy as cURL". Credentials are redacted.
func (p *Page) CopyRequest(q, format string) (string, error) {
	r, err := p.findRequest(q)
	if err != nil {
		return "", err
	}
	names := make([]string, 0, len(r.Headers))
	for k := range r.Headers {
		if !strings.HasPrefix(k, ":") {
			names = append(names, k)
		}
	}
	slices.Sort(names)
	redacted := false
	header := func(k string) string {
		if secretHeaders[strings.ToLower(k)] {
			redacted = true
			return "<redacted>"
		}
		return r.Headers[k]
	}
	var b strings.Builder
	if format == "fetch" {
		h := map[string]string{}
		for _, k := range names {
			// fetch() may not set these; the browser adds them itself.
			if lk := strings.ToLower(k); browserSetHeaders[lk] || strings.HasPrefix(lk, "sec-") {
				continue
			}
			h[k] = header(k)
		}
		opts := map[string]any{"method": r.Method, "headers": h}
		if r.PostData != "" {
			opts["body"] = r.PostData
		}
		var js bytes.Buffer
		enc := json.NewEncoder(&js)
		enc.SetEscapeHTML(false) // keep <redacted> readable in the copied code
		enc.SetIndent("", "  ")
		enc.Encode(opts)
		fmt.Fprintf(&b, "await fetch(%s, %s);", jsQuote(r.URL), strings.TrimSpace(js.String()))
	} else {
		fmt.Fprintf(&b, "curl %s", shellQuote(r.URL))
		if r.Method != "GET" {
			fmt.Fprintf(&b, " \\\n  -X %s", r.Method)
		}
		for _, k := range names {
			fmt.Fprintf(&b, " \\\n  -H %s", shellQuote(k+": "+header(k)))
		}
		if r.PostData != "" {
			fmt.Fprintf(&b, " \\\n  --data-raw %s", shellQuote(r.PostData))
		}
	}
	if redacted {
		b.WriteString("\n(credentials redacted; network replay resends the request with them)")
	}
	return b.String(), nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ReplayRequest sends a captured XHR or fetch again and reports the new
// response, as "Replay XHR" does. Chrome only replays XHRs itself, so a
// fetch is re-issued from the page with the same method, headers and body.
func (p *Page) ReplayRequest(ctx context.Context, q string) (string, error) {
	r, err := p.findRequest(q)
	if err != nil {
		return "", err
	}
	switch r.Type {
	case "Fetch":
		status, err := p.evaluateOn(ctx, p.sessionID, nil, refetchJS(r))
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("replayed %s %s: %s (was %d)", r.Method, r.URL, status, r.Status), nil
	case "XHR":
	default:
		return "", fmt.Errorf("only XHR and fetch requests can be replayed; %s is %s", r.URL, r.Type)
	}
	before := p.captured()
	if err := p.call(ctx, "Network.replayXHR", map[string]any{"requestId": r.ID}, nil); err != nil {
		return "", err
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var done *netRequest
		p.withCapture(func(c *netCapture) {
			for _, n := range c.requests[min(before, len(c.requests)):] {
				if n.URL == r.URL && n.Finished {
					done = n
				}
			}
		})
		if done != nil {
			if done.Failed != "" {
				return fmt.Sprintf("replayed %s %s: failed (%s)", r.Method, r.URL, done.Failed), nil
			}
			return fmt.Sprintf("replayed %s %s: %d %s (was %d)", r.Method, r.URL, done.Status, done.MimeType, r.Status), nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Sprintf("replayed %s %s; no response within 5s", r.Method, r.URL), nil
}

// browserSetHeaders are headers fetch() may not set; the browser adds them.
var browserSetHeaders = map[string]bool{"cookie": true, "host": true, "content-length": true, "origin": true, "referer": true,
	"user-agent": true, "accept-encoding": true, "connection": true}

// refetchJS issues a captured fetch again and resolves to "status type".
func refetchJS(r netRequest) string {
	headers := map[string]string{}
	for k, v := range r.Headers {
		lk := strings.ToLower(k)
		if !strings.HasPrefix(k, ":") && !browserSetHeaders[lk] && !strings.HasPrefix(lk, "sec-") {
			headers[k] = v
		}
	}
	opts := map[string]any{"method": r.Method, "headers": headers, "credentials": "include", "cache": "no-store"}
	if r.PostData != "" && r.Method != "GET" && r.Method != "HEAD" {
		opts["body"] = r.PostData
	}
	js, _ := json.Marshal(opts)
	// await: evaluateOn runs in REPL mode, which only awaits top-level await.
	return fmt.Sprintf(`await fetch(%s, %s).then((r) => r.status + " " + (r.headers.get("content-type") || "").split(";")[0])`, jsQuote(r.URL), js)
}

func (p *Page) captured() int {
	n := 0
	p.withCapture(func(c *netCapture) { n = len(c.requests) })
	return n
}

// SearchResponses looks for text in the bodies of captured responses, like
// the Network panel's search across all responses.
func (p *Page) SearchResponses(ctx context.Context, query string, limit int) (string, error) {
	var candidates []netRequest
	var seen []string
	p.withCapture(func(c *netCapture) {
		for i := len(c.requests) - 1; i >= 0 && len(candidates) < 100; i-- {
			r := c.requests[i]
			// Not r.Finished: a fetch whose body the page never reads does not finish.
			if r.Status > 0 && r.Failed == "" && textual(r.MimeType) {
				candidates = append(candidates, *r)
			} else if len(seen) < 5 {
				seen = append(seen, fmt.Sprintf("%s %s (status %d, %s, finished %v)", r.Method, r.URL, r.Status, cmp.Or(r.MimeType, "no type"), r.Finished))
			}
		}
	})
	if len(candidates) == 0 {
		msg := "no text responses captured yet: reload the page or repeat the action, then search"
		if len(seen) > 0 {
			msg += "; captured: " + strings.Join(seen, "; ")
		}
		return "", fmt.Errorf("%s", msg)
	}
	var lines []string
	for _, r := range candidates {
		var res struct {
			Body          string `json:"body"`
			Base64Encoded bool   `json:"base64Encoded"`
		}
		if p.call(ctx, "Network.getResponseBody", map[string]any{"requestId": r.ID}, &res) != nil {
			continue // evicted from the browser's buffer
		}
		body := res.Body
		if res.Base64Encoded {
			b, err := base64.StdEncoding.DecodeString(body)
			if err != nil {
				continue
			}
			body = string(b)
		}
		if i := strings.Index(body, query); i >= 0 {
			start, end := max(0, i-60), min(len(body), i+len(query)+60)
			lines = append(lines, fmt.Sprintf("%s  …%s…", r.URL, strings.ReplaceAll(body[start:end], "\n", " ")))
			if len(lines) == limit {
				break
			}
		}
	}
	if len(lines) == 0 {
		return fmt.Sprintf("%q is in none of the last %d text responses", query, len(candidates)), nil
	}
	return strings.Join(lines, "\n"), nil
}

func textual(mime string) bool {
	return strings.HasPrefix(mime, "text/") || strings.Contains(mime, "json") || strings.Contains(mime, "javascript") ||
		strings.Contains(mime, "xml") || strings.Contains(mime, "html")
}
