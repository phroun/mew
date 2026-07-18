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
		out, err := json.MarshalIndent(normalizeState(data), "", "  ")
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
func DecodeState(content string) (map[string]interface{}, error) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return map[string]interface{}{}, nil
	}

	switch trimmed[0] {
	case '(':
		parsed, err := pawscript.ParsePSL(trimmed)
		if err != nil {
			return nil, err
		}
		return normalizeState(parsed), nil
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
// slices, in place, and returns the map.
func normalizeState(m map[string]interface{}) map[string]interface{} {
	for k, v := range m {
		m[k] = normalizeValue(v)
	}
	return m
}

// normalizeValue converts a single value's nested containers to plain types.
func normalizeValue(v interface{}) interface{} {
	switch t := v.(type) {
	case pawscript.PSLMap:
		return normalizeState(t)
	case map[string]interface{}:
		return normalizeState(t)
	case pawscript.PSLList:
		return normalizeList(t)
	case []interface{}:
		return normalizeList(t)
	}
	return v
}

// normalizeList converts a list's nested containers to plain types, in place.
func normalizeList(l []interface{}) []interface{} {
	for i, v := range l {
		l[i] = normalizeValue(v)
	}
	return l
}
