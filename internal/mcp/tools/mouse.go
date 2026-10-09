package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/vercel-labs/agent-browser-mcp/internal/config"
)

func (r *Registry) registerMouse() {
	r.add(config.ToolsetCore, mcp.NewTool("mouse",
		mcp.WithDescription("Low-level mouse at viewport coordinates. Fallback for canvas, maps or targets with no @ref — prefer click @ref."),
		mcp.WithString("action", mcp.Required(), mcp.Enum("click", "move", "down", "up", "wheel")),
		mcp.WithNumber("x", mcp.Description("X for click/move.")),
		mcp.WithNumber("y", mcp.Description("Y for click/move.")),
		mcp.WithString("button", mcp.Enum("left", "right", "middle"), mcp.Description("Default left.")),
		mcp.WithNumber("deltaX", mcp.Description("Horizontal wheel delta.")),
		mcp.WithNumber("deltaY", mcp.Description("Vertical wheel delta.")),
		mcp.WithBoolean("human", mcp.Description("click/move: follow a human-like eased curve.")),
		mcp.WithNumber("seed", mcp.Description("With human: seed that makes the path reproducible.")),
		mcp.WithNumber("duration", mcp.Description("click/move: movement time in ms.")),
		sessionParam(), mutating(),
	), r.handleMouse)
}

// mouseArgv returns one argv per CLI call; click is move + down + up.
func mouseArgv(req mcp.CallToolRequest) ([][]string, error) {
	b := newArgv(req)
	action := b.enum("action", "", "click", "move", "down", "up", "wheel")
	button := b.enum("button", "left", "left", "right", "middle")
	var steps [][]string
	switch action {
	case "down", "up":
		steps = [][]string{{"mouse", action, button}}
	case "wheel":
		steps = [][]string{b.wheel()}
	case "move":
		steps = [][]string{b.moveTo(action)}
	case "click":
		steps = [][]string{b.moveTo(action), {"mouse", "down", button}, {"mouse", "up", button}}
	}
	if _, err := b.done(); err != nil {
		return nil, err
	}
	return steps, nil
}

func (b *argv) moveTo(action string) []string {
	if !b.has("x") || !b.has("y") {
		b.fail(fmt.Errorf("x and y are required for %s", action))
	}
	step := []string{"mouse", "move", b.int("x"), b.int("y")}
	if b.boolean("human") {
		step = append(step, "--human")
	}
	for _, f := range []struct{ flag, key string }{{"--seed", "seed"}, {"--duration", "duration"}} {
		if b.has(f.key) {
			step = append(step, f.flag, b.int(f.key))
		}
	}
	return step
}

func (b *argv) wheel() []string {
	if !b.has("deltaY") && !b.has("deltaX") {
		b.fail(fmt.Errorf("deltaY or deltaX is required for wheel"))
	}
	step := []string{"mouse", "wheel", b.int("deltaY")}
	if b.has("deltaX") {
		step = append(step, b.int("deltaX"))
	}
	return step
}

func (r *Registry) handleMouse(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	steps, err := mouseArgv(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	var res *mcp.CallToolResult
	for _, args := range steps {
		if res = r.run(ctx, req, args...); res.IsError {
			return res, nil
		}
	}
	if req.GetString("action", "") == "click" {
		res = mcp.NewToolResultText(fmt.Sprintf("clicked at %s\n%s", strings.Join(steps[0][2:], ","), hintMouseClick))
	}
	return res, nil
}
