package tools

import (
	"strings"
	"testing"

	"github.com/xcode-studio/agentbrowser-mcp/internal/testutil/fakecdp"
)

// captureFixture: enabling Network replays a failed POST with cookie
// decisions and a WebSocket conversation; replaying the POST succeeds. It
// also answers the back/forward cache test and the security state.
func captureFixture(s *fakecdp.Server) {
	r := func(result any, events ...ev) fakecdp.Reply { return fakecdp.Reply{Result: result, Events: events} }
	sent := func(id string) ev {
		return ev{Method: "Network.requestWillBeSent", Params: a{"requestId": id, "type": "Fetch",
			"request":   a{"url": "http://fake/api/cart", "method": "POST", "headers": a{"Content-Type": "application/json", "Authorization": "Bearer x", "Host": "fake", "Sec-Fetch-Mode": "cors"}, "postData": `{"id":1}`},
			"initiator": a{"type": "script", "stack": a{"callFrames": []any{a{"functionName": "save", "scriptId": "7", "url": "http://fake/app.js", "lineNumber": 11, "columnNumber": 2}}}}}}
	}
	status := func(id string, code int) ev {
		return ev{Method: "Network.responseReceived", Params: a{"requestId": id, "response": a{"status": code, "mimeType": "application/json"}}}
	}
	done := func(id string) ev { return ev{Method: "Network.loadingFinished", Params: a{"requestId": id}} }
	s.Reply("Network.enable", r(a{},
		sent("R1"),
		ev{Method: "Network.requestWillBeSentExtraInfo", Params: a{"requestId": "R1", "associatedCookies": []any{
			a{"cookie": a{"name": "session"}, "blockedReasons": []any{}},
			a{"cookie": a{"name": "tracker"}, "blockedReasons": []any{"SameSiteLax"}},
		}}},
		status("R1", 500),
		ev{Method: "Network.responseReceivedExtraInfo", Params: a{"requestId": "R1", "blockedCookies": []any{a{"cookieLine": "pref=1; Secure", "blockedReasons": []any{"SecureOnly"}}}}},
		done("R1"),
		ev{Method: "Network.requestWillBeSent", Params: a{"requestId": "R3", "type": "XHR", "request": a{"url": "http://fake/api/xhr", "method": "GET"}}},
		ev{Method: "Network.responseReceived", Params: a{"requestId": "R3", "response": a{"status": 404, "mimeType": "text/plain"}}},
		done("R3"),
		ev{Method: "Network.webSocketCreated", Params: a{"requestId": "W1", "url": "wss://fake/live"}},
		ev{Method: "Network.webSocketFrameSent", Params: a{"requestId": "W1", "response": a{"opcode": 1, "payloadData": "ping"}}},
		ev{Method: "Network.webSocketFrameReceived", Params: a{"requestId": "W1", "response": a{"opcode": 1, "payloadData": `{"price":5}`}}},
	))
	s.Reply("Network.replayXHR", r(a{},
		ev{Method: "Network.requestWillBeSent", Params: a{"requestId": "R4", "type": "XHR", "request": a{"url": "http://fake/api/xhr", "method": "GET"}}},
		ev{Method: "Network.responseReceived", Params: a{"requestId": "R4", "response": a{"status": 200, "mimeType": "text/plain"}}},
		done("R4")))
	s.Reply("Network.getResponseBody", r(a{"body": `{"error":"price missing"}`, "base64Encoded": false}))

	s.Reply("Page.getNavigationHistory", r(a{"currentIndex": 0, "entries": []any{a{"id": 1, "url": "http://fake/"}}}))
	loaded := ev{Method: "Page.loadEventFired", Params: a{}}
	s.Reply("Page.navigate", r(a{}, loaded))
	s.Reply("Page.navigateToHistoryEntry", r(a{},
		ev{Method: "Page.backForwardCacheNotUsed", Params: a{"notRestoredExplanations": []any{a{"type": "PageSupportNeeded", "reason": "WebSocket"}}}}, loaded))
	s.Reply("Security.enable", r(a{}, ev{Method: "Security.visibleSecurityStateChanged", Params: a{"visibleSecurityState": a{
		"securityState": "secure", "securityStateIssueIds": []any{},
		"certificateSecurityState": a{"protocol": "TLS 1.3", "keyExchange": "", "keyExchangeGroup": "X25519", "cipher": "AES_128_GCM",
			"subjectName": "fake.test", "issuer": "Fake CA", "validTo": 1893456000},
	}}}))
}

func init() {
	cdpCases = append(cdpCases, captureCases...)
}

var captureCases = []cdpCase{
	{tool: "network", args: a{"action": "capture"}, want: []string{"Network.enable"}, text: "capturing request initiators, cookie decisions and WebSocket/EventSource messages"},
	{tool: "network", args: a{"action": "capture"}, prior: []toolCall{{"network", a{"action": "capture"}}}, text: "already capturing"},
	{tool: "network", args: a{"action": "websockets"}, text: "2 messages (showing the last 2):"},
	{tool: "network", args: a{"action": "websockets", "filter": "price"}, text: "received wss://fake/live  {\"price\":5}"},
	{tool: "network", args: a{"action": "initiator", "filter": "api/cart"}, text: "POST http://fake/api/cart\ninitiator: script save (http://fake/app.js:12:3)"},
	{tool: "network", args: a{"action": "cookies", "requestId": "R1"},
		text: "sent:\n  session\nnot sent:\n  tracker: SameSiteLax\nSet-Cookie refused:\n  pref=1: SecureOnly"},
	{tool: "network", args: a{"action": "curl", "filter": "cart"},
		text: "curl 'http://fake/api/cart' \\\n  -X POST \\\n  -H 'Authorization: <redacted>' \\\n  -H 'Content-Type: application/json' \\\n  -H 'Host: fake' \\\n  -H 'Sec-Fetch-Mode: cors' \\\n  --data-raw '{\"id\":1}'\n(credentials redacted"},
	// fetch() may not set Host or Sec-* headers, so the copy leaves them out.
	{tool: "network", args: a{"action": "fetch", "filter": "cart"},
		text: "\"headers\": {\n    \"Authorization\": \"\u003credacted\u003e\",\n    \"Content-Type\": \"application/json\"\n  },"},
	// fetch is re-issued from the page; Chrome only replays XHRs itself.
	{tool: "network", args: a{"action": "replay", "filter": "cart"}, text: "replayed POST http://fake/api/cart: 200 application/json (was 500)",
		check: func(t *testing.T, s *fakecdp.Server) {
			t.Helper()
			expr, _ := s.Params("Runtime.evaluate")["expression"].(string)
			if !strings.Contains(expr, `await fetch("http://fake/api/cart", {"body":"{\"id\":1}","cache":"no-store","credentials":"include","headers":{"Authorization":"Bearer x","Content-Type":"application/json"},"method":"POST"})`) {
				t.Errorf("refetch: %s", expr)
			}
		}},
	{tool: "network", args: a{"action": "replay", "filter": "api/xhr"}, text: "replayed GET http://fake/api/xhr: 200 text/plain (was 404)",
		check: params("Network.replayXHR", a{"requestId": "R3"})},
	{tool: "network", args: a{"action": "search", "query": "price missing"}, text: "http://fake/api/cart  …{\"error\":\"price missing\"}…"},
	{tool: "network", args: a{"action": "initiator", "filter": "nothing-like-this"}, wantErr: "no captured request with id or URL containing"},
	{tool: "network", args: a{"action": "initiator"}, wantErr: "requestId or filter is required for initiator"},
	{tool: "network", args: a{"action": "search"}, wantErr: "query is required for search"},

	{tool: "application", args: a{"action": "bfcache"},
		text:  "http://fake/ was not restored from the back/forward cache, so going back reloads it. Chrome's reasons (PageSupportNeeded ones the page can fix):\n- WebSocket (PageSupportNeeded)",
		check: params("Page.navigateToHistoryEntry", a{"entryId": 1.0})},
	{tool: "application", args: a{"action": "frames"},
		text: "Frames:\nhttp://fake/  origin http://fake, InsecureScheme\n  http://ads.fake/frame.html (name \"ads\")  origin http://ads.fake, InsecureScheme"},
	{tool: "application", args: a{"action": "security"},
		text: "security state: secure\nconnection: TLS 1.3, X25519, AES_128_GCM\ncertificate: fake.test issued by Fake CA, valid until 2030-01-01"},
}
