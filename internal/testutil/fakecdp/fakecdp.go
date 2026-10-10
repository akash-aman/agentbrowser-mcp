// Package fakecdp is a scriptable Chrome DevTools Protocol server for tests.
// It answers the Target.* calls a client needs to attach to a tab, records
// every other call, and replies with per-method results and events.
package fakecdp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// SessionID is the session the fake hands out for its single page.
const SessionID = "S1"

// Event is a protocol event to send.
type Event struct {
	Method string
	Params any
}

// Reply is a method's response; Events are sent before the result, as Chrome
// does for e.g. heap snapshot chunks.
type Reply struct {
	Result any
	Events []Event
	Error  string
	Code   int // protocol error code; default -32000
	// Delay holds the result back while the server goes on answering other
	// calls, as Chrome does for a script stopped at a breakpoint.
	Delay time.Duration
}

// Handler answers one call.
type Handler func(params map[string]any) Reply

// Call is a recorded page-session call.
type Call struct {
	Method string
	Params map[string]any
}

// Server is the fake browser.
type Server struct {
	http *httptest.Server

	mu       sync.Mutex
	pages    []string          // URL of each tab; tab i has target T<i+1> and session S<i+1>
	titles   map[string]string // target id -> title; default "Fake"
	contexts map[string]string // target id -> browser context; default "DEFAULT"
	extra    []any             // other targets, such as workers and iframes
	handlers map[string]Handler
	calls    []Call
	conns    []*websocket.Conn
}

// New starts a fake browser with one page showing pageURL.
func New(t *testing.T, pageURL string) *Server {
	t.Helper()
	s := &Server{pages: []string{pageURL}, handlers: map[string]Handler{}}
	s.http = httptest.NewServer(http.HandlerFunc(s.serve))
	t.Cleanup(s.close)
	return s
}

// WSURL is the browser WebSocket URL, as agent-browser get cdp-url reports it.
func (s *Server) WSURL() string {
	return "ws" + strings.TrimPrefix(s.http.URL, "http") + "/devtools/browser/fake"
}

// SetPageURL changes the URL the first tab reports, as after a navigation.
func (s *Server) SetPageURL(url string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pages[0] = url
}

// AddPage opens another tab showing url.
func (s *Server) AddPage(url string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pages = append(s.pages, url)
}

// SetTitle sets the title Target.getTargets reports for a target.
func (s *Server) SetTitle(targetID, title string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.titles == nil {
		s.titles = map[string]string{}
	}
	s.titles[targetID] = title
}

// SetContext puts a target in another browser context than "DEFAULT".
func (s *Server) SetContext(targetID, browserContextID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.contexts == nil {
		s.contexts = map[string]string{}
	}
	s.contexts[targetID] = browserContextID
}

// AddTarget adds a non-page target (worker, iframe) to Target.getTargets.
func (s *Server) AddTarget(info map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.extra = append(s.extra, info)
}

// Handle sets the reply for a method.
func (s *Server) Handle(method string, h Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.handlers[method] = h
}

// Reply sets a fixed reply for a method.
func (s *Server) Reply(method string, r Reply) {
	s.Handle(method, func(map[string]any) Reply { return r })
}

// Calls returns the page-session calls received, in order.
func (s *Server) Calls() []Call {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Call(nil), s.calls...)
}

// PausedBanner is the call that shows or hides the "Paused in debugger"
// banner. It is sent in the background after each pause and resume, so
// Methods leaves it out to keep call orders exact.
const PausedBanner = "Overlay.setPausedInDebuggerMessage"

// Methods returns the method names of Calls, except PausedBanner.
func (s *Server) Methods() []string {
	var out []string
	for _, c := range s.Calls() {
		if c.Method != PausedBanner {
			out = append(out, c.Method)
		}
	}
	return out
}

// Params returns the params of the last call to method, or nil.
func (s *Server) Params(method string) map[string]any {
	calls := s.Calls()
	for i := len(calls) - 1; i >= 0; i-- {
		if calls[i].Method == method {
			return calls[i].Params
		}
	}
	return nil
}

// Push sends an event to every connected client, e.g. a Debugger.paused
// caused by something other than a call.
func (s *Server) Push(ev Event) {
	s.mu.Lock()
	conns := append([]*websocket.Conn(nil), s.conns...)
	s.mu.Unlock()
	for _, c := range conns {
		s.send(c, map[string]any{"method": ev.Method, "params": ev.Params, "sessionId": SessionID})
	}
}

func (s *Server) close() {
	s.mu.Lock()
	conns := s.conns
	s.conns = nil
	s.mu.Unlock()
	for _, c := range conns {
		c.Close(websocket.StatusGoingAway, "")
	}
	s.http.Close()
}

type request struct {
	ID        int64          `json:"id"`
	Method    string         `json:"method"`
	Params    map[string]any `json:"params"`
	SessionID string         `json:"sessionId"`
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	c.SetReadLimit(64 << 20)
	s.mu.Lock()
	s.conns = append(s.conns, c)
	s.mu.Unlock()
	for {
		_, data, err := c.Read(context.Background())
		if err != nil {
			return
		}
		var req request
		if json.Unmarshal(data, &req) != nil {
			continue
		}
		s.answer(c, req)
	}
}

var writeMu sync.Mutex

func (s *Server) send(c *websocket.Conn, msg map[string]any) {
	data, _ := json.Marshal(msg)
	writeMu.Lock()
	defer writeMu.Unlock()
	c.Write(context.Background(), websocket.MessageText, data)
}

func (s *Server) answer(c *websocket.Conn, req request) {
	reply := s.reply(req)
	for _, ev := range reply.Events {
		s.send(c, map[string]any{"method": ev.Method, "params": ev.Params, "sessionId": req.SessionID})
	}
	msg := map[string]any{"id": req.ID}
	if req.SessionID != "" {
		msg["sessionId"] = req.SessionID
	}
	if reply.Error != "" {
		code := reply.Code
		if code == 0 {
			code = -32000
		}
		msg["error"] = map[string]any{"code": code, "message": reply.Error}
	} else {
		result := reply.Result
		if result == nil {
			result = map[string]any{}
		}
		msg["result"] = result
	}
	if reply.Delay > 0 {
		go func() {
			time.Sleep(reply.Delay)
			s.send(c, msg)
		}()
		return
	}
	s.send(c, msg)
}

func (s *Server) reply(req request) Reply {
	s.mu.Lock()
	var pages []any
	for i, url := range s.pages {
		id := fmt.Sprintf("T%d", i+1)
		title, ok := s.titles[id]
		if !ok {
			title = "Fake"
		}
		browserContext, ok := s.contexts[id]
		if !ok {
			browserContext = "DEFAULT"
		}
		pages = append(pages, map[string]any{"targetId": id, "type": "page", "url": url, "title": title, "browserContextId": browserContext})
	}
	extra := append([]any(nil), s.extra...)
	s.mu.Unlock()
	switch req.Method {
	case "Target.getTargets":
		all := append(append(pages, extra...), map[string]any{"targetId": "T0", "type": "browser_ui", "url": "chrome://ui"})
		return Reply{Result: map[string]any{"targetInfos": all}}
	case "Target.attachToTarget":
		return Reply{Result: map[string]any{"sessionId": "S" + strings.TrimPrefix(fmt.Sprint(req.Params["targetId"]), "T")}}
	case "Target.getTargetInfo":
		for _, p := range pages {
			if p.(map[string]any)["targetId"] == req.Params["targetId"] {
				return Reply{Result: map[string]any{"targetInfo": p}}
			}
		}
		return Reply{Error: "No target with given id found"}
	}
	s.mu.Lock()
	s.calls = append(s.calls, Call{Method: req.Method, Params: req.Params})
	h := s.handlers[req.Method]
	s.mu.Unlock()
	if h == nil {
		return Reply{}
	}
	return h(req.Params)
}
