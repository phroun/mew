package protocol

import (
	"fmt"
	"strings"
)

// Object is a UI object under construction, provided by a Factory.
// The trinket binder implements this against real trinkets; tests use
// mocks. Set receives either a value (flag == FlagNone) or a flag
// assertion (value == nil); typed conversion and property validation
// are the binder's job.
type Object interface {
	Set(name string, value *Value, flag FlagState) error
	Append(child Object) error
	ID() uint64
}

// Factory creates objects for builtin (lowercase) type names.
type Factory interface {
	New(typeName string) (Object, error)
}

// EventControl is an optional Factory capability: the session's
// sub/unsub verbs configure event flow through it, and Suppressed
// wraps wire-initiated property application so mutations never echo
// back as events (D20). RegistryFactory implements it.
type EventControl interface {
	Subscribe(trinketID uint64, eventType string)
	Unsubscribe(trinketID uint64, eventType string)
	Suppressed(f func())
}

// destroyer is an optional Object capability backing the destroy verb.
type destroyer interface {
	Destroy() error
}

// Reply reports server-assigned IDs for a request: top-level
// correlation keys plus explicitly surfaced names (D11/D15). Extra
// carries additional raw wire statements a verb wants delivered ahead
// of the reply line (the describe verb's flat vocabulary stream, D24).
type Reply struct {
	IDs   map[string]uint64
	Extra []string
}

// Session holds connection-scoped interpretation state: alias and
// template dictionaries (D10/D14), plus — since D19's verbs — the
// persistent key table and object table. Keys registered by one
// Execute remain addressable by later ones (`set root.status ...` in
// a follow-up batch); each Reply still reports only the keys and
// surfacings of its own request. Re-registering a key shadows the
// old binding.
type Session struct {
	aliases   map[string]string
	templates map[string]*templateDef
	keys      map[string]uint64
	objects   map[uint64]Object
}

type templateDef struct {
	base string // builtin or another template name
	args []*Arg
}

// Register injects a pre-existing object into the session's object table so
// verbs (set, sub, destroy) can address it by ID without it having been
// created via `new`. The host uses this to make the connection's Application
// settable over the wire - its ID travels to the client in the handshake.
func (s *Session) Register(obj Object) {
	if obj != nil {
		s.objects[obj.ID()] = obj
	}
}

// Object returns the object registered under id, if any. The host uses it at
// window-adoption time to resolve wire references such as a window's owner id.
func (s *Session) Object(id uint64) (Object, bool) {
	obj, ok := s.objects[id]
	return obj, ok
}

// NewSession creates an empty session.
func NewSession() *Session {
	return &Session{
		aliases:   make(map[string]string),
		templates: make(map[string]*templateDef),
		keys:      make(map[string]uint64),
		objects:   make(map[uint64]Object),
	}
}

func isUpperInitial(name string) bool {
	return name != "" && name[0] >= 'A' && name[0] <= 'Z'
}

func isLowerInitial(name string) bool {
	return name != "" && (name[0] >= 'a' && name[0] <= 'z' || name[0] == '_')
}

// execState is the per-request state: the reply under construction.
// (The key table became session-persistent with D19.)
type execState struct {
	reply *Reply
}

// Execute runs a parsed script against the factory, applying and
// updating session state (aliases, templates, keys, objects) and
// returning the request's reply (top-level keys + surfaced names).
func (s *Session) Execute(script *Script, f Factory) (*Reply, error) {
	st := &execState{
		reply: &Reply{IDs: make(map[string]uint64)},
	}

	for _, stmt := range script.Statements {
		if err := s.executeTopLevel(stmt, f, st); err != nil {
			return nil, err
		}
	}
	return st.reply, nil
}

func (s *Session) executeTopLevel(stmt *Statement, f Factory, st *execState) error {
	// Surfacing reference: key=path (D15). Also registers the surfaced
	// name as a session key, so later verbs can use the short name
	// (`wcb=root.cb` then `sub wcb toggle`).
	if stmt.Verb == "" {
		id, ok := s.keys[stmt.Ref]
		if !ok {
			return fmt.Errorf("surfacing %s=%s: unknown key path %q", stmt.Key, stmt.Ref, stmt.Ref)
		}
		st.reply.IDs[stmt.Key] = id
		s.keys[stmt.Key] = id
		return nil
	}

	switch stmt.Verb {
	case "alias":
		if stmt.Key != "" {
			return fmt.Errorf("alias: correlation keys do not apply")
		}
		return s.declareAliases(stmt.Args)
	case "template":
		if stmt.Key != "" {
			return fmt.Errorf("template: correlation keys do not apply")
		}
		return s.declareTemplate(stmt.Args)
	case "new":
		var obj Object
		err := s.suppressed(f, func() error {
			var e error
			obj, e = s.instantiate(stmt.Args, f, st, stmt.Key)
			return e
		})
		if err != nil {
			return err
		}
		if stmt.Key != "" {
			s.keys[stmt.Key] = obj.ID()
			st.reply.IDs[stmt.Key] = obj.ID()
		}
		return nil
	case "set":
		obj, keyPath, rest, err := s.resolveTarget("set", stmt.Args)
		if err != nil {
			return err
		}
		return s.suppressed(f, func() error {
			return s.applyArgs(obj, rest, f, st, keyPath)
		})
	case "destroy":
		obj, _, rest, err := s.resolveTarget("destroy", stmt.Args)
		if err != nil {
			return err
		}
		if len(rest) != 0 {
			return fmt.Errorf("destroy: takes only a target")
		}
		d, ok := obj.(destroyer)
		if !ok {
			return fmt.Errorf("destroy: object does not support destroy")
		}
		if err := d.Destroy(); err != nil {
			return err
		}
		s.forget(obj.ID())
		return nil
	case "sub", "unsub":
		return s.subscribe(stmt.Verb, stmt.Args, f)
	case "describe":
		// Introspection (D24): stream the registered wire vocabulary as
		// flat statements (proptype/prop/propcommon), one per line, ahead
		// of the reply. Takes no arguments.
		if len(stmt.Args) != 0 {
			return fmt.Errorf("describe: takes no arguments")
		}
		enc := EncodeVocabulary(DescribeVocabulary())
		for _, line := range strings.Split(strings.TrimRight(enc, "\n"), "\n") {
			if line != "" {
				st.reply.Extra = append(st.reply.Extra, line)
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown verb %q", stmt.Verb)
	}
}

// suppressed runs fn with the connection's event emission suppressed
// when the factory supports it (D20: wire-initiated mutations never
// echo). Factories without EventControl run fn directly.
func (s *Session) suppressed(f Factory, fn func() error) error {
	ec, ok := f.(EventControl)
	if !ok {
		return fn()
	}
	var err error
	ec.Suppressed(func() { err = fn() })
	return err
}

// resolveTarget interprets a verb's leading argument as an object
// reference (D19): a key path (`set root.status ...`) or a bare
// numeric ID (`set 1042 ...`). Returns the object, the key path when
// referenced by key ("" for numeric), and the remaining args.
func (s *Session) resolveTarget(verb string, args []*Arg) (Object, string, []*Arg, error) {
	if len(args) == 0 {
		return nil, "", nil, fmt.Errorf("%s: expected a target (key path or object id)", verb)
	}
	head := args[0]

	var id uint64
	keyPath := ""
	switch {
	case head.Name == "" && head.Value != nil && head.Value.Kind == NumberValue && head.Value.IsInt:
		id = uint64(head.Value.Number)
	case head.Name != "" && head.Value == nil && head.Flag == FlagTrue:
		known, ok := s.keys[head.Name]
		if !ok {
			return nil, "", nil, fmt.Errorf("%s: unknown key path %q", verb, head.Name)
		}
		id = known
		keyPath = head.Name
	default:
		return nil, "", nil, fmt.Errorf("%s: target must be a key path or object id", verb)
	}

	obj, ok := s.objects[id]
	if !ok {
		return nil, "", nil, fmt.Errorf("%s: no object with id %d in this session", verb, id)
	}
	return obj, keyPath, args[1:], nil
}

// forget drops an object and every key that referenced it.
func (s *Session) forget(id uint64) {
	delete(s.objects, id)
	for k, v := range s.keys {
		if v == id {
			delete(s.keys, k)
		}
	}
}

// subscribe handles sub/unsub (D20): `sub <target>|all [events...]`.
// Event names are bare flags; none means all events of the target.
// `command` events flow unconditionally and need no subscription.
func (s *Session) subscribe(verb string, args []*Arg, f Factory) error {
	ec, ok := f.(EventControl)
	if !ok {
		return fmt.Errorf("%s: this connection does not support event subscriptions", verb)
	}
	if len(args) == 0 {
		return fmt.Errorf("%s: expected a target (key path, object id, or all)", verb)
	}

	var id uint64 // 0 = all trinkets
	head := args[0]
	switch {
	case head.Name == "all" && head.Value == nil && head.Flag == FlagTrue:
		id = 0
	default:
		obj, _, _, err := s.resolveTarget(verb, args[:1])
		if err != nil {
			return err
		}
		id = obj.ID()
	}

	events := args[1:]
	if len(events) == 0 {
		if verb == "sub" {
			ec.Subscribe(id, "")
		} else {
			ec.Unsubscribe(id, "")
		}
		return nil
	}
	for _, ev := range events {
		if ev.Value != nil || ev.Flag != FlagTrue || ev.Name == "" {
			return fmt.Errorf("%s: event names are bare words", verb)
		}
		if verb == "sub" {
			ec.Subscribe(id, ev.Name)
		} else {
			ec.Unsubscribe(id, ev.Name)
		}
	}
	return nil
}

// declareAliases handles `alias C="caption" ...`: targets are strings
// (lexical macros - D17 addendum), names must begin uppercase (D18).
func (s *Session) declareAliases(args []*Arg) error {
	if len(args) == 0 {
		return fmt.Errorf("alias: nothing to declare")
	}
	for _, a := range args {
		if a.Value == nil {
			return fmt.Errorf("alias %s: expected %s=\"target\"", a.Name, a.Name)
		}
		if a.Value.Kind != StringValue {
			return fmt.Errorf("alias %s: target must be a string (aliases are lexical macros)", a.Name)
		}
		if !isUpperInitial(a.Name) {
			return fmt.Errorf("alias %s: user-defined aliases must begin with an uppercase letter (D18)", a.Name)
		}
		s.aliases[a.Name] = a.Value.Str
	}
	return nil
}

// declareTemplate handles `template Name=base props...` (D14/D18).
func (s *Session) declareTemplate(args []*Arg) error {
	if len(args) == 0 || args[0].Value == nil || args[0].Value.Kind != WordValue {
		return fmt.Errorf("template: expected Name=type")
	}
	name := args[0].Name
	base := args[0].Value.Word
	if !isUpperInitial(name) {
		return fmt.Errorf("template %s: user-defined templates must begin with an uppercase letter (D18)", name)
	}
	if isUpperInitial(base) {
		if _, ok := s.templates[base]; !ok {
			return fmt.Errorf("template %s: unknown base template %q", name, base)
		}
	} else if !isLowerInitial(base) {
		return fmt.Errorf("template %s: invalid base type %q", name, base)
	}
	s.templates[name] = &templateDef{base: base, args: args[1:]}
	return nil
}

// resolveType expands a type name through the template chain (D14:
// expansion at instantiation; transitive with cycle guard), returning
// the final builtin type and the accumulated template args, base-most
// first.
func (s *Session) resolveType(typeName string) (string, []*Arg, error) {
	var chain []*templateDef
	visited := map[string]bool{}

	name := typeName
	for isUpperInitial(name) {
		if visited[name] {
			return "", nil, fmt.Errorf("template %s: cyclic template chain", typeName)
		}
		visited[name] = true
		def, ok := s.templates[name]
		if !ok {
			return "", nil, fmt.Errorf("unknown template %q", name)
		}
		chain = append(chain, def)
		name = def.base
	}
	if !isLowerInitial(name) {
		return "", nil, fmt.Errorf("invalid type %q", name)
	}

	// Accumulate args base-most first so later (more specific,
	// then instance) properties override earlier ones.
	var merged []*Arg
	for i := len(chain) - 1; i >= 0; i-- {
		merged = append(merged, chain[i].args...)
	}
	return name, merged, nil
}

// instantiate executes a `new` statement's args: the leading bare word
// is the type (builtin or template); remaining args are properties,
// flags, and children blocks. keyPath is the hierarchical key prefix
// for registering nested keys ("" when the statement is unkeyed).
func (s *Session) instantiate(args []*Arg, f Factory, st *execState, keyPath string) (Object, error) {
	if len(args) == 0 || args[0].Value != nil || args[0].Flag != FlagTrue {
		return nil, fmt.Errorf("new: expected a type name")
	}
	typeName := args[0].Name

	builtin, templateArgs, err := s.resolveType(typeName)
	if err != nil {
		return nil, err
	}

	obj, err := f.New(builtin)
	if err != nil {
		return nil, err
	}
	s.objects[obj.ID()] = obj

	// Template properties first, instance properties after: later Set
	// calls override earlier ones (scalars), and children concatenate
	// in application order (template children, then instance children).
	if err := s.applyArgs(obj, templateArgs, f, st, keyPath); err != nil {
		return nil, err
	}
	if err := s.applyArgs(obj, args[1:], f, st, keyPath); err != nil {
		return nil, err
	}
	return obj, nil
}

// applyArgs applies properties, flags, and children blocks to obj.
func (s *Session) applyArgs(obj Object, args []*Arg, f Factory, st *execState, keyPath string) error {
	for _, a := range args {
		name := a.Name
		if name == "" {
			// Anonymous numbers exist only for verb targets (D19).
			return fmt.Errorf("unnamed value: properties must be named (name=value)")
		}
		// Alias substitution (lexical, property-name position, D10/D18):
		// uppercase-initial names must be declared aliases.
		if isUpperInitial(name) {
			target, ok := s.aliases[name]
			if !ok {
				return fmt.Errorf("unknown alias %q (property names are lowercase; aliases must be declared)", name)
			}
			name = target
		}

		if name == "children" {
			if a.Value == nil || a.Value.Kind != BlockValue {
				return fmt.Errorf("children: expected a {} block")
			}
			if err := s.buildChildren(obj, a.Value.Block, f, st, keyPath); err != nil {
				return err
			}
			continue
		}

		if a.Value == nil {
			if err := obj.Set(name, nil, a.Flag); err != nil {
				return err
			}
			continue
		}
		if err := obj.Set(name, a.Value, FlagNone); err != nil {
			return err
		}
	}
	return nil
}

// buildChildren executes a children block (D13): each statement must
// be a (possibly keyed) `new`. Keys register hierarchically under the
// enclosing key path (D15); with no enclosing key they remain
// internal-only.
func (s *Session) buildChildren(parent Object, block *Script, f Factory, st *execState, keyPath string) error {
	for _, stmt := range block.Statements {
		if stmt.Verb != "new" {
			if stmt.Verb == "" {
				return fmt.Errorf("children: surfacing references are top-level statements")
			}
			return fmt.Errorf("children: only new statements allowed, found %q", stmt.Verb)
		}

		childPath := ""
		if stmt.Key != "" && keyPath != "" {
			childPath = keyPath + "." + stmt.Key
		}

		child, err := s.instantiate(stmt.Args, f, st, childPath)
		if err != nil {
			return err
		}
		if childPath != "" {
			s.keys[childPath] = child.ID()
		}
		if err := parent.Append(child); err != nil {
			return err
		}
	}
	return nil
}

// Aliases returns a copy of the session's alias table (for debugging
// and tests).
func (s *Session) Aliases() map[string]string {
	out := make(map[string]string, len(s.aliases))
	for k, v := range s.aliases {
		out[k] = v
	}
	return out
}

// HasTemplate reports whether a template is declared.
func (s *Session) HasTemplate(name string) bool {
	_, ok := s.templates[name]
	return ok
}
