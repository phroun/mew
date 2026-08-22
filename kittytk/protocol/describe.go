package protocol

import (
	"sort"
	"strings"
)

// Protocol introspection (D24): the host can be asked to describe its
// wire vocabulary - the trinket types it supports, and for each type the
// properties it accepts with each property's value kind, default, and a
// brief (tooltip-length) description. The reply is a stream of FLAT
// statements (no nested blocks), so the simplest clients can parse it
// line by line.

// PropDesc is the queryable descriptor for one wire property.
type PropDesc struct {
	// Kind is the value's wire type: string, int, float, bool, flag,
	// enum, word, color, units, stream, action.
	Kind string
	// Default is the literal default rendered as text ("", "0", "false"),
	// or a note like "inherited" / "as-noted" when there is no fixed value.
	Default string
	// Doc is a brief, tooltip-length description of what the property does.
	Doc string
	// Enum lists the allowed words when Kind == "enum".
	Enum []string
}

// Property bundles a property's applier with its descriptor so a single
// registration is the source of both behavior and introspection. The
// typed registration helpers set Desc.Kind; call the fluent builders to
// add the default, doc, and enum.
type Property struct {
	Apply PropertyApplier
	Desc  PropDesc
}

// NewProperty builds a Property from a value kind and an applier. The
// trinket registration helpers use it; callers add Tip/Def/OneOf.
func NewProperty(kind string, apply PropertyApplier) Property {
	return Property{Apply: apply, Desc: PropDesc{Kind: kind}}
}

// Tip sets the brief description. Returns the Property for chaining.
func (p Property) Tip(doc string) Property { p.Desc.Doc = doc; return p }

// Def sets the documented default (a literal, or a note like "inherited").
func (p Property) Def(def string) Property { p.Desc.Default = def; return p }

// OneOf declares the allowed enum words (and marks the kind "enum").
func (p Property) OneOf(words ...string) Property {
	p.Desc.Enum = words
	if p.Desc.Kind == "" || p.Desc.Kind == "word" {
		p.Desc.Kind = "enum"
	}
	return p
}

// As overrides the value kind (for raw appliers built without a helper).
func (p Property) As(kind string) Property { p.Desc.Kind = kind; return p }

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

// EventDesc is the queryable descriptor for one wire event: when it
// fires, and what it carries.
//
// A type declares its events beside its properties, so one registration
// is the source of both behavior and introspection — the arrangement
// Property already has. Without this an event exists only as a string
// literal inside a Bind closure, where nothing can ask about it and a
// client has to read the host's source to learn the event even exists.
type EventDesc struct {
	// Doc is a brief description of when the event fires.
	Doc string
	// Fields are the record's fields, in the order they are documented.
	Fields []EventFieldDesc
}

// NewEventDesc builds an EventDesc from its description; add fields with
// Field.
func NewEventDesc(doc string) EventDesc { return EventDesc{Doc: doc} }

// Field appends one field to the descriptor and returns it for chaining.
//
// The append copies rather than growing in place. An EventDesc is a
// value, written into map literals and passed around by copy, so a chain
// continuing from a shared backing array would write over a sibling's
// fields instead of its own.
func (e EventDesc) Field(name, kind, doc string) EventDesc {
	fields := make([]EventFieldDesc, len(e.Fields), len(e.Fields)+1)
	copy(fields, e.Fields)
	e.Fields = append(fields, EventFieldDesc{Name: name, Kind: kind, Doc: doc})
	return e
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

func descToInfo(name string, d PropDesc) PropInfo {
	return PropInfo{Name: name, Kind: d.Kind, Default: d.Default, Doc: d.Doc, Enum: d.Enum}
}

func sortedPropInfos(props map[string]Property) []PropInfo {
	names := make([]string, 0, len(props))
	for n := range props {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]PropInfo, 0, len(names))
	for _, n := range names {
		out = append(out, descToInfo(n, props[n].Desc))
	}
	return out
}

func sortedEventInfos(events map[string]EventDesc) []EventInfo {
	if len(events) == 0 {
		return nil
	}
	names := make([]string, 0, len(events))
	for n := range events {
		names = append(names, n)
	}
	sort.Strings(names)
	out := make([]EventInfo, 0, len(names))
	for _, n := range names {
		d := events[n]
		out = append(out, EventInfo{Name: n, Doc: d.Doc, Fields: d.Fields})
	}
	return out
}

// DescribeVocabulary returns the registered wire vocabulary: common
// properties plus every type, each with its type-specific properties and
// the events it emits. Types, properties and events are sorted for
// deterministic output; an event's fields keep their declared order,
// which is the order they are worth reading in.
func DescribeVocabulary() *Vocabulary {
	regMu.RLock()
	defer regMu.RUnlock()

	v := &Vocabulary{Common: sortedPropInfos(regCommon)}
	names := make([]string, 0, len(regTypes))
	for n := range regTypes {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		spec := regTypes[n]
		v.Types = append(v.Types, TypeInfo{
			Name:    n,
			Virtual: spec.Virtual,
			Props:   sortedPropInfos(spec.Props),
			Events:  sortedEventInfos(spec.Events),
		})
	}
	return v
}

// EncodeVocabulary renders a Vocabulary as a stream of FLAT wire
// statements (one per line, no nested blocks) so the simplest clients
// can parse it line by line:
//
//	propcommon name="enabled" kind=flag default="true" doc="..."
//	proptype name="button" virtual=false
//	prop of="button" name="caption" kind=string default="" doc="..." enum=""
//	event of="button" name="click" doc="..."
//	eventfield of="button" event="click" name="trinket" kind="uint" doc="..."
//
// Every property and event statement carries its owning type via of=,
// and an eventfield names its event as well, because the stream has no
// nesting to carry that relationship. enum= is a comma-separated list
// (empty unless kind is enum).
func EncodeVocabulary(v *Vocabulary) string {
	var sb strings.Builder
	for _, p := range v.Common {
		writePropStmt(&sb, "propcommon", "", p)
	}
	for _, t := range v.Types {
		sb.WriteString("proptype name=")
		sb.WriteString(Quote(t.Name))
		if t.Virtual {
			sb.WriteString(" virtual")
		} else {
			sb.WriteString(" !virtual")
		}
		sb.WriteByte('\n')
		for _, p := range t.Props {
			writePropStmt(&sb, "prop", t.Name, p)
		}
		for _, e := range t.Events {
			sb.WriteString("event of=")
			sb.WriteString(Quote(t.Name))
			sb.WriteString(" name=")
			sb.WriteString(Quote(e.Name))
			sb.WriteString(" doc=")
			sb.WriteString(Quote(e.Doc))
			sb.WriteByte('\n')
			for _, f := range e.Fields {
				sb.WriteString("eventfield of=")
				sb.WriteString(Quote(t.Name))
				sb.WriteString(" event=")
				sb.WriteString(Quote(e.Name))
				sb.WriteString(" name=")
				sb.WriteString(Quote(f.Name))
				sb.WriteString(" kind=")
				sb.WriteString(Quote(f.Kind))
				sb.WriteString(" doc=")
				sb.WriteString(Quote(f.Doc))
				sb.WriteByte('\n')
			}
		}
	}
	return sb.String()
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

func writePropStmt(sb *strings.Builder, verb, of string, p PropInfo) {
	sb.WriteString(verb)
	if of != "" {
		sb.WriteString(" of=")
		sb.WriteString(Quote(of))
	}
	sb.WriteString(" name=")
	sb.WriteString(Quote(p.Name))
	sb.WriteString(" kind=")
	sb.WriteString(Quote(p.Kind))
	sb.WriteString(" default=")
	sb.WriteString(Quote(p.Default))
	sb.WriteString(" doc=")
	sb.WriteString(Quote(p.Doc))
	sb.WriteString(" enum=")
	sb.WriteString(Quote(strings.Join(p.Enum, ",")))
	sb.WriteByte('\n')
}
