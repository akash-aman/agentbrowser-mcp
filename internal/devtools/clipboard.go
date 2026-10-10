package devtools

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"strings"
)

// focusStateJS reports what copy and paste would act on: the focused
// element, whether it takes typing, and whether anything is selected. Email
// and number inputs do not expose their selection, so it is unknown there.
const focusStateJS = `(() => {
  const a = document.activeElement;
  let label = "";
  if (a && a !== document.body) {
    label = a.localName;
    if (a.id) label += "#" + a.id;
  }
  const field = !!a && /^(input|textarea)$/.test(a.localName);
  const editable = !!a && (a.isContentEditable || (field && !a.readOnly && !a.disabled));
  let selected = String(getSelection()) !== "";
  if (field) selected = a.selectionStart === null ? null : a.selectionEnd > a.selectionStart;
  return { label, editable, selected };
})()`

type focusState struct {
	Label    string `json:"label"`
	Editable bool   `json:"editable"`
	Selected *bool  `json:"selected"`
}

// EditCommand copies the selection or pastes into the focused element by
// running Chrome's editing command with the shortcut's key event, as a user
// pressing Ctrl+C or Ctrl+V does. The key event alone reaches the page but
// never runs the command, so the clipboard and the field stayed as they were.
// The page should be focused, with clipboard access granted, to read back
// what was copied or pasted.
func (p *Page) EditCommand(ctx context.Context, command string) (string, error) {
	key := map[string]string{"copy": "c", "paste": "v"}[command]
	if key == "" {
		return "", fmt.Errorf("unknown editing command %q", command)
	}
	var state focusState
	if err := p.evaluateValue(ctx, focusStateJS, &state); err != nil {
		return "", err
	}
	where := state.Label
	if where == "" {
		where = "the page"
	}
	switch {
	case command == "copy" && state.Selected != nil && !*state.Selected:
		return "", fmt.Errorf("nothing is selected in %s to copy", where)
	case command == "paste" && !state.Editable:
		return "", fmt.Errorf("nothing editable is focused to paste into (focus is on %s); focus an input first", where)
	}
	var before string
	if command == "paste" {
		if err := p.evaluateValue(ctx, "navigator.clipboard.readText()", &before); err != nil {
			return "", fmt.Errorf("read the clipboard: %w", err)
		}
	}
	modifier := ModControl
	if runtime.GOOS == "darwin" {
		modifier = ModMeta
	}
	if err := p.KeyCommand(ctx, key, modifier, command); err != nil {
		return "", err
	}
	if command == "paste" {
		return fmt.Sprintf("pasted %s into %s", quoteShort(before), where), nil
	}
	var copied string
	if err := p.evaluateValue(ctx, "navigator.clipboard.readText()", &copied); err != nil {
		return "copied the selection in " + where, nil
	}
	return fmt.Sprintf("copied %s from %s", quoteShort(copied), where), nil
}

// Key event modifier bits.
const (
	ModAlt     = 1
	ModControl = 2
	ModMeta    = 4
	ModShift   = 8
)

// KeyCommand presses a letter key with modifiers and runs the editing
// command (selectAll, copy, cut, paste, undo, redo) it stands for. On macOS
// those shortcuts come from the app menu, so a key event alone reaches the
// page but never runs the command; Playwright sends commands the same way.
func (p *Page) KeyCommand(ctx context.Context, letter string, modifiers int, command string) error {
	upper := strings.ToUpper(letter)
	key := letter
	if modifiers&ModShift != 0 {
		key = upper
	}
	event := map[string]any{"key": key, "code": "Key" + upper, "modifiers": modifiers, "windowsVirtualKeyCode": int(upper[0])}
	down := map[string]any{"type": "keyDown", "commands": []string{command}}
	up := map[string]any{"type": "keyUp"}
	for _, e := range []map[string]any{down, up} {
		for k, v := range event {
			e[k] = v
		}
		if err := p.call(ctx, "Input.dispatchKeyEvent", e, nil); err != nil {
			return err
		}
	}
	return nil
}

// evaluateValue evaluates expr in the page, awaiting a promise, as a user
// gesture, and decodes the result's value into out.
func (p *Page) evaluateValue(ctx context.Context, expr string, out any) error {
	var res struct {
		Result           RemoteObject      `json:"result"`
		ExceptionDetails *ExceptionDetails `json:"exceptionDetails"`
	}
	params := map[string]any{"expression": expr, "returnByValue": true, "awaitPromise": true, "userGesture": true}
	if err := p.call(ctx, "Runtime.evaluate", params, &res); err != nil {
		return err
	}
	if res.ExceptionDetails != nil {
		return res.ExceptionDetails
	}
	return json.Unmarshal(res.Result.Value, out)
}

func quoteShort(s string) string {
	return fmt.Sprintf("%q", shorten(s, 200))
}
