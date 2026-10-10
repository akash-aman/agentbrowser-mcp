package tools

import (
	"strings"
	"testing"
	"time"

	"github.com/xcode-studio/agentbrowser-mcp/internal/config"
)

const restartedWant = "Note: this session's browser had closed since its last use (browsers close after 15m without commands), so this call started a fresh one; earlier tabs, page state and @refs are gone."

// TestNoteWhenTheBrowserRestarted: once the idle timeout has closed a
// browser, the next call runs in a fresh one where the model's old page and
// @refs no longer exist.
func TestNoteWhenTheBrowserRestarted(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(c *config.Config) { c.IdleTimeout = 15 * time.Minute })
	if got := e.call("get", a{"what": "url"}).text(); strings.Contains(got, "had closed") {
		t.Fatalf("first use is not a restart: %q", got)
	}
	e.fake.Respond("get", `{"url":"about:blank","lifecycle":{"launched":true}}`)
	if got := e.call("get", a{"what": "url"}).text(); !strings.HasPrefix(got, restartedWant) {
		t.Fatalf("got %q, want it to start with %q", got, restartedWant)
	}
	e.fake.Respond("get", `{"url":"about:blank"}`)
	if got := e.call("get", a{"what": "url"}).text(); strings.Contains(got, "had closed") {
		t.Fatalf("the note is given once: %q", got)
	}
}

func TestRestartNoteInsideBatch(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(c *config.Config) { c.IdleTimeout = 15 * time.Minute })
	e.call("get", a{"what": "url"})
	// Only the first step's call reports the launch.
	e.fake.Respond("get url", `{"url":"about:blank","lifecycle":{"launched":true}}`)
	got := e.call("batch", a{"steps": []any{
		map[string]any{"tool": "get", "args": map[string]any{"what": "url"}},
		map[string]any{"tool": "tabs", "args": map[string]any{"action": "list"}},
	}}).text()
	if n := strings.Count(got, "had closed"); n != 1 {
		t.Fatalf("want the note once, in the step that restarted, got %d:\n%s", n, got)
	}
}

func TestShortDuration(t *testing.T) {
	t.Parallel()
	for d, want := range map[time.Duration]string{
		15 * time.Minute:       "15m",
		time.Hour:              "1h",
		90 * time.Minute:       "1h30m",
		90 * time.Second:       "1m30s",
		10 * time.Second:       "10s",
		500 * time.Millisecond: "500ms",
	} {
		if got := shortDuration(d); got != want {
			t.Errorf("shortDuration(%v) = %q, want %q", d, got, want)
		}
	}
}
