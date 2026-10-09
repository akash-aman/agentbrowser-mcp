# agent-browser-mcp

An [MCP](https://modelcontextprotocol.io/) server that exposes the [agent-browser](https://github.com/vercel-labs/agent-browser) CLI as MCP tools, so any MCP client and any LLM can drive a browser over `stdio`.

Built with Go, powered by [`mark3labs/mcp-go`](https://github.com/mark3labs/mcp-go).

It is designed to be cheap for models to use:

- **41 tools, ~8k tokens of schema** with everything enabled, including a JavaScript debugger, profiling and the DevTools Application panel (17 KB / 25 tools for `--toolsets core`).
- **Plain-text results.** Snapshots are the bare accessibility tree, and network and console results are one line per entry.
- **Ref-first interaction.** Elements are acted on by `@ref` from `snapshot`, and annotated screenshots map what a vision model sees back to refs.
- **One round trip for many steps** with `batch`, and `snapshot:"delta"` on any action to get just the `@ref`s it added, changed or removed.

## Prerequisites

- [agent-browser](https://github.com/vercel-labs/agent-browser) CLI **0.38.0 or newer** on `PATH` (tested with 0.38.2): `npm install -g agent-browser@latest`
- Go 1.26+ (to build)

### Supported agent-browser versions

| agent-browser-mcp | agent-browser CLI | Tested with |
|---|---|---|
| 2.0.x | 0.38.0 or newer | 0.38.2 |

0.38.0 is the minimum because the server uses `snapshot --delta`, `--human` pointer movement and the `lifecycle` field that 0.38 adds to every JSON result. At startup the server runs `agent-browser --version`. If the CLI is missing or older than the minimum, it logs a warning to stderr and tells the model in its instructions. `help {topic:"doctor"}` also reports the installed version against the supported range.

## Install

```bash
go install github.com/vercel-labs/agent-browser-mcp@latest
```

Or clone and build:

```bash
git clone https://github.com/vercel-labs/agent-browser-mcp.git
cd agent-browser-mcp
go build -o agent-browser-mcp .
```

## MCP client setup

```json
{
  "mcpServers": {
    "agent-browser": {
      "command": "agent-browser-mcp",
      "args": ["--headed"]
    }
  }
}
```

The same block works for Claude Desktop (`claude_desktop_config.json`), Claude Code, Cline, Cursor and other MCP clients.

### Use your own logged-in Chrome

Recent Chrome versions refuse remote debugging on the default profile. Pick one of these:

- **Dedicated profile:** use `--profile ~/.agent-browser/chrome`, then log in once with `--headed`. The logins persist.
- **Copy your logins once:** run `agent-browser --auto-connect state save ~/auth.json` while Chrome is running with remote debugging, then start the server with `--state ~/auth.json`.
- **Attach to a debuggable Chrome:** use `--cdp 9222` or `--auto-connect`, or call the `session` tool with `action: "connect"`.

## Configuration

Every setting can come from an environment variable or a flag; flags win.

| Environment variable | Flag | Default | Description |
|---|---|---|---|
| `AGENT_BROWSER_MCP_TOOLSETS` | `--toolsets` | `all` | `all` or a comma list of `core`, `network`, `devtools`, `emulation`, `storage` (core is always on) |
| `AGENT_BROWSER_MCP_MAX_OUTPUT` | `--max-output` | `40000` | Max characters of text per tool result (`0` = unlimited) |
| `AGENT_BROWSER_MCP_TIMEOUT` | `--timeout` | `60000` | Command timeout in ms |
| `AGENT_BROWSER_MCP_NAME` | `--name` | `agent-browser-mcp` | Server identity name |
| `AGENT_BROWSER_MCP_PROJECT` | `--project` | — | Project this browser serves (shown to the model) |
| `AGENT_BROWSER_MCP_PURPOSE` | `--purpose` | — | Purpose of this instance (shown to the model) |
| `AGENT_BROWSER_MCP_BROWSER_PATH` | `--agent-browser-path` | `agent-browser` | Path to the agent-browser binary |
| `AGENT_BROWSER_MCP_SESSION` | `--session` | — | Default session name |
| `AGENT_BROWSER_SESSION_NAME` | `--session-name` | — | Auto-save/restore cookies and storage under this name |
| `AGENT_BROWSER_PROFILE` | `--profile` | — | Chrome profile name or directory |
| `AGENT_BROWSER_STATE` | `--state` | — | Storage state file to load |
| `AGENT_BROWSER_AUTO_CONNECT` | `--auto-connect` | `false` | Attach to a running Chrome |
| `AGENT_BROWSER_CDP` | `--cdp` | — | Attach on this CDP port or URL |
| `AGENT_BROWSER_ENGINE` | `--engine` | — | `chrome` or `lightpanda` |
| `AGENT_BROWSER_PROVIDER` | `--provider` | — | `browserless`, `browserbase`, `browseruse`, `kernel`, `agentcore`, `ios` |
| `AGENT_BROWSER_HEADED` | `--headed` | `false` | Show the browser window |
| `AGENT_BROWSER_EXECUTABLE_PATH` | `--executable-path` | — | Custom browser binary |
| `AGENT_BROWSER_ARGS` | `--browser-args` | — | Extra browser launch args, comma-separated |
| `AGENT_BROWSER_PROXY` | `--proxy` | — | Proxy URL |
| `AGENT_BROWSER_PROXY_BYPASS` | `--proxy-bypass` | — | Hosts that skip the proxy |
| `AGENT_BROWSER_USER_AGENT` | `--user-agent` | — | User-Agent at launch |
| `AGENT_BROWSER_INPUT_MODE` | `--input-mode` | — (CLI default `instant`) | Pointer movement for every action: `instant`, `smooth` or `human` |
| `AGENT_BROWSER_EXTENSIONS` | `--extensions` | — | Extension paths, comma-separated |
| `AGENT_BROWSER_INIT_SCRIPTS` | `--init-scripts` | — | Scripts run before page scripts, comma-separated |
| `AGENT_BROWSER_ENABLE` | `--enable` | — | Built-in init scripts, e.g. `react-devtools` (needed for the `react` tool) |
| `AGENT_BROWSER_DOWNLOAD_PATH` | `--download-path` | — | Default download directory |
| `AGENT_BROWSER_IGNORE_HTTPS_ERRORS` | `--ignore-https-errors` | `false` | Ignore certificate errors |
| `AGENT_BROWSER_NO_AUTO_DIALOG` | `--no-auto-dialog` | `false` | Keep alerts open so the `dialog` tool can handle them |
| `AGENT_BROWSER_ALLOWED_DOMAINS` | `--allowed-domains` | — | Restrict navigation to these domains |
| `AGENT_BROWSER_ACTION_POLICY` | `--action-policy` | — | Action policy JSON file |
| `AGENT_BROWSER_CONTENT_BOUNDARIES` | `--content-boundaries` | `false` | Mark page output to resist prompt injection |
| `AGENT_BROWSER_CONFIG` | `--config` | — | agent-browser.json config file |
| `AGENT_BROWSER_MCP_CLOSE_ON_EXIT` | `--close-on-exit` | `false` | Close the browser sessions this server used when it stops. Off by default: sessions are agent-browser daemons, so an MCP reconnect or update keeps your browser, page and logins |
| `AGENT_BROWSER_MCP_LIGHTHOUSE_PATH` | `--lighthouse-path` | `lighthouse` | Lighthouse CLI for `performance` action `lighthouse` (`npm install -g lighthouse`) |

## Tools

Every tool takes an optional `session` for an isolated browser.

**core** (always on)

| Tool | What it does |
|---|---|
| `navigate` | goto (with optional per-origin `headers`), back, forward, reload, SPA pushstate |
| `snapshot` | Accessibility tree with `@ref`s — the primary way to read a page. `delta:true` returns only the refs added, changed or removed since the last delta snapshot (`full:true` resets the baseline) |
| `page_text` | Readable text of the page or an element |
| `get` | text, html, value, attr, count, box, styles, visible/enabled/checked, title, url, cdp_url |
| `find` | Locate by role/text/label/placeholder/alt/title/testid/first/last/nth and act; `all` returns every match's text |
| `click`, `fill`, `type`, `press_key`, `select_option`, `drag`, `scroll` | Interaction. `click` and `drag` take `human:true` to move the pointer along a human-like curve first. `click`, `fill`, `type` and hover refuse a target that is not visible, because agent-browser reports success for them on `display:none` elements even though nothing receives them |
| `element_action` | hover, focus, check, uncheck, scroll_into_view, highlight |
| `upload_file`, `download` | Files |
| `wait` | element, hidden, text, url, load state, JS condition, time, download |
| `screenshot` | Returned as an image; `annotate` labels elements with their refs |
| `mouse` | Coordinate click/move/down/up/wheel for canvas and targets with no ref; `human`, `seed` and `duration` shape the movement |
| `tabs`, `dialog`, `console`, `eval_script`, `batch`, `close_browser`, `help` | Tabs and windows, JS dialogs, console + page errors + DevTools Issues (with pattern/limit), JS, multi-step calls, cleanup, CLI help |

**network** — `network`: request log (one line each, filterable), request detail, route/mock/block, HAR recording.

**devtools**
- `debugger` — a JavaScript debugger: line, conditional and URL-regex breakpoints, logpoints, DOM-change / XHR-fetch / event-listener breakpoints, pause on exceptions; pause, resume, step over/into/out, continue to a line; call stack, scope variables (expandable by object id), evaluate in a frame, script list and source, and the event listeners on an element. Any tool that hits a breakpoint (a `click`, say) returns right away with the paused location and source, and finishes after you resume.
- `performance` — Web Vitals, navigation timing and slowest resources, live runtime metrics (heap, DOM nodes, listeners, layouts, script time), JS heap and DOM size, heap snapshots summarized by constructor with detached DOM node count, JS/CSS coverage (unused bytes per file), Lighthouse audits (scores, metrics, top issues), and traces / CPU profiles that are summarized on stop (long tasks and blocking time; top functions by self time).
- `react` (tree, inspect, render profiling, Suspense), `record` (WebM video), `diff` (snapshot, screenshot, two URLs), `debug_ui` (open Chrome's own DevTools window in the same browser, on any panel such as `sources` to watch the debugger pause, or get a DevTools URL with `external: true`. If DevTools docks beside the page, the window is widened so the page keeps its width and layout, or the result says the page narrowed; live stream; observability dashboard), `save_pdf`.

**emulation** — `emulate`: viewport, device, geolocation, offline, headers, HTTP auth, color scheme, reduced motion, user agent, and network (slow-3g … fast-4g or custom latency/bandwidth) and CPU throttling — several in one call.

**storage** — `cookies` (list, set with url/domain/path/httpOnly/secure/sameSite/expires, clear, import), `storage` (local/session), `state` (save, load, list, show, rename, clear, clean), `clipboard`, `session` (info, list, profiles, connect over CDP), `auth` (use saved login profiles; secrets never pass through the model), `application` (IndexedDB databases and records, Cache Storage, service workers, web app manifest, storage usage and quota, clear site data).

### How the DevTools features work

`debugger`, `performance` (metrics, heap, coverage, Lighthouse), `application`, `console` issues and throttling talk to Chrome directly over the DevTools Protocol, using the CDP endpoint of the agent-browser session. The server keeps one connection per session on its active tab. Breakpoints, coverage recording and throttling live on that connection, so they last across tool calls until the browser closes. When the MCP server restarts (a reconnect or an update), the browser keeps running, but this per-connection state is released: throttling and breakpoints are dropped, and a paused page resumes unless a DevTools window also holds the pause. The server finds the active tab by its tab ID, which agent-browser can report even while the page is paused, so after a restart the `debugger` tool reattaches to a page that is still paused. While the debugger is on, it stays on the tab where you turned it on. If that tab is closed, the next call moves to the session's current tab and says the breakpoints are gone. While the page is paused, the other DevTools tools refuse with "resume first" rather than hanging, and every CDP call is bounded by `--timeout`.

### Choose the cheapest tool

The server sends this table to the model as instructions:

| Goal | Prefer | Avoid / only when |
|---|---|---|
| See page structure | `snapshot` (interactive, scoped) | `screenshot` — only for visual checks |
| Read content | `page_text` | full `snapshot`, `get html` |
| Act on an element | `click`/`fill` with `@ref` | `mouse` x,y — canvas or no-ref targets |
| Vision: locate what you see | `screenshot annotate:true`, then act by `@ref` | estimating coordinates |
| Several known steps | one `batch` call | one call per step |
| See what an action changed | `snapshot:"delta"` on the action, or `snapshot delta:true` | a fresh full snapshot |
| Wait for the page | `wait` for element/text/url/load | fixed sleeps |
| Debug API or JS | `network`/`console` with filter, pattern, limit | unfiltered dumps |
| Step through JavaScript | `debugger` breakpoint, trigger it, then `stack`/`scope`/`evaluate` | logging with `eval_script` |
| Find performance problems | `performance` vitals or metrics first; `lighthouse` for a full audit | trace/profiler files unless you need a deep dive |

## Upgrading from 1.x

2.0 merges the 150 single-purpose tools into 41 tools with an `action`/`by`/`what` parameter, for example `find_by_text_click` → `find {by:"text", action:"click"}` and `get_page_title` → `get {what:"title"}`. Old tool names are gone. It also fixes these bugs:

- back/forward ignored `session`
- multi-file upload sent one file
- `set_user_agent` navigated to about:blank
- element screenshots failed
- `find_all` always errored
- timeouts were not reported
- the default session was not closed on shutdown

## Architecture

```
MCP client → stdio → agent-browser-mcp (Go) → agent-browser CLI (--json) → browser
```

Each tool call becomes one or more `agent-browser <command> --json --session <name>` runs; results are reshaped into compact text (or an image for screenshots) and capped at `--max-output`.

## Development

```bash
go test ./...                                         # unit tests with a fake CLI and fake Chrome, ~2s
go test -tags integration ./internal/mcp/tools/       # real browser against local fixture sites
LIGHTHOUSE_PATH=$(which lighthouse) go test -tags integration ./internal/mcp/tools/   # also run the Lighthouse audit
```

Unit tests run every tool against a fake agent-browser that records argv and a fake Chrome DevTools endpoint that records protocol calls. A coverage test fails if any tool or any enum value has no test row, and a schema budget test keeps `tools/list` under 36 KB.

## License

MIT — see [LICENSE](./LICENSE).
