package protocol

import (
	"fmt"
	"strings"
)

// Event is a display-service → app record, following the same
// named-property discipline as commands (D10/D17): `event <type>
// field=value ...`. Its encoded form is a parseable statement, so the
// same tokenizer serves both directions of the wire.
type Event struct {
	Type   string
	Fields []*Arg
}

// NewEvent creates an event record of the given type ("click",
// "toggle", "change", "command", ...; see docs/property-vocabulary.md).
func NewEvent(eventType string) *Event {
	return &Event{Type: eventType}
}

// WithUint adds an integer field (object IDs, indices).
func (e *Event) WithUint(name string, v uint64) *Event {
	e.Fields = append(e.Fields, &Arg{Name: name, Value: &Value{
		Kind: NumberValue, Number: float64(v), IsInt: true,
	}})
	return e
}

// WithInt adds an integer field.
func (e *Event) WithInt(name string, v int) *Event {
	e.Fields = append(e.Fields, &Arg{Name: name, Value: &Value{
		Kind: NumberValue, Number: float64(v), IsInt: true,
	}})
	return e
}

// WithString adds a quoted-string field.
func (e *Event) WithString(name, s string) *Event {
	e.Fields = append(e.Fields, &Arg{Name: name, Value: &Value{
		Kind: StringValue, Str: s,
	}})
	return e
}

// WithWord adds an identifier/enum field.
func (e *Event) WithWord(name, w string) *Event {
	e.Fields = append(e.Fields, &Arg{Name: name, Value: &Value{
		Kind: WordValue, Word: w,
	}})
	return e
}

// WithFlag adds a flag field (D12/D16: true, false, or indeterminate).
func (e *Event) WithFlag(name string, state FlagState) *Event {
	e.Fields = append(e.Fields, &Arg{Name: name, Flag: state})
	return e
}

// field returns the named field, or nil.
func (e *Event) field(name string) *Arg {
	for _, a := range e.Fields {
		if a.Name == name {
			return a
		}
	}
	return nil
}

// Uint reads an integer field (false if absent or not an integer).
func (e *Event) Uint(name string) (uint64, bool) {
	a := e.field(name)
	if a == nil || a.Value == nil || a.Value.Kind != NumberValue || !a.Value.IsInt || a.Value.Number < 0 {
		return 0, false
	}
	return uint64(a.Value.Number), true
}

// Int reads an integer field.
func (e *Event) Int(name string) (int, bool) {
	a := e.field(name)
	if a == nil || a.Value == nil || a.Value.Kind != NumberValue || !a.Value.IsInt {
		return 0, false
	}
	return int(a.Value.Number), true
}

// Text reads a string field.
func (e *Event) Text(name string) (string, bool) {
	a := e.field(name)
	if a == nil || a.Value == nil || a.Value.Kind != StringValue {
		return "", false
	}
	return a.Value.Str, true
}

// Word reads an identifier/enum field.
func (e *Event) Word(name string) (string, bool) {
	a := e.field(name)
	if a == nil || a.Value == nil || a.Value.Kind != WordValue {
		return "", false
	}
	return a.Value.Word, true
}

// Flag reads a flag field; FlagNone means absent (unsaid).
func (e *Event) Flag(name string) FlagState {
	a := e.field(name)
	if a == nil || a.Value != nil {
		return FlagNone
	}
	return a.Flag
}

// Trinket reads the conventional trinket-identity field.
func (e *Event) Trinket() (uint64, bool) {
	if id, ok := e.Uint("trinket"); ok {
		return id, ok
	}
	// Window events name their source window= rather than trinket=;
	// both are ObjectIDs, and subscriptions key on the source.
	return e.Uint("window")
}

// Encode renders the event as protocol text: a parseable statement.
func (e *Event) Encode() string {
	var sb strings.Builder
	sb.WriteString("event ")
	sb.WriteString(e.Type)
	for _, a := range e.Fields {
		sb.WriteByte(' ')
		if a.Value == nil {
			switch a.Flag {
			case FlagFalse:
				sb.WriteByte('!')
			case FlagIndeterminate:
				sb.WriteByte('?')
			}
			sb.WriteString(a.Name)
			continue
		}
		sb.WriteString(a.Name)
		sb.WriteByte('=')
		switch a.Value.Kind {
		case WordValue:
			sb.WriteString(a.Value.Word)
		case NumberValue:
			if a.Value.IsInt {
				fmt.Fprintf(&sb, "%d", int64(a.Value.Number))
			} else {
				fmt.Fprintf(&sb, "%g", a.Value.Number)
			}
		case StringValue:
			sb.WriteString(quoteString(a.Value.Str))
		}
	}
	return sb.String()
}

// quoteString renders a string literal with the provisional escape set
// (matching the parser).
// Quote renders s as a protocol string literal (quotes + escapes,
// control bytes as \xNN). Script builders use it to interpolate
// arbitrary text safely.
func Quote(s string) string { return quoteString(s) }

func quoteString(s string) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		case '\n':
			sb.WriteString(`\n`)
		case '\t':
			sb.WriteString(`\t`)
		case '\r':
			sb.WriteString(`\r`)
		case 0x1b:
			sb.WriteString(`\e`)
		default:
			if r < 0x20 || r == 0x7f {
				// Control bytes as \xNN so any byte stream (terminal
				// feeds) round-trips through the text form.
				fmt.Fprintf(&sb, `\x%02x`, r)
			} else {
				sb.WriteRune(r)
			}
		}
	}
	sb.WriteByte('"')
	return sb.String()
}

// ParseEvent decodes an encoded event statement back into an Event.
func ParseEvent(src string) (*Event, error) {
	script, err := Parse(src)
	if err != nil {
		return nil, err
	}
	if len(script.Statements) != 1 {
		return nil, fmt.Errorf("expected one event statement, got %d", len(script.Statements))
	}
	st := script.Statements[0]
	if st.Verb != "event" || st.Key != "" {
		return nil, fmt.Errorf("not an event statement")
	}
	if len(st.Args) == 0 || st.Args[0].Value != nil || st.Args[0].Flag != FlagTrue {
		return nil, fmt.Errorf("event: missing type word")
	}
	return &Event{Type: st.Args[0].Name, Fields: st.Args[1:]}, nil
}

// EventDispatcher routes events to app-side handlers keyed by trinket
// ObjectID and event type (the app half of the event seam). It is
// instance-scoped: one dispatcher per connection.
type EventDispatcher struct {
	byTrinket map[uint64]map[string][]func(*Event)
	byType    map[string][]func(*Event)
}

// NewEventDispatcher creates an empty dispatcher.
func NewEventDispatcher() *EventDispatcher {
	return &EventDispatcher{
		byTrinket: make(map[uint64]map[string][]func(*Event)),
		byType:    make(map[string][]func(*Event)),
	}
}

// On subscribes to an event type from a specific trinket.
func (d *EventDispatcher) On(trinketID uint64, eventType string, fn func(*Event)) {
	m := d.byTrinket[trinketID]
	if m == nil {
		m = make(map[string][]func(*Event))
		d.byTrinket[trinketID] = m
	}
	m[eventType] = append(m[eventType], fn)
}

// OnType subscribes to all events of a type regardless of source
// (e.g. "command", window events).
func (d *EventDispatcher) OnType(eventType string, fn func(*Event)) {
	d.byType[eventType] = append(d.byType[eventType], fn)
}

// Dispatch routes one event; returns true if any handler ran.
func (d *EventDispatcher) Dispatch(ev *Event) bool {
	handled := false
	if id, ok := ev.Trinket(); ok {
		if m := d.byTrinket[id]; m != nil {
			for _, fn := range m[ev.Type] {
				fn(ev)
				handled = true
			}
		}
	}
	for _, fn := range d.byType[ev.Type] {
		fn(ev)
		handled = true
	}
	return handled
}
