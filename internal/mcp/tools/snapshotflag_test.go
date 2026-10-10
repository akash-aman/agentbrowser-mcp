package tools

import (
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// TestSnapshotFollowUp checks the snapshot param on every tool that has it,
// using each tool's first successful argvCases row as the base call.
func TestSnapshotFollowUp(t *testing.T) {
	t.Parallel()
	base := map[string]argvCase{}
	for _, c := range argvCases {
		if _, ok := base[c.tool]; !ok && c.wantErr == "" {
			base[c.tool] = c
		}
	}

	tools := 0
	for _, st := range newEnv(t).reg.Tools() {
		name := st.Tool.Name
		if _, ok := st.Tool.InputSchema.Properties["snapshot"]; !ok {
			continue
		}
		tools++
		c := base[name]
		var covered []string
		for _, m := range followUps {
			covered = append(covered, m.value)
		}
		if got := enumValues(st.Tool.InputSchema.Properties["snapshot"]); !slices.Equal(got, covered) {
			t.Fatalf("%s snapshot modes %q, test covers %q", name, got, covered)
		}
		for _, mode := range followUps {
			t.Run(name+"_"+mode.value, func(t *testing.T) {
				t.Parallel()
				e := newEnv(t)
				if c.setup != nil {
					c.setup(e.fake)
				}
				args := maps.Clone(c.args)
				args["snapshot"] = mode.value
				res := e.call(name, args)
				if res.IsError {
					t.Fatal(res.text())
				}
				want := c.want
				if mode.follow != nil {
					want = append(append([][]string{}, c.want...), mode.follow)
				}
				if got := e.fake.Commands(); !reflect.DeepEqual(got, want) {
					t.Fatalf("got %q, want %q", got, want)
				}
				if mode.label != "" && !strings.Contains(res.text(), mode.label) {
					t.Fatalf("result missing %q:\n%s", mode.label, res.text())
				}
			})
		}
	}
	if tools < 10 {
		t.Fatalf("only %d tools have a snapshot param", tools)
	}
}

// followUps is every snapshot param value with the follow-up CLI call it
// makes and a label its result must contain.
var followUps = []struct {
	value  string
	follow []string
	label  string
}{
	{"none", nil, ""},
	{"delta", cmd("snapshot", "-i", "-c", "--delta"), "Snapshot revision 1 (full"},
	// The server diffs snapshots itself; agent-browser's keeps no baseline.
	{"diff", cmd("snapshot"), "Changes:"},
	{"full", cmd("snapshot", "-i", "-c"), "Snapshot:"},
}

func TestSnapshotFollowUpSkippedOnError(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.fake.FailOn("press")
	e.call("press_key", a{"key": "Enter", "snapshot": "diff"})
	if got := e.fake.Commands(); len(got) != 1 {
		t.Fatalf("no follow-up after a failed action, got %q", got)
	}
}
