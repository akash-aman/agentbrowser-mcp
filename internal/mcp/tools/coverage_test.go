package tools

import (
	"fmt"
	"slices"
	"testing"
)

// TestEveryToolAndEnumValueHasArgvCase makes "a test for each case" enforced:
// adding a tool or an enum value without a successful row in argvCases (CLI
// tools) or cdpCases (CDP tools) fails here. The snapshot param is covered per
// tool by TestSnapshotFollowUp instead.
func TestEveryToolAndEnumValueHasArgvCase(t *testing.T) {
	t.Parallel()
	e := newEnv(t)

	covered := map[string]bool{}
	cover := func(tool string, args map[string]any) {
		covered[tool] = true
		for k, v := range args {
			if list, ok := v.([]any); ok {
				for _, item := range list {
					covered[fmt.Sprintf("%s.%s=%v", tool, k, item)] = true
				}
				continue
			}
			covered[fmt.Sprintf("%s.%s=%v", tool, k, v)] = true
		}
	}
	for _, c := range argvCases {
		if c.wantErr == "" {
			cover(c.tool, c.args)
		}
	}
	for _, c := range cdpCases {
		if c.wantErr == "" {
			cover(c.tool, c.args)
		}
	}

	checked := 0
	for _, st := range e.reg.Tools() {
		name := st.Tool.Name
		if !covered[name] {
			t.Errorf("tool %s has no successful argvCases or cdpCases row", name)
			continue
		}
		for param, schema := range st.Tool.InputSchema.Properties {
			if param == "snapshot" {
				continue
			}
			for _, v := range enumValues(schema) {
				checked++
				if !covered[fmt.Sprintf("%s.%s=%s", name, param, v)] {
					t.Errorf("%s: no successful test row with %s=%q", name, param, v)
				}
			}
		}
	}
	// Guard against the check silently matching nothing (e.g. if enums stop
	// being readable from the schema).
	if checked < 100 {
		t.Fatalf("only %d enum values checked; schema enums are not being read", checked)
	}
}

// enumValues reads a property's enum, including the enum of array items.
func enumValues(schema any) []string {
	m, ok := schema.(map[string]any)
	if !ok {
		return nil
	}
	if items, ok := m["items"].(map[string]any); ok {
		return enumValues(items)
	}
	switch v := m["enum"].(type) {
	case []string:
		return slices.Clone(v)
	case []any:
		out := make([]string, 0, len(v))
		for _, x := range v {
			out = append(out, fmt.Sprint(x))
		}
		return out
	}
	return nil
}
