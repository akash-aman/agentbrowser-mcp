package tools

import (
	"fmt"
	"slices"

	"github.com/mark3labs/mcp-go/mcp"
)

// argv builds CLI arguments from a tool request. It keeps the first
// validation error, so each tool reads as a flat list of steps and checks the
// error once in done.
type argv struct {
	req  mcp.CallToolRequest
	args []string
	err  error
}

func newArgv(req mcp.CallToolRequest, base ...string) *argv {
	return &argv{req: req, args: base}
}

// done returns the arguments, or the first validation error.
func (b *argv) done() ([]string, error) {
	if b.err != nil {
		return nil, b.err
	}
	return b.args, nil
}

func (b *argv) fail(err error) {
	if b.err == nil {
		b.err = err
	}
}

func (b *argv) add(args ...string) *argv {
	b.args = append(b.args, args...)
	return b
}

func (b *argv) has(key string) bool {
	_, ok := b.req.GetArguments()[key]
	return ok
}

func (b *argv) str(key string) string {
	return b.req.GetString(key, "")
}

func (b *argv) boolean(key string) bool {
	return b.req.GetBool(key, false)
}

// int formats a numeric argument as an integer.
func (b *argv) int(key string) string {
	return fmt.Sprint(int(b.req.GetFloat(key, 0)))
}

// required returns a non-empty string argument.
func (b *argv) required(key string) string {
	v := b.str(key)
	if v == "" {
		b.fail(fmt.Errorf("%s is required", key))
	}
	return v
}

// requiredFor is required with the action that needs the argument named in the error.
func (b *argv) requiredFor(key, action string) string {
	v := b.str(key)
	if v == "" {
		b.fail(errRequired(key, action))
	}
	return v
}

// provided returns a string argument that must be present but may be empty.
func (b *argv) provided(key string) string {
	if !b.has(key) {
		b.fail(fmt.Errorf("%s is required", key))
	}
	return b.str(key)
}

// enum returns the argument, or def when omitted, and rejects other values.
func (b *argv) enum(key, def string, allowed ...string) string {
	v := b.req.GetString(key, def)
	if !slices.Contains(allowed, v) {
		b.fail(fmt.Errorf("%s must be one of %v, got %q", key, allowed, v))
	}
	return v
}

// list returns a non-empty string array argument.
func (b *argv) list(key string) []string {
	v := b.req.GetStringSlice(key, nil)
	if len(v) == 0 {
		b.fail(fmt.Errorf("%s must not be empty", key))
	}
	return v
}

// opt appends the argument's value as a positional argument when set.
func (b *argv) opt(key string) *argv {
	if v := b.str(key); v != "" {
		b.add(v)
	}
	return b
}

// flag appends flag and the argument's value when set.
func (b *argv) flag(flag, key string) *argv {
	if v := b.str(key); v != "" {
		b.add(flag, v)
	}
	return b
}

// boolFlag appends flag when the boolean argument is true.
func (b *argv) boolFlag(flag, key string) *argv {
	if b.boolean(key) {
		b.add(flag)
	}
	return b
}

// intFlag appends flag and the integer value when it is positive.
func (b *argv) intFlag(flag, key string) *argv {
	if b.req.GetFloat(key, 0) > 0 {
		b.add(flag, b.int(key))
	}
	return b
}

func errRequired(key, action string) error {
	return fmt.Errorf("%s is required for %s", key, action)
}
