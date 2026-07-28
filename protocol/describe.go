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

// TypeInfo describes one registered type and its type-specific props
// (common props are reported once at the vocabulary level).
type TypeInfo struct {
	Name    string
	Virtual bool
	Props   []PropInfo
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

// DescribeVocabulary returns the registered wire vocabulary: common
// properties plus every type, each with its type-specific properties.
// Types and properties are sorted for deterministic output.
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
//
// Every property statement carries its owning type via of=. enum= is a
// comma-separated list (empty unless kind is enum).
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
	}
	return sb.String()
}

// DecodeVocabulary parses the flat describe stream (the statements the
// describe verb emits, one per line) back into a Vocabulary. Lines are
// proptype/prop/propcommon statements; unknown lines are ignored.
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
