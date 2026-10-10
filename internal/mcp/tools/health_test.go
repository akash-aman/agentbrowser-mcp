package tools

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestNavigateReportsPageProblems(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	got := e.call("navigate", a{"url": "http://fake/broken"}).text()
	want := "Page problems during this load: 1 uncaught JS error and 2 failed requests. Look into them unless they are expected for this page:\n" +
		"- JS error: TypeError: cart is undefined (at http://fake/broken.js:12:5)\n" +
		"- 404 Image http://fake/missing.png\n" +
		"- 500 Fetch http://fake/api/cart"
	if !strings.Contains(got, want) {
		t.Fatalf("got:\n%s\nwant it to contain:\n%s", got, want)
	}
	// Errors from before the load, requests from before it, and the favicon
	// Chrome asks for on its own are not the page's problems.
	for _, noise := range []string{"Error: boom", "http://fake/old", "favicon"} {
		if strings.Contains(got, noise) {
			t.Errorf("note mentions %q:\n%s", noise, got)
		}
	}
}

func TestNavigateIsQuietOnAHealthyPage(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	if got := e.call("navigate", a{"url": "http://x/"}).text(); strings.Contains(got, "Page problems") {
		t.Fatalf("got %q", got)
	}
}

// TestHealthNoteComesBeforeTheSnapshot keeps the note next to the navigation
// it describes rather than after a long snapshot.
func TestHealthNoteComesBeforeTheSnapshot(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	got := e.call("navigate", a{"url": "http://fake/broken", "snapshot": "full"}).text()
	note, snap := strings.Index(got, "Page problems"), strings.Index(got, "Snapshot:")
	if note < 0 || snap < 0 || note > snap {
		t.Fatalf("want the note before the snapshot:\n%s", got)
	}
}

// TestHealthWithoutTheErrorsLog: when the CLI cannot list page errors, the
// navigation still succeeds and failed requests are still reported.
func TestHealthWithoutTheErrorsLog(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.fake.FailOn("errors")
	res := e.call("navigate", a{"url": "http://fake/broken"})
	if res.IsError {
		t.Fatal(res.text())
	}
	got := res.text()
	if strings.Contains(got, "JS error") || !strings.Contains(got, "Page problems during this load: 2 failed requests.") {
		t.Fatalf("got:\n%s", got)
	}
}

// TestHealthWaitsWhileDebugging: a paused page could hold the extra
// commands, so navigation does not check health while the debugger is on.
func TestHealthWaitsWhileDebugging(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.withCDP()
	e.call("debugger", a{"action": "breakpoint", "url": "http://fake/app.js", "line": 12})
	e.fake.Reset()
	e.call("navigate", a{"url": "http://fake/broken"})
	for _, c := range e.fake.Commands() {
		if c[0] == "errors" || c[0] == "network" {
			t.Fatalf("health ran while debugging: %q", e.fake.Commands())
		}
	}
}

func TestHealthNoteListsTheFirstFew(t *testing.T) {
	t.Parallel()
	var errs []string
	for i := range 5 {
		errs = append(errs, fmt.Sprintf("Error: e%d", i))
	}
	got := healthNote(errs, []string{"404 Image http://x/a.png"})
	for _, want := range []string{
		"5 uncaught JS errors and 1 failed request.",
		"- JS error: Error: e2\n- … 2 more: console kind:\"errors\"\n- 404 Image",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "e3") {
		t.Errorf("listed more than %d errors:\n%s", healthShown, got)
	}
	if healthNote(nil, nil) != "" {
		t.Error("no problems must give no note")
	}
}

// TestHealthWaitsForLateErrors: errors thrown just after the load event
// count, so the check waits healthSettle before looking.
func TestHealthWaitsForLateErrors(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.reg.healthSettle = 400 * time.Millisecond
	start := time.Now()
	e.call("navigate", a{"url": "http://fake/broken"})
	if d := time.Since(start); d < 400*time.Millisecond {
		t.Fatalf("navigate returned after %v, before the settle time", d)
	}
}
