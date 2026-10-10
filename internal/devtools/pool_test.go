package devtools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/xcode-studio/agentbrowser-mcp/internal/browser"
	"github.com/xcode-studio/agentbrowser-mcp/internal/config"
	"github.com/xcode-studio/agentbrowser-mcp/internal/testutil/fakecdp"
	"github.com/xcode-studio/agentbrowser-mcp/internal/testutil/fakecli"
)

func TestMain(m *testing.M) {
	fakecli.MaybeRun()
	os.Exit(m.Run())
}

func newPool(t *testing.T, url string) (*Pool, *fakecli.Fake, *fakecdp.Server) {
	t.Helper()
	fake := fakecli.Install(t)
	s := fakecdp.New(t, url)
	fake.SetURL(url)
	fake.Respond("get cdp-url", fmt.Sprintf(`{"cdpUrl":%q}`, s.WSURL()))
	pool := NewPool(browser.NewManager(&config.Config{AgentBrowserPath: fake.Path, DefaultTimeout: 10000}))
	t.Cleanup(pool.Close)
	return pool, fake, s
}

func TestPoolReusesConnectionForTheSameTab(t *testing.T) {
	t.Parallel()
	pool, _, _ := newPool(t, "http://x/")
	ctx := context.Background()
	first, err := pool.Page(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := pool.Page(ctx, "")
	if err != nil || second != first {
		t.Fatalf("want the same page, got %p vs %p (%v)", second, first, err)
	}
	if pool.Existing("") != first {
		t.Fatal("Existing should return the open page")
	}
}

func TestPoolKeepsConnectionWhenTheTabNavigates(t *testing.T) {
	t.Parallel()
	pool, fake, s := newPool(t, "http://x/")
	ctx := context.Background()
	first, err := pool.Page(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	s.SetPageURL("http://x/next")
	fake.SetURL("http://x/next")
	if second, err := pool.Page(ctx, ""); err != nil || second != first {
		t.Fatalf("navigating within the tab must keep throttling/coverage/breakpoints, got new page (%v)", err)
	}
}

func TestPoolReattachesWhenAnotherTabBecomesActive(t *testing.T) {
	t.Parallel()
	pool, fake, s := newPool(t, "http://x/")
	ctx := context.Background()
	first, err := pool.Page(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	s.AddPage("http://x/other")
	fake.SetActiveTab("T2")
	second, err := pool.Page(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if second == first || first.conn.Alive() || second.targetID != "T2" || second.sessionID != "S2" {
		t.Fatalf("want a new connection attached to T2/S2, got %s/%s (old alive=%v)", second.targetID, second.sessionID, first.conn.Alive())
	}
}

func TestPoolFailsWhenTheActiveTabIsMissing(t *testing.T) {
	t.Parallel()
	pool, fake, _ := newPool(t, "http://x/")
	fake.SetActiveTab("T9")
	_, err := pool.Page(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "the active tab T9 is not among the browser's tabs") {
		t.Fatalf("got %v", err)
	}
}

func TestPoolSessionsAreSeparate(t *testing.T) {
	t.Parallel()
	pool, _, _ := newPool(t, "http://x/")
	ctx := context.Background()
	a, _ := pool.Page(ctx, "a")
	b, _ := pool.Page(ctx, "b")
	if a == nil || b == nil || a == b {
		t.Fatal("each session needs its own page")
	}
	pool.Forget("a")
	if pool.Existing("a") != nil || pool.Existing("b") != b {
		t.Fatal("Forget must only drop its own session")
	}
}

func TestPoolReportsCLIErrors(t *testing.T) {
	t.Parallel()
	pool, fake, _ := newPool(t, "http://x/")
	fake.FailOn("get")
	if _, err := pool.Page(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "fake failure") {
		t.Fatalf("got %v", err)
	}
}

func TestPoolKeepsAttachedTabWhenTheCLICannotAnswer(t *testing.T) {
	t.Parallel()
	pool, fake, _ := newPool(t, "http://x/")
	ctx := context.Background()
	first, err := pool.Page(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	fake.FailOn("tab") // e.g. agent-browser blocked by a hung page
	if second, err := pool.Page(ctx, ""); err != nil || second != first {
		t.Fatalf("want the attached page despite the CLI error, got %v", err)
	}
}

// A page paused in the debugger: agent-browser cannot read its URL, but tab
// list still names the active tab, even among several.
func TestPoolFindsThePausedTabByTargetID(t *testing.T) {
	t.Parallel()
	pool, fake, s := newPool(t, "http://x/")
	s.AddPage("http://x/")
	fake.SetActiveTab("T2")
	fake.FailOn("get url") // "CDP error (Runtime.evaluate): Promise was collected"
	p, err := pool.Page(context.Background(), "")
	if err != nil || p.targetID != "T2" {
		t.Fatalf("want the active tab T2, got %v", err)
	}
}

// When agent-browser cannot list tabs at all, the browser's only tab is used.
func TestPoolFindsTheOnlyTabWhenTheCLICannotNameIt(t *testing.T) {
	t.Parallel()
	pool, fake, s := newPool(t, "http://x/")
	s.AddPage("devtools://devtools/bundled/devtools_app.html") // a docked DevTools window
	fake.FailOn("tab")
	p, err := pool.Page(context.Background(), "")
	if err != nil || p.targetID != "T1" {
		t.Fatalf("want the only real tab T1, got %v", err)
	}
}

func TestPoolCannotGuessAmongSeveralTabs(t *testing.T) {
	t.Parallel()
	pool, fake, s := newPool(t, "http://x/")
	s.AddPage("http://y/")
	fake.FailOn("tab")
	_, err := pool.Page(context.Background(), "")
	if err == nil || !strings.Contains(err.Error(), "cannot tell which of 2 tabs is active") || !strings.Contains(err.Error(), "fake failure") {
		t.Fatalf("got %v", err)
	}
}

// A tab closed while the debugger was on must not pin the pool to a dead
// session: the next call attaches to the session's current tab and says why
// the breakpoints are gone.
func TestPoolReattachesWhenTheDebuggedTabCloses(t *testing.T) {
	t.Parallel()
	pool, fake, s := newPool(t, "http://x/")
	ctx := context.Background()
	first, err := pool.Page(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := first.EnableDebugger(ctx); err != nil {
		t.Fatal(err)
	}
	s.Push(fakecdp.Event{Method: "Target.detachedFromTarget", Params: map[string]any{"sessionId": "S1", "targetId": "T1"}})
	s.AddPage("about:blank")
	fake.SetActiveTab("T2")
	waitFor(t, func() bool { return pool.Existing("") == nil })

	second, err := pool.Page(ctx, "")
	if err != nil || second.targetID != "T2" {
		t.Fatalf("want the new active tab T2, got %v", err)
	}
	if n := pool.Notice(""); !strings.Contains(n, "the tab being debugged was closed") {
		t.Fatalf("notice %q", n)
	}
	if pool.Notice("") != "" {
		t.Fatal("the notice is reported once")
	}
}

func TestPageNoticesItsSessionIsGone(t *testing.T) {
	t.Parallel()
	pool, _, s := newPool(t, "http://x/")
	p, err := pool.Page(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	s.Reply("Runtime.evaluate", fakecdp.Reply{Error: "Session with given id not found."})
	if _, err := p.Evaluate(context.Background(), "1", 0); !errors.Is(err, ErrTabClosed) {
		t.Fatalf("got %v", err)
	}
	if pool.Existing("") != nil {
		t.Fatal("a page whose session is gone must not be reused")
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for range 100 {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition not met within 1s")
}
