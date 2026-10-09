package tools

import (
	"context"
	"encoding/base64"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/vercel-labs/agent-browser-mcp/internal/config"
	"github.com/vercel-labs/agent-browser-mcp/internal/testutil/fakecli"
)

// One test per bug found in the 1.x server.

func TestRegressionBackForwardUseSession(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"back", "forward"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			e.call("navigate", a{"action": action, "session": "work"})
			calls := e.fake.Calls()
			if len(calls) != 1 || !hasFlag(calls[0], "--session", "work") {
				t.Fatalf("navigate %s must run in session work, got %q", action, calls)
			}
		})
	}
}

func TestRegressionUploadSendsEachFileSeparately(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.call("upload_file", a{"selector": "#f", "files": []any{"/a.txt", "/b.txt"}})
	got := e.fake.Commands()
	want := cmds(cmd("upload", "#f", "/a.txt", "/b.txt"))
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestRegressionUserAgentKeepsCurrentPage(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.fake.SetURL("https://app.example/dashboard")
	res := e.call("emulate", a{"userAgent": "Bot/1"})
	if res.IsError {
		t.Fatal(res.text())
	}
	for _, c := range e.fake.Commands() {
		if slices.Contains(c, "about:blank") {
			t.Fatalf("user agent change navigated to about:blank: %q", c)
		}
	}
	last := e.fake.Commands()[len(e.fake.Commands())-1]
	if last[len(last)-1] != "https://app.example/dashboard" {
		t.Fatalf("expected to reopen the current page, got %q", last)
	}
}

func TestRegressionTimeoutIsReported(t *testing.T) {
	t.Parallel()
	e := newEnv(t, func(c *config.Config) { c.DefaultTimeout = 200 })
	e.fake.SleepMS(3000, "click")
	res := e.call("click", a{"selector": "@e1"})
	if !res.IsError || !strings.Contains(res.text(), "timed out after") {
		t.Fatalf("want a timeout error, got isError=%v %q", res.IsError, res.text())
	}
}

func TestRegressionShutdownClosesDefaultSession(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.call("click", a{"selector": "@e1"})
	e.call("click", a{"selector": "@e1", "session": "other"})
	e.fake.Reset()

	e.mgr.CloseAll(context.Background())

	var closedDefault, closedOther bool
	for _, c := range e.fake.Calls() {
		if !reflect.DeepEqual(fakecli.Command(c), []string{"close"}) {
			continue
		}
		switch {
		case hasFlag(c, "--session", "other"):
			closedOther = true
		case !slices.Contains(c, "--session"):
			closedDefault = true
		}
	}
	if !closedDefault || !closedOther {
		t.Fatalf("closed default=%v other=%v; calls %q", closedDefault, closedOther, e.fake.Calls())
	}
}

func TestRegressionScreenshotSelectorIsPositional(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.call("screenshot", a{"selector": "#hero"})
	c := e.fake.Commands()[0]
	if slices.Contains(c, "--selector") || c[len(c)-1] != "#hero" {
		t.Fatalf("the CLI takes the screenshot selector positionally, got %q", c)
	}
}

func TestRegressionFindAllReturnsTexts(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	res := e.call("find", a{"by": "all", "value": ".item"})
	if res.IsError || res.text() != `["one","two"]` {
		t.Fatalf("got isError=%v %q", res.IsError, res.text())
	}
}

func TestRegressionSnapshotIsPlainTree(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	res := e.call("snapshot", nil)
	if got := res.text(); got != `- button "Go" [ref=e1]` {
		t.Fatalf("snapshot should be the bare tree without refs map or JSON escaping, got %q", got)
	}
}

func TestRegressionScreenshotReturnsImage(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	res := e.call("screenshot", nil)
	if res.images() != 1 {
		t.Fatalf("want 1 image block, got %+v", res.Content)
	}
	img := res.Content[0]
	if img.MimeType != "image/jpeg" {
		t.Fatalf("mime = %q", img.MimeType)
	}
	if raw, _ := base64.StdEncoding.DecodeString(img.Data); !reflect.DeepEqual(raw, fakecli.TinyPNG()) {
		t.Fatal("image data does not match the file the CLI wrote")
	}
}

func TestRegressionGetURLIsPlain(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.fake.SetURL("http://x/y")
	if got := e.call("get", a{"what": "url"}).text(); got != "url: http://x/y" {
		t.Fatalf("got %q", got)
	}
}

// agent-browser reports success for clicks, fills, typing and hovers on a
// display:none element even though nothing receives them, e.g. a desktop-only
// toggle after docked DevTools narrowed the page into its mobile layout.
func TestRegressionActionsOnHiddenElementsAreRefused(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		tool  string
		args  a
		doing string
	}{
		{"click", a{"selector": "@e26", "snapshot": "delta"}, "clicking it"},
		{"click", a{"selector": "@e26", "double": true}, "clicking it"},
		{"fill", a{"selector": "@e26", "value": "x"}, "filling it"},
		{"type", a{"selector": "@e26", "text": "x"}, "typing into it"},
		{"element_action", a{"selector": "@e26", "action": "hover"}, "hovering over it"},
	} {
		t.Run(c.tool+"_"+c.doing, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			e.fake.Respond("is visible", `{"visible":false}`)
			res := e.call(c.tool, c.args)
			want := "@e26 is not visible (display:none, visibility:hidden or zero size), so " + c.doing + " would do nothing"
			if !res.IsError || !strings.HasPrefix(res.text(), want) || !strings.Contains(res.text(), "take a fresh snapshot") {
				t.Fatalf("want a not-visible error, got isError=%v %q", res.IsError, res.text())
			}
			if got := e.fake.Commands(); !reflect.DeepEqual(got, cmds(visible("@e26"))) {
				t.Fatalf("a hidden target must not be acted on, got %q", got)
			}
		})
	}
}

// Actions that work on hidden elements, or have no element target, skip the check.
func TestActionsThatNeedNoVisibleTarget(t *testing.T) {
	t.Parallel()
	for name, c := range map[string]struct {
		tool string
		args a
		runs string // the CLI command the call makes; "" for none
	}{
		"new tab link":  {"click", a{"selector": "@e1", "newTab": true}, "click"},
		"keystrokes":    {"type", a{"text": "x", "mode": "keystrokes"}, "keyboard"},
		"check":         {"element_action", a{"selector": "@e1", "action": "check"}, "check"},
		"select":        {"select_option", a{"selector": "@e1", "values": []any{"b"}}, "select"},
		"invalid input": {"fill", a{"selector": "@e1"}, ""},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			e.fake.Respond("is visible", `{"visible":false}`)
			e.call(c.tool, c.args)
			var ran []string
			for _, cmd := range e.fake.Commands() {
				ran = append(ran, cmd[0])
			}
			want := []string{}
			if c.runs != "" {
				want = []string{c.runs}
			}
			if !slices.Equal(ran, want) {
				t.Fatalf("%s: CLI commands %q, want %q with no visibility check", name, ran, want)
			}
		})
	}
}

func TestClickProceedsWhenVisibilityIsUnknown(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.fake.FailOn("is") // e.g. "Unknown ref": let click report it
	if res := e.call("click", a{"selector": "@e1"}); res.IsError {
		t.Fatalf("click must still run, got %q", res.text())
	}
	if got := e.fake.Commands(); !reflect.DeepEqual(got, cmds(visible("@e1"), cmd("click", "@e1"))) {
		t.Fatalf("got %q", got)
	}
}
