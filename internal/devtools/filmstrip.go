package devtools

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/jpeg"
	"image/png"
	"strings"
	"sync"
	"time"
)

// filmFrame is one screencast frame and when it arrived after navigation.
type filmFrame struct {
	at   time.Duration
	jpeg []byte
}

// Filmstrip loads the current page, or url, and captures what it looked like
// as it loaded, like the screenshots track of the Performance panel. It
// returns a PNG of up to 10 frames labeled with their time, and a summary.
// It starts from a blank page, as Lighthouse does: a reload keeps showing the
// old page until the new one paints in full (paint holding), so a filmstrip
// of a reload shows no progress.
func (p *Page) Filmstrip(ctx context.Context, url string, limit time.Duration) ([]byte, string, error) {
	var mu sync.Mutex
	var frames []filmFrame
	var start time.Time
	stopFrames := p.onSession("Page.screencastFrame", func(params json.RawMessage) {
		var e struct {
			Data      string `json:"data"`
			SessionID int    `json:"sessionId"`
		}
		if json.Unmarshal(params, &e) != nil {
			return
		}
		b, err := base64.StdEncoding.DecodeString(e.Data)
		mu.Lock()
		if err == nil && !start.IsZero() {
			frames = append(frames, filmFrame{at: time.Since(start), jpeg: b})
		}
		mu.Unlock()
		// Chrome sends the next frame only after an ack; the read loop
		// cannot wait for a reply, so ack from a goroutine.
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), p.timeout)
			defer cancel()
			p.conn.Call(ctx, p.sessionID, "Page.screencastFrameAck", map[string]any{"sessionId": e.SessionID}, nil)
		}()
	})
	defer stopFrames()
	loaded := make(chan time.Duration, 1)
	stopLoad := p.onSession("Page.loadEventFired", func(json.RawMessage) {
		mu.Lock()
		at := time.Since(start)
		mu.Unlock()
		select {
		case loaded <- at:
		default:
		}
	})
	defer stopLoad()

	if err := p.call(ctx, "Page.enable", nil, nil); err != nil {
		return nil, "", err
	}
	if url == "" {
		var hist struct {
			CurrentIndex int `json:"currentIndex"`
			Entries      []struct {
				URL string `json:"url"`
			} `json:"entries"`
		}
		if err := p.call(ctx, "Page.getNavigationHistory", nil, &hist); err != nil {
			return nil, "", err
		}
		if hist.CurrentIndex >= len(hist.Entries) {
			return nil, "", fmt.Errorf("no current page; pass url")
		}
		url = hist.Entries[hist.CurrentIndex].URL
	}
	if err := p.call(ctx, "Page.navigate", map[string]any{"url": "about:blank"}, nil); err != nil {
		return nil, "", err
	}
	select { // the blank page's load
	case <-loaded:
	case <-time.After(2 * time.Second):
	}
	if err := p.call(ctx, "Page.startScreencast", map[string]any{"format": "jpeg", "quality": 70, "maxWidth": 480, "maxHeight": 800}, nil); err != nil {
		return nil, "", err
	}
	defer p.call(context.WithoutCancel(ctx), "Page.stopScreencast", nil, nil)
	mu.Lock()
	start = time.Now()
	mu.Unlock()
	if err := p.call(ctx, "Page.navigate", map[string]any{"url": url}, nil); err != nil {
		return nil, "", err
	}
	loadAt := time.Duration(-1)
	select {
	case loadAt = <-loaded:
		// Keep watching briefly: late content often lands after load.
		select {
		case <-time.After(min(time.Second, limit)):
		case <-ctx.Done():
		}
	case <-time.After(limit):
	case <-ctx.Done():
		return nil, "", ctx.Err()
	}
	mu.Lock()
	got := append([]filmFrame(nil), frames...)
	mu.Unlock()
	if len(got) == 0 {
		return nil, "", fmt.Errorf("Chrome sent no frames; the page may not be visible")
	}
	return composeFilmstrip(got, loadAt)
}

// composeFilmstrip keeps the frames where the picture changed, at most 10,
// and lays them out in a labeled grid.
func composeFilmstrip(frames []filmFrame, loadAt time.Duration) ([]byte, string, error) {
	var distinct []filmFrame
	var last [20]byte
	for i, f := range frames {
		sum := sha1.Sum(f.jpeg)
		if i == 0 || sum != last {
			distinct = append(distinct, f)
		}
		last = sum
	}
	firstChange, complete := time.Duration(-1), distinct[len(distinct)-1].at
	if len(distinct) > 1 {
		firstChange = distinct[1].at
	}
	pick := distinct
	if len(pick) > 10 {
		pick = make([]filmFrame, 0, 10)
		for i := range 10 {
			pick = append(pick, distinct[i*(len(distinct)-1)/9])
		}
	}
	thumbs := make([]image.Image, 0, len(pick))
	for _, f := range pick {
		img, err := jpeg.Decode(bytes.NewReader(f.jpeg))
		if err != nil {
			return nil, "", fmt.Errorf("decode frame: %w", err)
		}
		thumbs = append(thumbs, shrink(img, 180))
	}
	cellW, cellH := 0, 0
	for _, t := range thumbs {
		cellW, cellH = max(cellW, t.Bounds().Dx()), max(cellH, t.Bounds().Dy())
	}
	const pad, label = 6, 16
	cols := min(5, len(thumbs))
	rows := (len(thumbs) + cols - 1) / cols
	out := image.NewRGBA(image.Rect(0, 0, cols*(cellW+pad)+pad, rows*(cellH+label+pad)+pad))
	draw.Draw(out, out.Bounds(), &image.Uniform{color.RGBA{0x24, 0x29, 0x2f, 0xff}}, image.Point{}, draw.Src)
	for i, t := range thumbs {
		x := pad + (i%cols)*(cellW+pad)
		y := pad + (i/cols)*(cellH+label+pad)
		drawText(out, x+2, y+3, fmt.Sprintf("%.1fs", pick[i].at.Seconds()), color.White)
		draw.Draw(out, image.Rect(x, y+label, x+t.Bounds().Dx(), y+label+t.Bounds().Dy()), t, t.Bounds().Min, draw.Src)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, out); err != nil {
		return nil, "", err
	}
	parts := []string{fmt.Sprintf("Filmstrip: %d distinct frames, showing %d", len(distinct), len(pick))}
	if firstChange >= 0 {
		parts = append(parts, fmt.Sprintf("first change at %.1fs", firstChange.Seconds()))
	}
	parts = append(parts, fmt.Sprintf("last change (visually complete) at %.1fs", complete.Seconds()))
	if loadAt >= 0 {
		parts = append(parts, fmt.Sprintf("load event at %.1fs", loadAt.Seconds()))
	}
	return buf.Bytes(), strings.Join(parts, "; "), nil
}

// shrink scales an image down to width w by averaging the source pixels
// each target pixel covers.
func shrink(src image.Image, w int) image.Image {
	b := src.Bounds()
	if b.Dx() <= w {
		return src
	}
	h := max(1, b.Dy()*w/b.Dx())
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		y0, y1 := b.Min.Y+y*b.Dy()/h, b.Min.Y+(y+1)*b.Dy()/h
		for x := range w {
			x0, x1 := b.Min.X+x*b.Dx()/w, b.Min.X+(x+1)*b.Dx()/w
			var r, g, bl, n uint32
			for sy := y0; sy < max(y1, y0+1); sy++ {
				for sx := x0; sx < max(x1, x0+1); sx++ {
					cr, cg, cb, _ := src.At(sx, sy).RGBA()
					r, g, bl, n = r+cr, g+cg, bl+cb, n+1
				}
			}
			dst.Set(x, y, color.RGBA{uint8(r / n >> 8), uint8(g / n >> 8), uint8(bl / n >> 8), 0xff})
		}
	}
	return dst
}

// glyphs is a 3x5 pixel font for the frame labels.
var glyphs = map[rune][5]string{
	'0': {"###", "#.#", "#.#", "#.#", "###"}, '1': {".#.", "##.", ".#.", ".#.", "###"},
	'2': {"###", "..#", "###", "#..", "###"}, '3': {"###", "..#", "###", "..#", "###"},
	'4': {"#.#", "#.#", "###", "..#", "..#"}, '5': {"###", "#..", "###", "..#", "###"},
	'6': {"###", "#..", "###", "#.#", "###"}, '7': {"###", "..#", ".#.", ".#.", ".#."},
	'8': {"###", "#.#", "###", "#.#", "###"}, '9': {"###", "#.#", "###", "..#", "###"},
	'.': {"...", "...", "...", "...", ".#."}, 's': {"...", ".##", "##.", ".##", "##."},
}

// drawText writes digits, '.' and 's' at 2x scale.
func drawText(img *image.RGBA, x, y int, text string, c color.Color) {
	for _, r := range text {
		g, ok := glyphs[r]
		if !ok {
			x += 8
			continue
		}
		for row, line := range g {
			for col, px := range line {
				if px == '#' {
					draw.Draw(img, image.Rect(x+col*2, y+row*2, x+col*2+2, y+row*2+2), &image.Uniform{c}, image.Point{}, draw.Src)
				}
			}
		}
		x += 8
	}
}
