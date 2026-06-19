# agent-browser-mcp

An [MCP](https://modelcontextprotocol.io/) server that exposes every [agent-browser](https://github.com/vercel-labs/agent-browser) CLI command as MCP tools. Lets AI assistants control a browser through a standardized tool interface over `stdio`.

Built with Go, powered by [`mark3labs/mcp-go`](https://github.com/mark3labs/mcp-go).

## Prerequisites

- [agent-browser](https://github.com/anthropics/agent-browser) CLI installed and on `PATH`
- Go 1.26+ (to build)

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

## Configuration

Settings are loaded from environment variables with optional CLI flag overrides.

| Environment Variable | CLI Flag | Default | Description |
|---|---|---|---|
| `AGENT_BROWSER_MCP_NAME` | `--name` | `agent-browser-mcp` | Server identity name |
| `AGENT_BROWSER_MCP_PROJECT` | `--project` | — | Project this browser serves |
| `AGENT_BROWSER_MCP_PURPOSE` | `--purpose` | — | Purpose of this instance |
| `AGENT_BROWSER_MCP_BROWSER_PATH` | `--agent-browser-path` | `agent-browser` | Path to agent-browser binary |
| `AGENT_BROWSER_MCP_SESSION` | `--session` | — | Default session name |
| `AGENT_BROWSER_SESSION_NAME` | `--session-name` | — | Session persistence name |
| `AGENT_BROWSER_PROFILE` | `--profile` | — | Chrome profile name or path |
| `AGENT_BROWSER_STATE` | `--state` | — | Storage state file path |
| `AGENT_BROWSER_ENGINE` | `--engine` | — | Browser engine (`chrome`, `lightpanda`) |
| `AGENT_BROWSER_PROVIDER` | `--provider` | — | Cloud provider (`browserless`, `browserbase`, `browseruse`, `kernel`, `agentcore`, `ios`) |
| `AGENT_BROWSER_HEADED` | `--headed` | `false` | Show browser window |
| `AGENT_BROWSER_EXECUTABLE_PATH` | `--executable-path` | — | Custom browser binary path |
| `AGENT_BROWSER_PROXY` | `--proxy` | — | Proxy URL |
| `AGENT_BROWSER_MCP_TIMEOUT` | `--timeout` | `60000` | Command timeout in ms |

## Usage

Run as an MCP server — it speaks MCP over `stdio`:

```bash
agent-browser-mcp --headed --timeout 30000
```

### MCP Client Setup

**Claude Desktop** — add to `claude_desktop_config.json`:

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

**Cline / VSCode** — add to MCP settings:

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

## Tools

The server registers 22 tool categories (~80+ tools total).

### Core Interaction
`navigate_page`, `click`, `double_click`, `fill`, `type_text`, `press_key`, `hover`, `focus`, `check`, `uncheck`, `select_option`, `scroll`, `scroll_into_view`, `drag`, `upload_file`, `close_browser`, `eval_script`

### Navigation
`navigate_back`, `navigate_forward`, `reload_page`, `spa_navigate`

### Page Information
`take_snapshot`, `get_text`, `get_html`, `get_value`, `get_attribute`, `get_page_title`, `get_current_url`, `get_cdp_url`, `get_element_count`, `get_bounding_box`, `get_computed_styles`, `is_visible`, `is_enabled`, `is_checked`

### Find by Selector
`find_by_text_{click,fill,type,hover,focus,check,uncheck,text}`, `find_by_label_*`, `find_by_placeholder_*`, `find_by_role_*`, `find_first`

### Screenshot & PDF
`take_screenshot`, `save_pdf`

### Network
`network_requests`, `network_request_detail`, `network_route`, `network_unroute`, `network_har_start`, `network_har_stop`

### Console
`console_messages`, `page_errors`

### Dialogs
`dialog_status`, `dialog_accept`, `dialog_dismiss`

### Cookies
`cookies_list`, `cookies_set`, `cookies_clear`, `cookies_import`

### Storage
`local_storage_{list,set,clear}`, `session_storage_{list,set,clear}`

### Waiting
`wait_for_element`, `wait_for_text`, `wait_for_url`, `wait_for_function`, `wait_for_load`, `wait_for_time`

### Tabs
`new_tab`, `list_tabs`, `switch_tab`, `close_tab`

### Clipboard
`clipboard_read`, `clipboard_write`, `clipboard_copy`, `clipboard_paste`

### Keyboard & Mouse
`keyboard_type`, `keyboard_insert`, `key_down`, `key_up`, `mouse_move`, `mouse_down`, `mouse_up`, `mouse_wheel`

### Emulation
`set_viewport`, `set_device`, `set_user_agent`, `set_geolocation`, `set_color_scheme`, `set_offline`, `set_headers`, `set_credentials`

### Session
`session_list`, `session_info`, `profiles_list`

### State
`state_save`, `state_load`, `state_list`, `state_show`, `state_clear`

### Diff
`diff_snapshot`, `diff_screenshot`, `diff_urls`

### Performance
`performance_start_trace`, `performance_stop_trace`, `web_vitals`, `profiler_start`, `profiler_stop`

### Security
`security_check`, `security_headers`, `security_cookies`

### Help & Debug
`help_discover`, `help_doctor`, `help_skills`, `highlight_element`, `open_devtools`, `get_cdp_url`

Every tool accepts an optional `session` parameter for isolated browser instances.

## Architecture

```
AI Assistant → MCP stdio → agent-browser-mcp (Go) → agent-browser CLI → Browser
```

The Go server:
1. Receives MCP tool calls over `stdio`
2. Routes to the correct tool handler
3. Executes `agent-browser <subcommand> --json --session <name>` as a subprocess
4. Parses JSON output and returns it as the tool result

Session state (cookies, local storage, tabs) persists in the browser between calls. The server handles graceful shutdown on SIGINT/SIGTERM, closing all active sessions.

## Design for Non-Vision Models

All tools return structured text output — accessibility snapshots, network request/response bodies, console messages — so non-vision AI models can effectively use browser automation without needing screenshots.

## License

MIT — see [LICENSE](./LICENSE).
