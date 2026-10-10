//go:build ignore

// Command demo records the demo GIFs in the README. It runs agent-browser-mcp
// over stdio, as an MCP client does, against the pages in site/, records the
// browser with the record tool, and draws each frame of the recording next
// to the tool calls and results the model had read by then.
//
// The calls are scripted; the results are the server's real output. From the
// repository root, with agent-browser, ffmpeg and lighthouse on PATH:
//
//	go build -o agent-browser-mcp . && go run docs/demo/main.go
package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/client"
	"github.com/mark3labs/mcp-go/mcp"

	"github.com/vercel-labs/agent-browser-mcp/internal/cdp"
)

const (
	fps       = 12
	session   = "abm-demo"        // the browser the scenes run in
	renderer  = "abm-demo-render" // the browser that draws the frames
	slowCall  = 3.0               // calls longer than this many seconds are sped up
	spedUpTo  = 1.2               // to this many seconds
	endHold   = 4.0               // seconds the last frame stays, with the finding
	firstLook = time.Second       // time on the page before the first call
)

type step struct {
	tool    string
	args    map[string]any
	caption string
	lines   int           // result lines to show; 0 means 7
	hold    time.Duration // time to watch the page after the result; 0 means 1.2s
	capture func(result string, vars map[string]string)
	before  bool // run before recording starts, e.g. a resize, but still listed

	start, end float64 // seconds since the recording started
	result     string
	url        string // page URL after this step, if it navigated
}

type scene struct {
	name, task, intro, finding string
	setup, steps               []step
}

type args = map[string]any

func scenes(base string) []scene {
	viewport := step{tool: "emulate", args: args{"width": 800, "height": 600}}
	blank := step{tool: "navigate", args: args{"url": "about:blank"}}
	return []scene{{
		name:    "debug",
		task:    "Adding mugs to the cart gives a wrong total. Find out why.",
		intro:   "A shop whose cart total is wrong. The model starts from the symptom.",
		finding: "button.dataset.price is a string, so total + price joins text instead of adding numbers. Fix: total += Number(button.dataset.price).",
		setup:   []step{viewport, blank},
		steps: []step{
			{tool: "navigate", args: args{"url": base + "/"}, caption: "navigate reports the uncaught JS error and the 404 from the load without being asked.", lines: 9},
			{tool: "snapshot", args: args{"interactive": true}, caption: "snapshot lists the elements with @refs to act on.", capture: addButtons, lines: 9},
			{tool: "click", args: args{"selector": "@{btn1}"}, caption: "Add the $12.50 mug: the total reads $012.50."},
			{tool: "click", args: args{"selector": "@{btn2}"}, caption: "Add the $18.00 mug: $012.5018.00. The prices are joined, not added.", hold: 1600 * time.Millisecond},
			{tool: "debugger", args: args{"action": "breakpoint", "url": base + "/shop.js", "line": 11}, caption: "Set a breakpoint on the line that updates the total."},
			{tool: "click", args: args{"selector": "@{btn3}"}, caption: "The click hits the breakpoint and returns at once with the paused line. The page shows the pause.", hold: 2 * time.Second, lines: 8},
			{tool: "debugger", args: args{"action": "scope"}, caption: "scope: price is the string \"7.25\", and total is already text.", lines: 8},
			{tool: "debugger", args: args{"action": "evaluate", "expression": "typeof price"}, caption: "Check the type in the paused frame."},
			{tool: "debugger", args: args{"action": "disable"}, caption: "Remove the breakpoint and resume the page."},
		},
	}, {
		name:    "rendering",
		task:    "The blog stutters when I scroll, and the page jumps while it loads.",
		intro:   "A blog that janks on scroll and shifts as it loads.",
		finding: "The promo is inserted above the posts after load (CLS), and onScroll calls rankPosts, which sorts 600,000 numbers on every scroll event. Reserve the promo's space, and rank once instead of per scroll.",
		setup:   []step{viewport, blank},
		steps: []step{
			{tool: "navigate", args: args{"url": base + "/feed.html"}, caption: "No JS errors or failed requests this time."},
			{tool: "debug_ui", args: args{"action": "rendering", "paintFlashing": true, "layoutShifts": true, "fpsMeter": true}, caption: "Turn on DevTools rendering overlays: paint flashing, layout shift regions and the FPS meter."},
			{tool: "navigate", args: args{"action": "reload"}, caption: "Reload: the late promo pushes the posts down, outlined as a layout shift.", hold: 2 * time.Second},
			{tool: "performance", args: args{"action": "vitals"}, caption: "Web Vitals measure the jump as Cumulative Layout Shift, rated against Google's thresholds.", lines: 9, hold: 1600 * time.Millisecond},
			{tool: "scroll", args: args{"direction": "down", "px": 600}, caption: "Each scroll repaints (green), and the FPS meter marks a slow frame (yellow).", hold: 1500 * time.Millisecond},
			{tool: "performance", args: args{"action": "profiler_start"}, caption: "Profile the scroll to find the script behind it."},
			{tool: "scroll", args: args{"direction": "down", "px": 600}, hold: 600 * time.Millisecond},
			{tool: "scroll", args: args{"direction": "up", "px": 900}, hold: 600 * time.Millisecond},
			{tool: "performance", args: args{"action": "profiler_stop"}, caption: "The CPU profile puts rankPosts, called from the scroll handler, at the top.", lines: 9, hold: 1600 * time.Millisecond},
		},
	}, {
		name:    "performance",
		task:    "Is the shop fast enough on a mid-range phone?",
		intro:   "The same shop, measured as a phone on a slow network.",
		finding: "Fast enough: Lighthouse scores performance 100 on a throttled phone. Worth fixing: vendor.js is mostly unused, the Add to cart buttons fail color contrast, and analytics.js throws on every load.",
		setup:   []step{blank},
		steps: []step{
			{tool: "emulate", args: args{"device": "iPhone 14", "networkProfile": "slow-4g", "cpuSlowdown": 4}, before: true, lines: 4},
			{tool: "navigate", args: args{"url": base + "/"}, caption: "Emulate a phone on slow 4G with a 4x slower CPU, then load the shop.", lines: 4},
			{tool: "performance", args: args{"action": "vitals"}, caption: "Web Vitals for the phone.", lines: 9},
			{tool: "performance", args: args{"action": "coverage_start"}, caption: "Record which JavaScript and CSS the page runs."},
			{tool: "navigate", args: args{"action": "reload"}, lines: 3},
			{tool: "performance", args: args{"action": "coverage_stop"}, caption: "Coverage: most of vendor.js never runs.", lines: 7},
			{tool: "performance", args: args{"action": "lighthouse", "categories": []any{"performance", "accessibility", "best-practices"}}, caption: "A Lighthouse audit, run in the same browser.", lines: 12, hold: 2 * time.Second},
		},
	}}
}

var addToCart = regexp.MustCompile(`button "Add to cart" \[ref=(e\d+)\]`)

// addButtons names the three "Add to cart" refs btn1..btn3 for later steps.
func addButtons(result string, vars map[string]string) {
	for i, m := range addToCart.FindAllStringSubmatch(result, 3) {
		vars[fmt.Sprintf("btn%d", i+1)] = m[1]
	}
}

func main() {
	bin := flag.String("bin", "./agent-browser-mcp", "agent-browser-mcp binary")
	out := flag.String("out", "docs/media", "directory for the GIFs")
	work := flag.String("work", "", "scratch directory; default a new temp directory")
	port := flag.Int("port", 8123, "port for the demo site")
	only := flag.String("only", "", "comma-separated scenes to make; default all")
	mp4 := flag.Bool("mp4", false, "also write an MP4 of each scene to -out")
	flag.Parse()
	log.SetFlags(0)

	if *work == "" {
		dir, err := os.MkdirTemp("", "abm-demo-")
		check(err)
		*work = dir
	}
	check(os.MkdirAll(*out, 0o755))
	base := serve(*port, http.FileServer(http.Dir("docs/demo/site")))
	ctx := context.Background()

	env := []string{"AGENT_BROWSER_MCP_SESSION=" + session}
	if lh, err := exec.LookPath("lighthouse"); err == nil {
		env = append(env, "AGENT_BROWSER_MCP_LIGHTHOUSE_PATH="+lh)
	}
	c, err := client.NewStdioMCPClient(*bin, env)
	check(err)
	defer c.Close()
	initReq := mcp.InitializeRequest{}
	initReq.Params.ProtocolVersion = mcp.LATEST_PROTOCOL_VERSION
	initReq.Params.ClientInfo = mcp.Implementation{Name: "demo", Version: "1"}
	_, err = c.Initialize(ctx, initReq)
	check(err)

	draw := startRenderer(ctx, *work)
	defer draw.close()
	for _, sc := range scenes(base) {
		if *only != "" && !slices.Contains(strings.Split(*only, ","), sc.name) {
			continue
		}
		log.Printf("== %s", sc.name)
		video := record(ctx, c, &sc, *work)
		frames := draw.scene(ctx, sc, video, *work)
		encode(frames, filepath.Join(*out, sc.name+".gif"), *mp4)
	}
	log.Printf("frames and recordings are in %s", *work)
}

// record runs a scene's steps while the record tool films the browser, and
// notes when each call started and ended.
func record(ctx context.Context, c *client.Client, sc *scene, work string) string {
	for _, s := range sc.setup {
		mustCall(ctx, c, s.tool, s.args)
	}
	for i := range sc.steps {
		if s := &sc.steps[i]; s.before {
			s.result = mustCall(ctx, c, s.tool, s.args)
		}
	}
	video := filepath.Join(work, sc.name+".webm")
	mustCall(ctx, c, "record", args{"action": "start", "path": video, "cursor": true})
	t0 := time.Now()
	since := func() float64 { return time.Since(t0).Seconds() }
	time.Sleep(firstLook)

	vars := map[string]string{}
	for i := range sc.steps {
		s := &sc.steps[i]
		if s.before {
			continue
		}
		s.args = resolve(s.args, vars)
		s.start = since()
		s.result = mustCall(ctx, c, s.tool, s.args)
		s.end = since()
		if u, ok := s.args["url"].(string); ok && s.tool == "navigate" {
			s.url = u
		}
		if s.capture != nil {
			s.capture(s.result, vars)
		}
		log.Printf("  %5.1fs %-11s %s (%.1fs)", s.start, s.tool, formatArgs(s.args), s.end-s.start)
		hold := s.hold
		if hold == 0 {
			hold = 1200 * time.Millisecond
		}
		time.Sleep(hold)
	}
	time.Sleep(time.Second)
	mustCall(ctx, c, "record", args{"action": "stop"})
	mustCall(ctx, c, "close_browser", args{})
	return video
}

// resolve fills {name} placeholders in string arguments from vars.
func resolve(in args, vars map[string]string) args {
	out := args{}
	for k, v := range in {
		if s, ok := v.(string); ok {
			for name, val := range vars {
				s = strings.ReplaceAll(s, "{"+name+"}", val)
			}
			v = s
		}
		out[k] = v
	}
	return out
}

func mustCall(ctx context.Context, c *client.Client, tool string, a args) string {
	req := mcp.CallToolRequest{}
	req.Params.Name = tool
	req.Params.Arguments = a
	res, err := c.CallTool(ctx, req)
	check(err)
	var text []string
	for _, content := range res.Content {
		if t, ok := content.(mcp.TextContent); ok {
			text = append(text, t.Text)
		}
	}
	joined := strings.Join(text, "\n")
	if res.IsError {
		log.Fatalf("%s %s: %s", tool, formatArgs(a), joined)
	}
	return joined
}

// formatArgs shows arguments as key=value, action first.
func formatArgs(a args) string {
	keys := make([]string, 0, len(a))
	for k := range a {
		keys = append(keys, k)
	}
	slices.SortFunc(keys, func(x, y string) int {
		switch {
		case x == "action":
			return -1
		case y == "action":
			return 1
		}
		return strings.Compare(x, y)
	})
	var parts []string
	for _, k := range keys {
		v, _ := json.Marshal(a[k])
		parts = append(parts, k+"="+string(v))
	}
	return strings.Join(parts, " ")
}

// serve starts a file server on localhost:port and returns its base URL.
func serve(port int, h http.Handler) string {
	ln, err := net.Listen("tcp", fmt.Sprintf("localhost:%d", port))
	check(err)
	go http.Serve(ln, h)
	return fmt.Sprintf("http://localhost:%d", ln.Addr().(*net.TCPAddr).Port)
}

// frameRenderer draws frames with player.html in a headless browser of its own.
type frameRenderer struct {
	conn *cdp.Conn
	sid  string
	base string
}

func startRenderer(ctx context.Context, work string) *frameRenderer {
	mux := http.NewServeMux()
	mux.Handle("/work/", http.StripPrefix("/work/", http.FileServer(http.Dir(work))))
	mux.Handle("/", http.FileServer(http.Dir("docs/demo")))
	base := serve(0, mux)
	page := base + "/player.html"
	cli("open", page)
	cli("set", "viewport", "1000", "600", "2")
	var info struct {
		CDPURL string `json:"cdpUrl"`
	}
	check(json.Unmarshal(cli("get", "cdp-url"), &info))
	conn, err := cdp.Dial(ctx, info.CDPURL)
	check(err)
	pages, err := conn.Pages(ctx)
	check(err)
	for _, p := range pages {
		if p.URL == page {
			sid, err := conn.Attach(ctx, p.TargetID)
			check(err)
			return &frameRenderer{conn: conn, sid: sid, base: base}
		}
	}
	log.Fatalf("player page not found among %v", pages)
	return nil
}

func (r *frameRenderer) close() {
	r.conn.Close()
	cli("close")
}

type stepView struct {
	Tool    string  `json:"tool"`
	Args    string  `json:"args"`
	Result  *string `json:"result"`
	Running string  `json:"running,omitempty"`
}

type frameView struct {
	Frame   string     `json:"frame"`
	URL     string     `json:"url"`
	Caption string     `json:"caption"`
	Task    string     `json:"task"`
	Badge   string     `json:"badge,omitempty"`
	Steps   []stepView `json:"steps"`
	Finding string     `json:"finding,omitempty"`
}

// knot pins a time in the recording (src) to a time in the GIF (out).
type knot struct{ src, out float64 }

// timeline speeds up slow calls, such as a Lighthouse run, so the GIF does
// not show a still page for half a minute.
func timeline(sc scene, duration float64) []knot {
	ks := []knot{{0, 0}}
	for _, s := range sc.steps {
		if s.end-s.start <= slowCall {
			continue
		}
		last := ks[len(ks)-1]
		a, b := s.start+0.5, s.end-0.5
		ks = append(ks, knot{a, last.out + a - last.src}, knot{b, last.out + a - last.src + spedUpTo})
	}
	last := ks[len(ks)-1]
	return append(ks, knot{duration, last.out + duration - last.src})
}

func srcAt(ks []knot, out float64) float64 {
	for i := 1; i < len(ks); i++ {
		if out <= ks[i].out {
			a, b := ks[i-1], ks[i]
			return a.src + (out-a.out)*(b.src-a.src)/(b.out-a.out)
		}
	}
	return ks[len(ks)-1].src
}

// scene draws every frame of a scene and returns the frame directory.
func (r *frameRenderer) scene(ctx context.Context, sc scene, video, work string) string {
	src := filepath.Join(work, sc.name, "src")
	dst := filepath.Join(work, sc.name, "out")
	for _, d := range []string{src, dst} {
		check(os.RemoveAll(d))
		check(os.MkdirAll(d, 0o755))
	}
	run("ffmpeg", "-v", "error", "-i", video, "-vf", fmt.Sprintf("fps=%d", fps), filepath.Join(src, "%05d.png"))
	entries, err := os.ReadDir(src)
	check(err)
	srcFrames := len(entries)
	duration := float64(srcFrames) / fps
	ks := timeline(sc, duration)
	end := ks[len(ks)-1].out
	total := int((end + endHold) * fps)

	for i := range total {
		out := float64(i) / fps
		t := srcAt(ks, out)
		v := r.view(sc, t, out > end)
		frame := min(max(int(t*fps)+1, 1), srcFrames)
		v.Frame = fmt.Sprintf("/work/%s/src/%05d.png", sc.name, frame)
		js, err := json.Marshal(v)
		check(err)
		var ev struct {
			ExceptionDetails json.RawMessage `json:"exceptionDetails"`
		}
		check(r.conn.Call(ctx, r.sid, "Runtime.evaluate", args{"expression": "render(" + string(js) + ")", "awaitPromise": true}, &ev))
		if ev.ExceptionDetails != nil {
			log.Fatalf("render: %s", ev.ExceptionDetails)
		}
		var shot struct {
			Data string `json:"data"`
		}
		check(r.conn.Call(ctx, r.sid, "Page.captureScreenshot", args{"format": "png"}, &shot))
		png, err := base64.StdEncoding.DecodeString(shot.Data)
		check(err)
		check(os.WriteFile(filepath.Join(dst, fmt.Sprintf("%05d.png", i)), png, 0o644))
	}
	log.Printf("  %d frames, %.1fs", total, float64(total)/fps)
	return dst
}

// view is what the player shows at recording time t.
func (r *frameRenderer) view(sc scene, t float64, done bool) frameView {
	v := frameView{Task: sc.task, Caption: sc.intro, URL: "about:blank", Steps: []stepView{}}
	for _, s := range sc.steps {
		if s.start > t {
			break
		}
		if s.caption != "" {
			v.Caption = s.caption
		}
		sv := stepView{Tool: s.tool, Args: formatArgs(s.args)}
		if s.end <= t {
			res := excerpt(s.result, s.lines)
			sv.Result = &res
			if s.url != "" {
				v.URL = s.url
			}
		} else {
			sv.Running = fmt.Sprintf("… %.0f s", t-s.start)
			if s.end-s.start > slowCall {
				v.Badge = fmt.Sprintf("⏩ sped up: %s took %.0f s", s.tool, s.end-s.start)
			}
		}
		v.Steps = append(v.Steps, sv)
	}
	if done {
		v.Finding = sc.finding
	}
	return v
}

// excerpt keeps the first lines of a result, each cut to a readable length.
func excerpt(text string, n int) string {
	if n == 0 {
		n = 7
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	more := len(lines) - n
	if more > 0 {
		lines = lines[:n]
	}
	for i, l := range lines {
		if r := []rune(l); len(r) > 140 {
			lines[i] = string(r[:140]) + "…"
		}
	}
	if more > 0 {
		lines = append(lines, fmt.Sprintf("… %d more lines", more))
	}
	return strings.Join(lines, "\n")
}

// encode turns a frame directory into a GIF, and optionally an MP4 beside it.
func encode(frames, gif string, mp4 bool) {
	in := filepath.Join(frames, "%05d.png")
	run("ffmpeg", "-v", "error", "-y", "-framerate", fmt.Sprint(fps), "-i", in, "-vf",
		"scale=1000:-1:flags=lanczos,split[a][b];[a]palettegen=max_colors=192:stats_mode=diff[p];[b][p]paletteuse=dither=bayer:bayer_scale=5:diff_mode=rectangle",
		"-loop", "0", gif)
	if mp4 {
		run("ffmpeg", "-v", "error", "-y", "-framerate", fmt.Sprint(fps), "-i", in,
			"-c:v", "libx264", "-pix_fmt", "yuv420p", "-crf", "20", "-preset", "slow", "-movflags", "+faststart",
			strings.TrimSuffix(gif, ".gif")+".mp4")
	}
	st, err := os.Stat(gif)
	check(err)
	log.Printf("  wrote %s (%.1f MB)", gif, float64(st.Size())/1e6)
}

// cli runs an agent-browser command in the renderer's browser and returns its data.
func cli(a ...string) json.RawMessage {
	out, err := exec.Command("agent-browser", append([]string{"--session", renderer, "--json"}, a...)...).Output()
	check(err)
	var env struct {
		Success bool            `json:"success"`
		Data    json.RawMessage `json:"data"`
		Error   any             `json:"error"`
	}
	check(json.Unmarshal(out, &env))
	if !env.Success {
		log.Fatalf("agent-browser %s: %v", a[0], env.Error)
	}
	return env.Data
}

func run(name string, a ...string) {
	cmd := exec.Command(name, a...)
	cmd.Stderr = os.Stderr
	check(cmd.Run())
}

func check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
