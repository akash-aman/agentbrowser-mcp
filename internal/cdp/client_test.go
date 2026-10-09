package cdp

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/vercel-labs/agent-browser-mcp/internal/testutil/fakecdp"
)

func dial(t *testing.T, s *fakecdp.Server) *Conn {
	t.Helper()
	c, err := Dial(context.Background(), s.WSURL())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestCallDecodesResult(t *testing.T) {
	t.Parallel()
	s := fakecdp.New(t, "http://fake/")
	s.Reply("Runtime.evaluate", fakecdp.Reply{Result: map[string]any{"result": map[string]any{"value": 42}}})
	c := dial(t, s)

	var res struct {
		Result struct {
			Value int `json:"value"`
		} `json:"result"`
	}
	if err := c.Call(context.Background(), fakecdp.SessionID, "Runtime.evaluate", map[string]any{"expression": "6*7"}, &res); err != nil {
		t.Fatal(err)
	}
	if res.Result.Value != 42 {
		t.Fatalf("got %d", res.Result.Value)
	}
	if got := s.Params("Runtime.evaluate")["expression"]; got != "6*7" {
		t.Fatalf("params not sent: %v", got)
	}
}

func TestCallReturnsProtocolError(t *testing.T) {
	t.Parallel()
	s := fakecdp.New(t, "http://fake/")
	s.Reply("Debugger.resume", fakecdp.Reply{Error: "Can only perform operation while paused."})
	err := dial(t, s).Call(context.Background(), fakecdp.SessionID, "Debugger.resume", nil, nil)
	var protoErr *Error
	if !errors.As(err, &protoErr) || protoErr.Message != "Can only perform operation while paused." {
		t.Fatalf("got %v", err)
	}
}

func TestEventsReachSubscribersBeforeTheResponse(t *testing.T) {
	t.Parallel()
	s := fakecdp.New(t, "http://fake/")
	s.Reply("HeapProfiler.takeHeapSnapshot", fakecdp.Reply{Events: []fakecdp.Event{
		{Method: "HeapProfiler.addHeapSnapshotChunk", Params: map[string]any{"chunk": "a"}},
		{Method: "HeapProfiler.addHeapSnapshotChunk", Params: map[string]any{"chunk": "b"}},
	}})
	c := dial(t, s)
	var mu sync.Mutex
	var got []string
	unsubscribe := c.On("HeapProfiler.addHeapSnapshotChunk", func(ev Event) {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, string(ev.Params)+"@"+ev.SessionID)
	})
	if err := c.Call(context.Background(), fakecdp.SessionID, "HeapProfiler.takeHeapSnapshot", nil, nil); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	if len(got) != 2 || got[0] != `{"chunk":"a"}@S1` {
		t.Fatalf("got %q", got)
	}
	mu.Unlock()

	unsubscribe()
	c.Call(context.Background(), fakecdp.SessionID, "HeapProfiler.takeHeapSnapshot", nil, nil)
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 2 {
		t.Fatalf("unsubscribed handler still called: %q", got)
	}
}

func TestCallHonoursContext(t *testing.T) {
	t.Parallel()
	s := fakecdp.New(t, "http://fake/")
	block := make(chan struct{})
	defer close(block) // unblock the server before cleanup closes the connection
	s.Handle("Slow.method", func(map[string]any) fakecdp.Reply { <-block; return fakecdp.Reply{} })
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := dial(t, s).Call(ctx, "", "Slow.method", nil, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("got %v", err)
	}
}

func TestClosedConnection(t *testing.T) {
	t.Parallel()
	s := fakecdp.New(t, "http://fake/")
	c, err := Dial(context.Background(), s.WSURL())
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
	if c.Alive() {
		t.Fatal("closed connection reports alive")
	}
	if err := c.Call(context.Background(), "", "X.y", nil, nil); !errors.Is(err, ErrClosed) {
		t.Fatalf("got %v", err)
	}
}

func TestDialFailure(t *testing.T) {
	t.Parallel()
	if _, err := Dial(context.Background(), "ws://127.0.0.1:1/devtools/browser/x"); err == nil {
		t.Fatal("want error")
	}
}

func TestTargets(t *testing.T) {
	t.Parallel()
	s := fakecdp.New(t, "http://fake/page")
	c := dial(t, s)
	ctx := context.Background()
	pages, err := c.Pages(ctx)
	if err != nil || len(pages) != 1 || pages[0].URL != "http://fake/page" {
		t.Fatalf("pages %+v, %v", pages, err)
	}
	if info, err := c.Target(ctx, "T1"); err != nil || info.URL != "http://fake/page" {
		t.Fatalf("target %+v, %v", info, err)
	}
	if id, err := c.Attach(ctx, "T1"); err != nil || id != fakecdp.SessionID {
		t.Fatalf("attach %q, %v", id, err)
	}
}

func TestPort(t *testing.T) {
	t.Parallel()
	if p, err := Port("ws://127.0.0.1:9222/devtools/browser/x"); err != nil || p != 9222 {
		t.Fatalf("got %d, %v", p, err)
	}
	if _, err := Port("ws://localhost/devtools"); err == nil {
		t.Fatal("URL without port must fail")
	}
}
