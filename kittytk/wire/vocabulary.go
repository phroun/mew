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
	// Members are the types a collection property accepts; empty on a
	// collection means any trinket.
	Members []string
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

// AskInfo describes one question a type answers.
type CallInfo struct {
	Name    string
	Doc     string
	Args    []EventFieldDesc
	Answers []string
}

// AskInfo is a question a type answers; DoInfo is an action it performs. One
// shape, because they are the same declaration under a different verb.
type (
	AskInfo = CallInfo
	DoInfo  = CallInfo
)

// TypeInfo describes one registered type, its type-specific props, and
// the events it emits (common props are reported once at the vocabulary
// level).
type TypeInfo struct {
	Name    string
	Virtual bool
	// Hosted marks a type the wire cannot construct: the host registers
	// an instance and hands over its ID, and `new <name>` is refused.
	Hosted bool
	Props  []PropInfo
	Asks   []AskInfo
	Does   []DoInfo
	Events []EventInfo
}

// Vocabulary is the full introspection result: the common properties
// every non-virtual type accepts, plus each registered type.
type Vocabulary struct {
	Common []PropInfo
	Types  []TypeInfo
}

// DecodeVocabulary parses the flat describe stream (the statements the
// describe verb emits, one per line) back into a Vocabulary. Lines are
// proptype/prop/propcommon/ask/askarg/do/doarg/event/eventfield statements; unknown lines
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
				v.Types = append(v.Types, TypeInfo{
					Name:    name,
					Virtual: stmtFlag(st, "virtual"),
					Hosted:  stmtFlag(st, "hosted"),
				})
				byType[name] = len(v.Types) - 1
			case "propcommon":
				v.Common = append(v.Common, stmtToPropInfo(st))
			case "prop":
				of := stmtStr(st, "of")
				if i, ok := byType[of]; ok {
					v.Types[i].Props = append(v.Types[i].Props, stmtToPropInfo(st))
				}
			case "ask", "do":
				of := stmtStr(st, "of")
				i, ok := byType[of]
				if !ok {
					continue
				}
				c := CallInfo{
					Name: stmtStr(st, "name"),
					Doc:  stmtStr(st, "doc"),
				}
				if st.Verb == "ask" {
					c.Answers = splitList(stmtStr(st, "answers"))
					v.Types[i].Asks = append(v.Types[i].Asks, c)
				} else {
					v.Types[i].Does = append(v.Types[i].Does, c)
				}
			case "askarg", "doarg":
				i, ok := byType[stmtStr(st, "of")]
				if !ok {
					continue
				}
				calls := v.Types[i].Asks
				name := stmtStr(st, "ask")
				if st.Verb == "doarg" {
					calls, name = v.Types[i].Does, stmtStr(st, "do")
				}
				for j := range calls {
					if calls[j].Name != name {
						continue
					}
					calls[j].Args = append(calls[j].Args, EventFieldDesc{
						Name: stmtStr(st, "name"),
						Kind: stmtStr(st, "kind"),
						Doc:  stmtStr(st, "doc"),
					})
					break
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

// splitList reads a comma-separated field, empty for an empty one.
func splitList(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ",")
}

func stmtToPropInfo(st *Statement) PropInfo {
	p := PropInfo{
		Name:    stmtStr(st, "name"),
		Kind:    stmtStr(st, "kind"),
		Default: stmtStr(st, "default"),
		Doc:     stmtStr(st, "doc"),
	}
	if e := stmtStr(st, "enum"); e != "" {
		p.Enum = splitList(e)
	}
	if m := stmtStr(st, "members"); m != "" {
		p.Members = splitList(m)
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
