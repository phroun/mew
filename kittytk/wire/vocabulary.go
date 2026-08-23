package wire

// The shapes a describe stream carries, and the decoder that reads it.
// A client needs these to understand what a host answers; producing the
// stream is the host's side and lives beside the registry.

import "strings"

// PropInfo is one property in a described vocabulary.
type PropInfo struct {
	Name    string
	Kind    string
	Default string
	Doc     string
	Enum    []string
}

// EventFieldDesc is one field an event record carries. It names itself,
// so the same shape serves the registration and the described result —
// unlike PropDesc, which takes its name from the map key.
type EventFieldDesc struct {
	// Name is the field's name in the event record.
	Name string
	// Kind is the value's wire type: uint, int, string, word, flag.
	Kind string
	// Doc is a brief, tooltip-length description of the field.
	Doc string
}

// EventInfo is one event in a described vocabulary.
type EventInfo struct {
	Name   string
	Doc    string
	Fields []EventFieldDesc
}

// TypeInfo describes one registered type, its type-specific props, and
// the events it emits (common props are reported once at the vocabulary
// level).
type TypeInfo struct {
	Name    string
	Virtual bool
	Props   []PropInfo
	Events  []EventInfo
}

// Vocabulary is the full introspection result: the common properties
// every non-virtual type accepts, plus each registered type.
type Vocabulary struct {
	Common []PropInfo
	Types  []TypeInfo
}

// DecodeVocabulary parses the flat describe stream (the statements the
// describe verb emits, one per line) back into a Vocabulary. Lines are
// proptype/prop/propcommon/event/eventfield statements; unknown lines
// are ignored, so a newer host can add statement kinds without breaking
// an older client.
func DecodeVocabulary(lines []string) (*Vocabulary, error) {
	v := &Vocabulary{}
	byType := map[string]int{} // type name -> index in v.Types
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		script, err := Parse(line)
		if err != nil {
			return nil, err
		}
		for _, st := range script.Statements {
			switch st.Verb {
			case "proptype":
				name := stmtStr(st, "name")
				v.Types = append(v.Types, TypeInfo{Name: name, Virtual: stmtFlag(st, "virtual")})
				byType[name] = len(v.Types) - 1
			case "propcommon":
				v.Common = append(v.Common, stmtToPropInfo(st))
			case "prop":
				of := stmtStr(st, "of")
				if i, ok := byType[of]; ok {
					v.Types[i].Props = append(v.Types[i].Props, stmtToPropInfo(st))
				}
			case "event":
				of := stmtStr(st, "of")
				if i, ok := byType[of]; ok {
					v.Types[i].Events = append(v.Types[i].Events, EventInfo{
						Name: stmtStr(st, "name"),
						Doc:  stmtStr(st, "doc"),
					})
				}
			case "eventfield":
				// The event this belongs to was emitted just above it,
				// so it is the last one recorded for that type — but say
				// so by name rather than by position, since a stream may
				// have been filtered or reordered on the way here.
				i, ok := byType[stmtStr(st, "of")]
				if !ok {
					continue
				}
				evName := stmtStr(st, "event")
				for j := range v.Types[i].Events {
					if v.Types[i].Events[j].Name != evName {
						continue
					}
					v.Types[i].Events[j].Fields = append(v.Types[i].Events[j].Fields, EventFieldDesc{
						Name: stmtStr(st, "name"),
						Kind: stmtStr(st, "kind"),
						Doc:  stmtStr(st, "doc"),
					})
					break
				}
			}
		}
	}
	return v, nil
}

func stmtToPropInfo(st *Statement) PropInfo {
	p := PropInfo{
		Name:    stmtStr(st, "name"),
		Kind:    stmtStr(st, "kind"),
		Default: stmtStr(st, "default"),
		Doc:     stmtStr(st, "doc"),
	}
	if e := stmtStr(st, "enum"); e != "" {
		p.Enum = strings.Split(e, ",")
	}
	return p
}

func stmtStr(st *Statement, name string) string {
	for _, a := range st.Args {
		if a.Name == name && a.Value != nil && a.Value.Kind == StringValue {
			return a.Value.Str
		}
	}
	return ""
}

func stmtFlag(st *Statement, name string) bool {
	for _, a := range st.Args {
		if a.Name == name && a.Value == nil {
			return a.Flag == FlagTrue
		}
	}
	return false
}
