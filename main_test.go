package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/vercel-labs/agent-browser-mcp/internal/browser"
	"github.com/vercel-labs/agent-browser-mcp/internal/testutil/fakecli"
)

// TestMain lets the test binary act as the fake agent-browser CLI, or as the
// server itself (main) when ABM_RUN_MAIN is set, so tests can drive the real
// process lifecycle: stdio, signals and shutdown.
func TestMain(m *testing.M) {
	fakecli.MaybeRun()
	if os.Getenv("ABM_RUN_MAIN") == "1" {
		os.Args = append([]string{"agent-browser-mcp"}, strings.Fields(os.Getenv("ABM_ARGS"))...)
		main()
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// stopAfterToolCall starts the server, uses session "work" via one tool call,
// then sends SIGINT the way Claude Code does when it reconnects a server.
func stopAfterToolCall(t *testing.T, fake *fakecli.Fake, flags ...string) {
	t.Helper()
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "ABM_RUN_MAIN=1", "ABM_ARGS="+strings.Join(append([]string{"--agent-browser-path", fake.Path}, flags...), " "))
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	out := bufio.NewScanner(stdout)
	send := func(id int, method string, params any) {
		msg, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
		fmt.Fprintf(stdin, "%s\n", msg)
		for out.Scan() {
			var resp struct {
				ID int `json:"id"`
			}
			if json.Unmarshal(out.Bytes(), &resp) == nil && resp.ID == id {
				return
			}
		}
		t.Fatalf("no response to %s", method)
	}
	send(1, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "1"}})
	send(2, "tools/call", map[string]any{"name": "click", "arguments": map[string]any{"selector": "@e1", "session": "work"}})

	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("server did not exit cleanly on SIGINT: %v", err)
		}
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		t.Fatal("server did not exit on SIGINT")
	}
}

// TestStopDuringASlowCall: Claude Code sends SIGKILL half a second after
// SIGINT, so a call in flight must not hold up the exit; a server killed
// first never releases its CDP connections.
func TestStopDuringASlowCall(t *testing.T) {
	t.Parallel()
	fake := fakecli.Install(t)
	fake.SleepMS(10000, "click")
	cmd := exec.Command(os.Args[0])
	// Under -race the runtime sleeps a second at exit unless told not to.
	cmd.Env = append(os.Environ(), "ABM_RUN_MAIN=1", "ABM_ARGS=--agent-browser-path "+fake.Path, "GORACE=atexit_sleep_ms=0")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(stdin, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`)
	fmt.Fprintln(stdin, `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"click","arguments":{"selector":"#slow","session":"work"}}}`)
	deadline := time.Now().Add(5 * time.Second)
	for !slices.ContainsFunc(fake.Calls(), func(c []string) bool { return slices.Contains(c, "click") }) {
		if time.Now().After(deadline) {
			cmd.Process.Kill()
			t.Fatal("the click never reached the CLI")
		}
		time.Sleep(10 * time.Millisecond)
	}
	start := time.Now()
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("exit: %v", err)
		}
		if took := time.Since(start); took > 500*time.Millisecond {
			t.Fatalf("took %v to stop; the client kills it after 500ms", took)
		}
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		t.Fatal("server waited for the slow click instead of stopping")
	}
}

func closeCalls(fake *fakecli.Fake) [][]string {
	var closes [][]string
	for _, c := range fake.Calls() {
		if slices.Equal(fakecli.Command(c), []string{"close"}) {
			closes = append(closes, c)
		}
	}
	return closes
}

// TestReconnectLeavesBrowsersRunning is the regression test for browsers
// dying whenever the MCP server was restarted: the session belongs to the
// agent-browser daemon and the next server, not to this process.
func TestReconnectLeavesBrowsersRunning(t *testing.T) {
	t.Parallel()
	fake := fakecli.Install(t)
	stopAfterToolCall(t, fake)
	if closes := closeCalls(fake); len(closes) != 0 {
		t.Fatalf("stopping the server closed browser sessions: %q", closes)
	}
	if len(fake.Calls()) == 0 {
		t.Fatal("the tool call never reached the CLI; the test proves nothing")
	}
}

func TestCloseOnExitClosesUsedSessions(t *testing.T) {
	t.Parallel()
	fake := fakecli.Install(t)
	stopAfterToolCall(t, fake, "--close-on-exit")
	closes := closeCalls(fake)
	if len(closes) != 1 || !slices.Contains(closes[0], "work") {
		t.Fatalf("want one close of session work, got %q", closes)
	}
}

// TestStdinCloseLeavesBrowsersRunning covers the other way a client stops a
// stdio server: closing its stdin.
func TestStdinCloseLeavesBrowsersRunning(t *testing.T) {
	t.Parallel()
	fake := fakecli.Install(t)
	cmd := exec.Command(os.Args[0])
	cmd.Env = append(os.Environ(), "ABM_RUN_MAIN=1", "ABM_ARGS=--agent-browser-path "+fake.Path)
	cmd.Stdin = strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"click","arguments":{"selector":"@e1","session":"work"}}}` + "\n")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("server exit: %v\n%s", err, out)
	}
	if len(fake.Calls()) == 0 || len(closeCalls(fake)) != 0 {
		t.Fatalf("calls %q", fake.Calls())
	}
}

// TestReadmeStatesSupportedCLI keeps the documented agent-browser versions in
// step with the ones the server checks.
func TestReadmeStatesSupportedCLI(t *testing.T) {
	readme, err := os.ReadFile("README.md")
	if err != nil {
		t.Fatal(err)
	}
	row := fmt.Sprintf("| %s or newer | %s |", browser.MinCLIVersion, browser.TestedCLIVersion)
	if !strings.Contains(string(readme), row) {
		t.Fatalf("README version table must contain %q", row)
	}
	if !strings.Contains(string(readme), "CLI **"+browser.MinCLIVersion+" or newer**") {
		t.Fatalf("README prerequisites must name %s", browser.MinCLIVersion)
	}
}
