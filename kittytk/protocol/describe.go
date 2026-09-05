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
	// enum, word, color, units, stream, action, collection.
	Kind string
	// Default is the literal default rendered as text ("", "0", "false"),
	// or a note like "inherited" / "as-noted" when there is no fixed value.
	Default string
	// Doc is a brief, tooltip-length description of what the property does.
	Doc string
	// Enum lists the allowed words when Kind == "enum".
	Enum []string
	// Members lists the type names a collection accepts. Empty on a
	// collection means any non-virtual type -- a panel takes trinkets and
	// does not enumerate them.
	Members []string
}

// Property bundles a property's applier with its descriptor so a single
// registration is the source of both behavior and introspection. The
// typed registration helpers set Desc.Kind; call the fluent builders to
// add the default, doc, and enum.
//
// A property takes one of two shapes, and exactly one of Apply and Accept
// is set to say which:
//
//   - a VALUE property is written name=value and applies that value;
//   - a COLLECTION property is written name={ new ...; new ... } and
//     adopts each object the block builds.
//
// Both live in one table, so `children` is a property a type registers
// rather than a word the session knows, and a type that takes columns
// registers `columns` beside it.
type Property struct {
	Apply  PropertyApplier
	Accept CollectionAppender
	Desc   PropDesc
}

// NewProperty builds a Property from a value kind and an applier. The
// trinket registration helpers use it; callers add Tip/Def/OneOf.
func NewProperty(kind string, apply PropertyApplier) Property {
	return Property{Apply: apply, Desc: PropDesc{Kind: kind}}
}

// NewCollection builds a property that takes a {} block: accept adopts one
// built child into the parent. Callers add Tip and Members.
func NewCollection(accept CollectionAppender) Property {
	return Property{Accept: accept, Desc: PropDesc{Kind: "collection"}}
}

// Members declares the type names a collection accepts, so the refusal and
// the documented vocabulary come from one statement. A collection that
// names none takes any non-virtual type.
func (p Property) Members(types ...string) Property {
	p.Desc.Members = types
	return p
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

func descToInfo(name string, d PropDesc) PropInfo {
	return PropInfo{Name: name, Kind: d.Kind, Default: d.Default, Doc: d.Doc, Enum: d.Enum, Members: d.Members}
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
//	prop of="button" name="caption" kind=string default="" doc="..." enum="" members=""
//	event of="button" name="click" doc="..."
//	eventfield of="button" event="click" name="trinket" kind="uint" doc="..."
//
// Every property and event statement carries its owning type via of=,
// and an eventfield names its event as well, because the stream has no
// nesting to carry that relationship. enum= and members= are
// comma-separated lists: enum= holds the allowed words of an enum, and
// members= the types a collection accepts (empty means any trinket).
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
	sb.WriteString(" members=")
	sb.WriteString(Quote(strings.Join(p.Members, ",")))
	sb.WriteByte('\n')
}
