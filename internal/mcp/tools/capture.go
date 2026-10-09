package tools

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/vercel-labs/agent-browser-mcp/internal/config"
)

// maxInlineImage is the largest screenshot returned inline; bigger files are
// returned by path only.
const maxInlineImage = 4 << 20

func (r *Registry) registerCapture() {
	r.add(config.ToolsetCore, mcp.NewTool("screenshot",
		mcp.WithDescription("Screenshot returned as an image (~1–2k tokens). Use for visual or layout checks; prefer snapshot to find things, and annotate:true to map what you see to @refs."),
		mcp.WithBoolean("annotate", mcp.Description("Number interactive elements; label [N] = @eN. Also returns the label legend.")),
		mcp.WithBoolean("fullPage", mcp.Description("Whole scroll height instead of the viewport.")),
		selectorParam(false),
		mcp.WithString("path", mcp.Description("Save location. Default temp dir.")),
		mcp.WithString("format", mcp.Enum("jpeg", "png"), mcp.Description("Default jpeg (smaller).")),
		mcp.WithNumber("quality", mcp.Description("JPEG quality 1-100. Default 70.")),
		mcp.WithBoolean("inline", mcp.Description("Return the image in the result. Default true; false returns only the path.")),
		sessionParam(), readOnly(),
	), r.handleScreenshot)

	r.add(config.ToolsetDevtools, mcp.NewTool("save_pdf",
		mcp.WithDescription("Save the current page as a PDF."),
		mcp.WithString("path", mcp.Required(), mcp.Description("Output file path.")),
		sessionParam(), mutating(),
	), r.cli(func(req mcp.CallToolRequest) ([]string, error) {
		b := newArgv(req, "pdf")
		return b.add(b.required("path")).done()
	}))
}

func screenshotArgv(req mcp.CallToolRequest) ([]string, error) {
	b := newArgv(req, "screenshot")
	defaultFormat := "jpeg"
	if strings.EqualFold(filepath.Ext(b.str("path")), ".png") {
		defaultFormat = "png"
	}
	format := b.enum("format", defaultFormat, "jpeg", "png")
	b.add("--screenshot-format", format)
	if format == "jpeg" {
		b.add("--screenshot-quality", b.quality())
	}
	// The CLI takes the selector and path as positional arguments.
	return b.boolFlag("--full", "fullPage").boolFlag("--annotate", "annotate").opt("selector").opt("path").done()
}

func (b *argv) quality() string {
	q := int(b.req.GetFloat("quality", 70))
	if q < 1 || q > 100 {
		b.fail(fmt.Errorf("quality must be 1-100, got %d", q))
	}
	return fmt.Sprint(q)
}

type screenshotData struct {
	Path        string `json:"path"`
	Annotations []struct {
		Number int    `json:"number"`
		Ref    string `json:"ref"`
		Role   string `json:"role"`
		Name   string `json:"name"`
	} `json:"annotations"`
}

func (r *Registry) handleScreenshot(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args, err := screenshotArgv(req)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	out, err := r.mgr.Run(ctx, getSession(req), args...)
	if err != nil {
		return mcp.NewToolResultError(err.Error()), nil
	}
	var shot screenshotData
	if err := json.Unmarshal(out.Data, &shot); err != nil || shot.Path == "" {
		return textResult(formatData(out.Data), r.cfg.MaxOutput), nil
	}

	res := &mcp.CallToolResult{}
	notes := []string{"path: " + shot.Path}
	if req.GetBool("inline", true) {
		img, problem := inlineImage(shot.Path)
		if problem != "" {
			notes = append(notes, problem)
		} else {
			res.Content = append(res.Content, img)
		}
	}
	notes = append(notes, shot.legend()...)
	if !req.GetBool("annotate", false) {
		notes = append(notes, hintAnnotate)
	}
	appendText(res, truncate(strings.Join(notes, "\n"), r.cfg.MaxOutput))
	return res, nil
}

// inlineImage loads a screenshot for the result, or says why it cannot.
func inlineImage(path string) (mcp.ImageContent, string) {
	img, err := os.ReadFile(path)
	if err != nil {
		return mcp.ImageContent{}, "could not read image: " + err.Error()
	}
	if len(img) > maxInlineImage {
		return mcp.ImageContent{}, fmt.Sprintf("image is %d bytes, too large to return inline; open the path instead or use selector/jpeg", len(img))
	}
	return mcp.NewImageContent(base64.StdEncoding.EncodeToString(img), imageMime(path)), ""
}

// legend maps annotation labels to refs: [N] @eN role "name".
func (s screenshotData) legend() []string {
	lines := make([]string, 0, len(s.Annotations))
	for _, a := range s.Annotations {
		line := fmt.Sprintf("[%d] @%s %s", a.Number, a.Ref, a.Role)
		if a.Name != "" {
			line += fmt.Sprintf(" %q", a.Name)
		}
		lines = append(lines, line)
	}
	return lines
}

func imageMime(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	}
	return "image/png"
}
