package tools

import (
	"reflect"
	"strings"
	"testing"
)

func TestBatchRunsStepsInOrder(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	res := e.call("batch", a{"steps": []any{
		a{"tool": "navigate", "args": a{"url": "http://x/"}},
		a{"tool": "fill", "args": a{"selector": "#q", "value": "hi"}},
		a{"tool": "press_key", "args": a{"key": "Enter"}},
	}})
	if res.IsError {
		t.Fatal(res.text())
	}
	want := cmds(cmd("open", "http://x/"), visible("#q"), cmd("fill", "#q", "hi"), cmd("press", "Enter"))
	if got := e.fake.Commands(); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	text := res.text()
	for _, s := range []string{"batch: 3/3 steps ran, 0 failed", "#1 navigate", "#2 fill", "#3 press_key"} {
		if !strings.Contains(text, s) {
			t.Errorf("result missing %q:\n%s", s, text)
		}
	}
}

func TestBatchContinuesAfterFailureByDefault(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.fake.FailOn("press")
	res := e.call("batch", a{"steps": []any{
		a{"tool": "press_key", "args": a{"key": "Enter"}},
		a{"tool": "get", "args": a{"what": "title"}},
	}})
	if res.IsError {
		t.Fatal("continue mode should not mark the batch as an error")
	}
	if len(e.fake.Commands()) != 2 {
		t.Fatalf("both steps should run, got %q", e.fake.Commands())
	}
	if !strings.Contains(res.text(), "#1 press_key FAILED") || !strings.Contains(res.text(), "1 failed") {
		t.Fatalf("failure not reported:\n%s", res.text())
	}
}

func TestBatchBailStopsAtFirstFailure(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.fake.FailOn("press")
	res := e.call("batch", a{"bail": true, "steps": []any{
		a{"tool": "press_key", "args": a{"key": "Enter"}},
		a{"tool": "get", "args": a{"what": "title"}},
	}})
	if !res.IsError {
		t.Fatal("bail with a failure must be an error")
	}
	if len(e.fake.Commands()) != 1 {
		t.Fatalf("second step must not run, got %q", e.fake.Commands())
	}
	if !strings.Contains(res.text(), "batch: 1/2 steps ran, 1 failed") {
		t.Fatalf("summary wrong:\n%s", res.text())
	}
}

func TestBatchRejectsBadSteps(t *testing.T) {
	t.Parallel()
	cases := map[string]struct {
		step any
		want string
	}{
		"nested":   {a{"tool": "batch", "args": a{"steps": []any{}}}, "batch cannot be nested"},
		"unknown":  {a{"tool": "teleport"}, `unknown tool "teleport"`},
		"no tool":  {a{"args": a{}}, "step is missing tool"},
		"not obj":  {"click", "step must be an object"},
		"bad args": {a{"tool": "click", "args": a{}}, "selector is required"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			e := newEnv(t)
			res := e.call("batch", a{"steps": []any{c.step}})
			if !strings.Contains(res.text(), c.want) || !strings.Contains(res.text(), "FAILED") {
				t.Fatalf("want %q, got:\n%s", c.want, res.text())
			}
			if len(e.fake.Commands()) != 0 {
				t.Fatalf("bad step must not reach the CLI, got %q", e.fake.Commands())
			}
		})
	}
}

func TestBatchSessionInheritance(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.call("batch", a{"session": "outer", "steps": []any{
		a{"tool": "press_key", "args": a{"key": "Enter"}},
		a{"tool": "press_key", "args": a{"key": "Tab", "session": "inner"}},
	}})
	calls := e.fake.Calls()
	if len(calls) != 2 || !hasFlag(calls[0], "--session", "outer") || !hasFlag(calls[1], "--session", "inner") {
		t.Fatalf("got %q", calls)
	}
}

func TestBatchKeepsImages(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	res := e.call("batch", a{"steps": []any{
		a{"tool": "screenshot"},
		a{"tool": "click", "args": a{"selector": "@e1"}},
	}})
	if res.images() != 1 {
		t.Fatalf("screenshot image lost in batch output: %+v", res.Content)
	}
}

func TestBatchSnapshotAfterSteps(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	res := e.call("batch", a{"snapshot": "diff", "steps": []any{
		a{"tool": "click", "args": a{"selector": "@e1"}},
	}})
	got := e.fake.Commands()
	if !reflect.DeepEqual(got[len(got)-1], cmd("diff", "snapshot", "-c")) {
		t.Fatalf("want a trailing snapshot diff, got %q", got)
	}
	if !strings.Contains(res.text(), "Changes:") {
		t.Fatalf("diff missing from result:\n%s", res.text())
	}
}
