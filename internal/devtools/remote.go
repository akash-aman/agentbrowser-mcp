package devtools

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RemoteObject is a JavaScript value as CDP describes it.
type RemoteObject struct {
	Type                string          `json:"type"`
	Subtype             string          `json:"subtype"`
	ClassName           string          `json:"className"`
	Description         string          `json:"description"`
	Value               json.RawMessage `json:"value"`
	UnserializableValue string          `json:"unserializableValue"`
	ObjectID            string          `json:"objectId"`
	Preview             *ObjectPreview  `json:"preview"`
}

// ObjectPreview is CDP's shallow preview of an object's properties.
type ObjectPreview struct {
	Subtype     string `json:"subtype"`
	Description string `json:"description"`
	Overflow    bool   `json:"overflow"`
	Properties  []struct {
		Name  string `json:"name"`
		Type  string `json:"type"`
		Value string `json:"value"`
	} `json:"properties"`
}

// ExceptionDetails is a thrown exception from an evaluation.
type ExceptionDetails struct {
	Text      string        `json:"text"`
	Exception *RemoteObject `json:"exception"`
}

func (e *ExceptionDetails) Error() string {
	if e.Exception != nil && e.Exception.Description != "" {
		return firstLine(e.Exception.Description)
	}
	return e.Text
}

// String renders the value the way the DevTools console shows it, shortened.
func (o RemoteObject) String() string {
	switch {
	case o.UnserializableValue != "":
		return o.UnserializableValue
	case o.Type == "undefined":
		return "undefined"
	case o.Subtype == "null":
		return "null"
	case o.Type == "function":
		return "ƒ " + shorten(firstLine(o.Description), 80)
	case o.Type != "object" && len(o.Value) > 0:
		return shorten(string(o.Value), 200)
	case o.Preview != nil:
		return o.Preview.String(o.ClassName)
	}
	return shorten(o.Description, 200)
}

// Full renders the value like String but never shortens a primitive: the
// result of an evaluation is the answer itself, not a preview.
func (o RemoteObject) Full() string {
	if o.Type != "object" && o.Type != "function" && o.UnserializableValue == "" && len(o.Value) > 0 {
		return string(o.Value)
	}
	return o.String()
}

// String renders a preview as ClassName {a: 1, b: "x", …} or Array(2) [1, 2].
func (p ObjectPreview) String(className string) string {
	parts := make([]string, 0, len(p.Properties)+1)
	for _, prop := range p.Properties {
		v := prop.Value
		if prop.Type == "string" {
			v = fmt.Sprintf("%q", v)
		}
		if p.Subtype == "array" {
			parts = append(parts, v)
		} else {
			parts = append(parts, prop.Name+": "+v)
		}
	}
	if p.Overflow {
		parts = append(parts, "…")
	}
	if p.Subtype == "array" {
		return p.Description + " [" + strings.Join(parts, ", ") + "]"
	}
	label := className
	if label == "" || label == "Object" {
		label = ""
	} else {
		label += " "
	}
	return label + "{" + strings.Join(parts, ", ") + "}"
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

func shorten(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
