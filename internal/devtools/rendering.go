package devtools

import (
	"context"
	"fmt"
	"strings"
)

// RenderingOptions turns DevTools Rendering overlays on or off in the page
// itself; a nil field leaves that overlay unchanged.
type RenderingOptions struct {
	PaintFlashing     *bool // green flashes where the page repaints
	LayoutShifts      *bool // blue flashes where layout shifts happen
	LayerBorders      *bool // borders around compositor layers
	FPSMeter          *bool // frame rate and GPU memory meter
	ScrollBottlenecks *bool // regions that slow down scrolling
}

type overlay struct {
	label, method, param string
	value                func(RenderingOptions) *bool
}

var overlays = []overlay{
	{"paint flashing", "Overlay.setShowPaintRects", "result", func(o RenderingOptions) *bool { return o.PaintFlashing }},
	{"layout shift regions", "Overlay.setShowLayoutShiftRegions", "result", func(o RenderingOptions) *bool { return o.LayoutShifts }},
	{"layer borders", "Overlay.setShowDebugBorders", "show", func(o RenderingOptions) *bool { return o.LayerBorders }},
	{"FPS meter", "Overlay.setShowFPSCounter", "show", func(o RenderingOptions) *bool { return o.FPSMeter }},
	{"scroll bottlenecks", "Overlay.setShowScrollBottleneckRects", "show", func(o RenderingOptions) *bool { return o.ScrollBottlenecks }},
}

// SetRendering applies the overlays and describes which are on. They last as
// long as this CDP connection, like throttling.
func (p *Page) SetRendering(ctx context.Context, o RenderingOptions) (string, error) {
	for _, m := range []string{"DOM.enable", "Overlay.enable"} {
		if err := p.call(ctx, m, nil, nil); err != nil {
			return "", err
		}
	}
	p.mu.Lock()
	if p.rendering == nil {
		p.rendering = map[string]bool{}
	}
	p.mu.Unlock()
	for _, ov := range overlays {
		v := ov.value(o)
		if v == nil {
			continue
		}
		if err := p.call(ctx, ov.method, map[string]any{ov.param: *v}, nil); err != nil {
			return "", err
		}
		p.mu.Lock()
		p.rendering[ov.label] = *v
		p.mu.Unlock()
	}
	return p.renderingState(), nil
}

func (p *Page) renderingState() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var on []string
	for _, ov := range overlays {
		if p.rendering[ov.label] {
			on = append(on, ov.label)
		}
	}
	if len(on) == 0 {
		return "rendering overlays: all off"
	}
	return fmt.Sprintf("rendering overlays on: %s (visible in the headed browser window)", strings.Join(on, ", "))
}
