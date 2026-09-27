package config

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/phroun/pawscript"
)

// StateFormat selects the serialization format for persisted editor state
// (preferences, sessions, and similar host-facing data).
type StateFormat int

const (
	// FormatPSL is the default: PawScript Serialized List, matching the
	// scripting ecosystem the editor is built on.
	FormatPSL StateFormat = iota
	// FormatJSON is offered for host applications that prefer a widely
	// supported interchange format.
	FormatJSON
)

// EncodeState serializes a state map in the requested format. Nested values
// may be plain map[string]interface{} / []interface{} or the pawscript
// PSLMap/PSLList types; both encode correctly in either format.
func EncodeState(data map[string]interface{}, format StateFormat) (string, error) {
	switch format {
	case FormatPSL:
		return pawscript.SerializePSLPretty(pawscript.PSLMap(data)) + "\n", nil
	case FormatJSON:
		plain, err := normalizeState(data, "")
		if err != nil {
			return "", err
		}
		out, err := json.MarshalIndent(plain, "", "  ")
		if err != nil {
			return "", err
		}
		return string(out) + "\n", nil
	}
	return "", fmt.Errorf("unknown state format %d", format)
}

// DecodeState parses serialized state in either format, auto-detected by the
// leading character: PSL documents start with '(' and JSON documents with
// '{'. Nested containers are normalized to plain map[string]interface{} and
// []interface{} regardless of source format, so callers always see one
// shape. (Numbers keep their decoder's native type: int64 from PSL, float64
// from JSON.)
//
// State is a set of NAMED values, so a PSL document with values that have no
// name is refused rather than filed under invented keys.
func DecodeState(content string) (map[string]interface{}, error) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return map[string]interface{}{}, nil
	}

	switch trimmed[0] {
	case '(':
		doc, err := pawscript.ParsePSL(trimmed)
		if err != nil {
			return nil, err
		}
		if doc.Len() > 0 {
			return nil, fmt.Errorf("state holds %d value(s) without a name at its top level; state is a set of named values", doc.Len())
		}
		return normalizeState(doc.Map(), "")
	case '{':
		var out map[string]interface{}
		if err := json.Unmarshal([]byte(trimmed), &out); err != nil {
			return nil, err
		}
		return out, nil
	}
	return nil, fmt.Errorf("unrecognized state format (expected '(' for PSL or '{' for JSON)")
}

// normalizeState converts nested PSL container types into plain maps and
// slices, in place, and returns the map. at names where in the document this
// map sits, for the message when something in it cannot be represented.
func normalizeState(m map[string]interface{}, at string) (map[string]interface{}, error) {
	for k, v := range m {
		nv, err := normalizeValue(v, member(at, k))
		if err != nil {
			return nil, err
		}
		m[k] = nv
	}
	return m, nil
}

// normalizeValue converts a single value's nested containers to plain types.
//
// A PSL list holds an ordered sequence and named members side by side. State
// uses one or the other in any one list, because JSON has no way to say both
// and the two formats have to mean the same thing here. A list holding both is
// named and refused rather than half-read.
func normalizeValue(v interface{}, at string) (interface{}, error) {
	switch t := v.(type) {
	case *pawscript.PSLNode:
		switch {
		case len(t.Items) == 0:
			return normalizeState(t.Named, at)
		case len(t.Named) == 0:
			return normalizeList(t.Items, at)
		}
		return nil, fmt.Errorf("state value %s holds %d named and %d unnamed value(s) in one list, which JSON has no way to say",
			at, len(t.Named), len(t.Items))
	case pawscript.PSLMap:
		return normalizeState(t, at)
	case map[string]interface{}:
		return normalizeState(t, at)
	case pawscript.PSLList:
		return normalizeList(t, at)
	case []interface{}:
		return normalizeList(t, at)
	}
	return v, nil
}

// normalizeList converts a list's nested containers to plain types, in place.
func normalizeList(l []interface{}, at string) ([]interface{}, error) {
	for i, v := range l {
		nv, err := normalizeValue(v, fmt.Sprintf("%s[%d]", at, i))
		if err != nil {
			return nil, err
		}
		l[i] = nv
	}
	return l, nil
}

// member names a value inside a state document, for a message about it.
func member(at, key string) string {
	if at == "" {
		return key
	}
	return at + "." + key
}
