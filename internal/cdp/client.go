// Package cdp is a minimal Chrome DevTools Protocol client: one WebSocket
// connection, request/response matching, event subscriptions, and flattened
// target sessions so one connection can drive a specific tab.
package cdp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/coder/websocket"
)

// maxMessage bounds one CDP message; heap snapshot chunks and coverage
// results can be several megabytes.
const maxMessage = 512 << 20

// ErrClosed is returned for calls on a connection that has shut down.
var ErrClosed = errors.New("cdp connection closed")

// Error is a protocol-level error returned by the browser.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data,omitempty"`
}

func (e *Error) Error() string {
	if e.Data != "" {
		return fmt.Sprintf("%s (%s)", e.Message, e.Data)
	}
	return e.Message
}

// Event is a protocol event, tagged with the target session that sent it.
type Event struct {
	SessionID string
	Method    string
	Params    json.RawMessage
}

type message struct {
	ID        int64           `json:"id,omitempty"`
	Method    string          `json:"method,omitempty"`
	Params    json.RawMessage `json:"params,omitempty"`
	SessionID string          `json:"sessionId,omitempty"`
	Result    json.RawMessage `json:"result,omitempty"`
	Error     *Error          `json:"error,omitempty"`
}

// Conn is a CDP connection. Calls may be made concurrently.
type Conn struct {
	ws     *websocket.Conn
	nextID atomic.Int64

	mu       sync.Mutex
	pending  map[int64]chan message
	handlers map[int64]handler
	nextSub  int64

	done chan struct{}
	err  error
}

type handler struct {
	method string
	fn     func(Event)
}

// Dial connects to a CDP WebSocket URL.
func Dial(ctx context.Context, url string) (*Conn, error) {
	ws, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", url, err)
	}
	ws.SetReadLimit(maxMessage)
	c := &Conn{
		ws:       ws,
		pending:  make(map[int64]chan message),
		handlers: make(map[int64]handler),
		done:     make(chan struct{}),
	}
	go c.readLoop()
	return c, nil
}

// Alive reports whether the connection is still open.
func (c *Conn) Alive() bool {
	select {
	case <-c.done:
		return false
	default:
		return true
	}
}

// Close shuts the connection down and fails pending calls.
func (c *Conn) Close() error {
	err := c.ws.Close(websocket.StatusNormalClosure, "")
	<-c.done
	return err
}

// Call sends method to the target session (empty for the browser) and decodes
// the result into result, which may be nil.
func (c *Conn) Call(ctx context.Context, sessionID, method string, params, result any) error {
	raw, err := marshalParams(params)
	if err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}
	id := c.nextID.Add(1)
	reply, err := c.register(id)
	if err != nil {
		return err
	}
	defer c.unregister(id)

	data, err := json.Marshal(message{ID: id, Method: method, Params: raw, SessionID: sessionID})
	if err != nil {
		return err
	}
	if err := c.ws.Write(ctx, websocket.MessageText, data); err != nil {
		return fmt.Errorf("%s: %w", method, err)
	}

	select {
	case msg := <-reply:
		return decodeReply(method, msg, result)
	case <-ctx.Done():
		return fmt.Errorf("%s: %w", method, ctx.Err())
	case <-c.done:
		return fmt.Errorf("%s: %w", method, c.err)
	}
}

// On subscribes fn to events named method from any session and returns a
// function that unsubscribes. fn runs on the read loop, so it must not block
// or make calls on this connection.
func (c *Conn) On(method string, fn func(Event)) (unsubscribe func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.nextSub++
	id := c.nextSub
	c.handlers[id] = handler{method: method, fn: fn}
	return func() {
		c.mu.Lock()
		defer c.mu.Unlock()
		delete(c.handlers, id)
	}
}

func marshalParams(params any) (json.RawMessage, error) {
	if params == nil {
		return nil, nil
	}
	return json.Marshal(params)
}

func decodeReply(method string, msg message, result any) error {
	if msg.Error != nil {
		return fmt.Errorf("%s: %w", method, msg.Error)
	}
	if result == nil || len(msg.Result) == 0 {
		return nil
	}
	if err := json.Unmarshal(msg.Result, result); err != nil {
		return fmt.Errorf("%s: decode result: %w", method, err)
	}
	return nil
}

func (c *Conn) register(id int64) (chan message, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.Alive() {
		return nil, c.err
	}
	ch := make(chan message, 1)
	c.pending[id] = ch
	return ch, nil
}

func (c *Conn) unregister(id int64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.pending, id)
}

func (c *Conn) readLoop() {
	for {
		_, data, err := c.ws.Read(context.Background())
		if err != nil {
			c.shutdown(err)
			return
		}
		var msg message
		if json.Unmarshal(data, &msg) != nil {
			continue
		}
		if msg.ID != 0 {
			c.deliver(msg)
		} else if msg.Method != "" {
			c.dispatch(Event{SessionID: msg.SessionID, Method: msg.Method, Params: msg.Params})
		}
	}
}

func (c *Conn) deliver(msg message) {
	c.mu.Lock()
	ch := c.pending[msg.ID]
	c.mu.Unlock()
	if ch != nil {
		ch <- msg
	}
}

func (c *Conn) dispatch(ev Event) {
	c.mu.Lock()
	var fns []func(Event)
	for _, h := range c.handlers {
		if h.method == ev.Method {
			fns = append(fns, h.fn)
		}
	}
	c.mu.Unlock()
	for _, fn := range fns {
		fn(ev)
	}
}

func (c *Conn) shutdown(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.err = fmt.Errorf("%w: %v", ErrClosed, err)
	close(c.done)
}
