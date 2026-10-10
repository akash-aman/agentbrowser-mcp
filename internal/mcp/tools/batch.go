package tools

import (
	"context"
	"fmt"
	"maps"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/vercel-labs/agent-browser-mcp/internal/config"
)

func (r *Registry) registerBatch() {
	r.add(config.ToolsetCore, mcp.NewTool("batch",
		mcp.WithDescription("Run several tool calls in order in one round trip, e.g. fill a form and submit. Prefer this over one call per step when you already know the steps."),
		mcp.WithArray("steps", mcp.Required(), mcp.Description(`Steps like {"tool":"fill","args":{"selector":"@e3","value":"a"}}. Any tool except batch.`),
			mcp.Items(map[string]any{
				"type": "object",
				"properties": map[string]any{
					"tool": map[string]any{"type": "string"},
					"args": map[string]any{"type": "object"},
				},
				"required": []string{"tool"},
			})),
		mcp.WithBoolean("bail", mcp.Description("Stop at the first failing step. Default false (run all).")),
		snapshotParam(), sessionParam(), mutating(),
	), r.handleBatch)
}

// handleBatch dispatches each step to the registered handler in-process, so
// steps get the same validation and output shaping as direct calls. A
// batch-level session applies to steps that do not set their own.
func (r *Registry) handleBatch(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	steps, ok := req.GetArguments()["steps"].([]any)
	if !ok || len(steps) == 0 {
		return mcp.NewToolResultError("steps must be a non-empty array"), nil
	}
	bail := req.GetBool("bail", false)
	session := getSession(req)

	res := &mcp.CallToolResult{}
	failed, ran := 0, 0
	for i, raw := range steps {
		ran++
		tool, out := r.runStep(ctx, raw, session)
		header := fmt.Sprintf("#%d %s", i+1, tool)
		if out.IsError {
			failed++
			header += " FAILED"
		}
		appendText(res, header+"\n"+resultText(out))
		for _, c := range out.Content {
			if img, ok := c.(mcp.ImageContent); ok {
				res.Content = append(res.Content, img)
			}
		}
		if out.IsError && bail {
			break
		}
	}

	summary := fmt.Sprintf("batch: %d/%d steps ran, %d failed", ran, len(steps), failed)
	// One text block: clients that join adjacent text contents without a
	// separator showed "0 failed#1 snapshot".
	if first, ok := firstText(res); ok {
		res.Content[0] = mcp.NewTextContent(summary + "\n" + first)
	} else {
		res.Content = append([]mcp.Content{mcp.NewTextContent(summary)}, res.Content...)
	}
	if failed > 0 && bail {
		res.IsError = true
		return res, nil
	}
	r.appendSnapshot(ctx, req, res)
	return res, nil
}

func (r *Registry) runStep(ctx context.Context, raw any, session string) (string, *mcp.CallToolResult) {
	step, ok := raw.(map[string]any)
	if !ok {
		return "?", mcp.NewToolResultError("step must be an object with tool and args")
	}
	tool, _ := step["tool"].(string)
	if tool == "" {
		return "?", mcp.NewToolResultError("step is missing tool")
	}
	if tool == "batch" {
		return tool, mcp.NewToolResultError("batch cannot be nested")
	}
	h, ok := r.handlers[tool]
	if !ok {
		return tool, mcp.NewToolResultError(fmt.Sprintf("unknown tool %q", tool))
	}

	args := map[string]any{}
	if a, ok := step["args"].(map[string]any); ok {
		maps.Copy(args, a)
	}
	if _, set := args["session"]; !set && session != "" {
		args["session"] = session
	}
	var sub mcp.CallToolRequest
	sub.Params.Name = tool
	sub.Params.Arguments = args

	out, err := h(ctx, sub)
	if err != nil {
		return tool, mcp.NewToolResultError(err.Error())
	}
	return tool, out
}

func firstText(res *mcp.CallToolResult) (string, bool) {
	if len(res.Content) == 0 {
		return "", false
	}
	t, ok := res.Content[0].(mcp.TextContent)
	return t.Text, ok
}
