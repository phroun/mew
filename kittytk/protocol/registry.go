package protocol

import (
	"fmt"
	"sort"
	"sync"
	"sync/atomic"
)

// This file is the type/property registry behind the wire vocabulary.
// Per the owner's architecture: each trinket's own codebase registers
// its type and property mappings (trinkets import protocol; protocol
// imports nothing of KittyTK). The registry is write-at-init,
// read-at-execute.

// BindContext carries per-connection services into property appliers
// and event wiring. It is instance-scoped, never global (multi-display
// guardrail): one app may hold several connections, each with its own
// context.
type BindContext struct {
	// Dispatch delivers an activated command ID (action= wiring).
	// Nil when the connection has no command sink; appliers that
	// need it must error clearly.
	Dispatch func(commandID string)

	// Emit delivers event records to the app side. Nil when the
	// connection does not consume events. Trinkets' Bind wiring calls
	// EmitEvent, which is nil-safe.
	Emit func(*Event)

	mu       sync.Mutex
	actions  map[uint64]string
	subs     map[uint64]map[string]bool     // trinketID -> event types ("" = all; ID 0 = all trinkets)
	onSub    map[uint64]map[string][]func() // trinketID -> event type -> on-subscribe hooks
	suppress int
	stash    map[string]any
	refs     map[uint64]any // virtual wire objects by ID, for pointer properties
}

// RegisterRef records a virtual wire object under its wire ID so
// POINTER PROPERTIES on this connection can reach it later (a tree
// column's enum= names a collection built earlier). The factory calls
// this for every virtual object it constructs.
func (c *BindContext) RegisterRef(id uint64, target any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.refs == nil {
		c.refs = make(map[uint64]any)
	}
	c.refs[id] = target
}

// LookupRef resolves a wire ID registered with RegisterRef (nil if
// unknown on this connection).
func (c *BindContext) LookupRef(id uint64) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.refs[id]
}

// Stash returns the connection-scoped value under key, creating it
// with create on first use. Trinket registrations use this for shared
// per-connection state (e.g. named radio groups) without the protocol
// package knowing the types involved.
func (c *BindContext) Stash(key string, create func() any) any {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stash == nil {
		c.stash = make(map[string]any)
	}
	v, ok := c.stash[key]
	if !ok {
		v = create()
		c.stash[key] = v
	}
	return v
}

// EmitEvent delivers an event if the connection consumes events AND
// the event passes the subscription filter (D20): `command` events
// always flow; state events flow only where a subscription exists.
// Nothing flows while emissions are suppressed (wire-initiated
// mutations never echo).
func (c *BindContext) EmitEvent(ev *Event) {
	if c.Emit == nil || ev == nil {
		return
	}
	c.mu.Lock()
	suppressed := c.suppress > 0
	pass := ev.Type == "command" || c.subscribedLocked(ev)
	c.mu.Unlock()
	if suppressed || !pass {
		return
	}
	c.Emit(ev)
}

// subscribedLocked checks the subscription table. Caller holds c.mu.
func (c *BindContext) subscribedLocked(ev *Event) bool {
	if c.subs == nil {
		return false
	}
	id, _ := ev.Trinket() // 0 when the event names no trinket
	for _, wid := range [2]uint64{id, 0} {
		if types, ok := c.subs[wid]; ok {
			if types[""] || types[ev.Type] {
				return true
			}
		}
		if id == 0 {
			break
		}
	}
	return false
}

// OnSubscribe registers fn to run each time a client subscribes to
// (trinketID, eventType). It lets a trinket push its current state to a
// late-subscribing client - e.g. a terminal re-emitting its grid size, so a
// client whose subscription arrives after the one-shot paint-time emit still
// learns the real size (its PTY would otherwise stay at the default and the
// shell would mis-wrap its prompt). fn runs after the subscription is
// recorded and without c.mu held, so it may call EmitEvent.
func (c *BindContext) OnSubscribe(trinketID uint64, eventType string, fn func()) {
	c.mu.Lock()
	if c.onSub == nil {
		c.onSub = make(map[uint64]map[string][]func())
	}
	if c.onSub[trinketID] == nil {
		c.onSub[trinketID] = make(map[string][]func())
	}
	c.onSub[trinketID][eventType] = append(c.onSub[trinketID][eventType], fn)
	c.mu.Unlock()
}

// Subscribe opens event flow for (trinketID, eventType). trinketID 0
// means all trinkets; eventType "" means all types.
func (c *BindContext) Subscribe(trinketID uint64, eventType string) {
	c.mu.Lock()
	if c.subs == nil {
		c.subs = make(map[uint64]map[string]bool)
	}
	if c.subs[trinketID] == nil {
		c.subs[trinketID] = make(map[string]bool)
	}
	c.subs[trinketID][eventType] = true
	// Collect on-subscribe hooks for this exact type (and, when a specific
	// type is named, the type-agnostic "" registrations too).
	var fire []func()
	if c.onSub != nil {
		if m, ok := c.onSub[trinketID]; ok {
			fire = append(fire, m[eventType]...)
			if eventType != "" {
				fire = append(fire, m[""]...)
			}
		}
	}
	c.mu.Unlock()
	for _, fn := range fire {
		fn()
	}
}

// Unsubscribe removes subscriptions. eventType "" removes all of the
// trinket's subscriptions; trinketID 0 with eventType "" clears the
// whole table.
func (c *BindContext) Unsubscribe(trinketID uint64, eventType string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.subs == nil {
		return
	}
	if trinketID == 0 && eventType == "" {
		c.subs = nil
		return
	}
	if eventType == "" {
		delete(c.subs, trinketID)
		return
	}
	if types, ok := c.subs[trinketID]; ok {
		delete(types, eventType)
		if len(types) == 0 {
			delete(c.subs, trinketID)
		}
	}
}

// Suppressed runs f with all emission AND action dispatch disabled.
// The session wraps wire-initiated property application (new, set) in
// this: mutations arriving over the wire neither echo back as events
// nor fire the app's command handlers (D20).
func (c *BindContext) Suppressed(f func()) {
	c.mu.Lock()
	c.suppress++
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.suppress--
		c.mu.Unlock()
	}()
	f()
}

// SetAction records the command bound to a trinket (action=). The
// trinket's activation wiring (set once at Bind time) consults this,
// so action can be assigned or replaced without re-wiring callbacks.
func (c *BindContext) SetAction(trinketID uint64, commandID string) {
	c.mu.Lock()
	if c.actions == nil {
		c.actions = make(map[uint64]string)
	}
	c.actions[trinketID] = commandID
	c.mu.Unlock()
}

// Action returns the command bound to a trinket, or "".
func (c *BindContext) Action(trinketID uint64) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.actions[trinketID]
}

// FireAction dispatches a trinket's bound command (if any) and emits
// the corresponding command event. Called from trinket activation
// wiring. Inert while emissions are suppressed: a wire-initiated
// state change (set checked, construction defaults) must not fire
// app command handlers (D20).
func (c *BindContext) FireAction(trinketID uint64) {
	c.mu.Lock()
	suppressed := c.suppress > 0
	c.mu.Unlock()
	if suppressed {
		return
	}
	id := c.Action(trinketID)
	if id == "" {
		return
	}
	if c.Dispatch != nil {
		c.Dispatch(id)
	}
	c.EmitEvent(NewEvent("command").WithWord("action", id))
}

// PropertyApplier applies one wire property to a target object,
// performing D17-typed conversion (see AsString and friends).
type PropertyApplier func(ctx *BindContext, target any, v *Value, flag FlagState) error

// TypeSpec describes a registered builtin type.
type TypeSpec struct {
	// New constructs the target (a trinket, or a virtual item's record).
	New func() any

	// Props maps property names to their Property (applier + descriptor).
	// Type-specific properties take precedence over common ones.
	Props map[string]Property

	// Append attaches a constructed child target to a parent target
	// (children blocks, D13). Nil means the type takes no children.
	Append func(parent, child any) error

	// Bind wires the target's event emission into the connection
	// (called once, immediately after New). Trinket codebases own this
	// wiring, same as their property registration. Optional.
	Bind func(ctx *BindContext, target any)

	// ID returns the target's stable object identity. Nil for Virtual
	// types, which get factory-assigned virtual IDs.
	ID func(target any) uint64

	// Destroy tears the target down (D19's destroy verb): detach from
	// its parent, release resources. Nil means the type cannot be
	// destroyed over the wire.
	Destroy func(target any) error

	// Virtual marks pseudo-object types (e.g. combobox items): they
	// skip common properties and trinket identity.
	Virtual bool
}

var (
	regMu     sync.RWMutex
	regTypes  = map[string]*TypeSpec{}
	regCommon = map[string]Property{}
)

// RegisterType registers a builtin type. Builtin names begin lowercase
// (D18). Panics on programmer error (duplicate, bad spec) - callers
// are init functions.
func RegisterType(name string, spec *TypeSpec) {
	if !isLowerInitial(name) {
		panic(fmt.Sprintf("protocol: builtin type %q must begin lowercase (D18)", name))
	}
	if spec == nil || spec.New == nil {
		panic(fmt.Sprintf("protocol: type %q: spec.New is required", name))
	}
	if !spec.Virtual && spec.ID == nil {
		panic(fmt.Sprintf("protocol: type %q: spec.ID is required for non-virtual types", name))
	}
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := regTypes[name]; dup {
		panic(fmt.Sprintf("protocol: type %q registered twice", name))
	}
	regTypes[name] = spec
}

// RegisterCommonProperty registers a property available on every
// non-virtual type (enabled, visible, font, ...). The Property carries
// both the applier and its introspection descriptor.
func RegisterCommonProperty(name string, p Property) {
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := regCommon[name]; dup {
		panic(fmt.Sprintf("protocol: common property %q registered twice", name))
	}
	regCommon[name] = p
}

// RegisteredTypes returns the sorted names of registered types.
func RegisteredTypes() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	names := make([]string, 0, len(regTypes))
	for n := range regTypes {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

var virtualIDCounter atomic.Uint64

// virtualIDSource allocates IDs for Virtual objects. The default is a
// package-private counter, which is fine only when virtual IDs never
// meet real object IDs. Hosts whose real IDs share the uint64 space
// (KittyTK's core.ObjectID does) must install their own allocator so the
// two can never collide — see SetVirtualIDSource.
var virtualIDSource = func() uint64 { return virtualIDCounter.Add(1) }

// SetVirtualIDSource installs the allocator used for Virtual objects'
// IDs. Call once at init, before any factory runs; the trinket package
// points this at the same counter that issues real object IDs.
func SetVirtualIDSource(fn func() uint64) {
	if fn != nil {
		virtualIDSource = fn
	}
}

// RegistryFactory implements Factory over the registered types, bound
// to one connection's BindContext.
type RegistryFactory struct {
	ctx *BindContext
}

// NewRegistryFactory creates a factory for one connection.
func NewRegistryFactory(ctx *BindContext) *RegistryFactory {
	if ctx == nil {
		ctx = &BindContext{}
	}
	return &RegistryFactory{ctx: ctx}
}

// New implements Factory.
func (f *RegistryFactory) New(typeName string) (Object, error) {
	regMu.RLock()
	spec := regTypes[typeName]
	regMu.RUnlock()
	if spec == nil {
		return nil, fmt.Errorf("unknown trinket type %q", typeName)
	}
	o := &registryObject{ctx: f.ctx, spec: spec, target: spec.New()}
	if spec.Virtual {
		o.virtualID = virtualIDSource()
		// Virtual targets that want to know their identity (e.g. tree
		// items, whose IDs outlive construction) receive it here.
		if aware, ok := o.target.(interface{ SetWireID(uint64) }); ok {
			aware.SetWireID(o.virtualID)
		}
		// And every virtual object is reachable by ID for pointer
		// properties (a column's enum= naming a collection).
		f.ctx.RegisterRef(o.virtualID, o.target)
	}
	if spec.Bind != nil {
		spec.Bind(f.ctx, o.target)
	}
	return o, nil
}

type registryObject struct {
	ctx       *BindContext
	spec      *TypeSpec
	target    any
	virtualID uint64
}

// Target exposes the constructed object (the trinket) so the embedding
// application can, e.g., set a built tree as window content.
func (o *registryObject) Target() any { return o.target }

// Set implements Object.
func (o *registryObject) Set(name string, v *Value, flag FlagState) error {
	if p, ok := o.spec.Props[name]; ok {
		return p.Apply(o.ctx, o.target, v, flag)
	}
	if !o.spec.Virtual {
		regMu.RLock()
		p, ok := regCommon[name]
		regMu.RUnlock()
		if ok {
			return p.Apply(o.ctx, o.target, v, flag)
		}
	}
	return fmt.Errorf("property %q is not supported by this type", name)
}

// Append implements Object.
func (o *registryObject) Append(child Object) error {
	if o.spec.Append == nil {
		return fmt.Errorf("this type does not accept children")
	}
	c, ok := child.(*registryObject)
	if !ok {
		return fmt.Errorf("cannot append foreign object")
	}
	return o.spec.Append(o.target, c.target)
}

// ID implements Object.
func (o *registryObject) ID() uint64 {
	if o.spec.Virtual || o.spec.ID == nil {
		return o.virtualID
	}
	return o.spec.ID(o.target)
}

// Destroy implements the session's optional destroyer interface.
func (o *registryObject) Destroy() error {
	if o.spec.Destroy == nil {
		return fmt.Errorf("this type does not support destroy")
	}
	return o.spec.Destroy(o.target)
}

// EventControl is implemented by RegistryFactory so the session's
// sub/unsub verbs and echo suppression reach the connection's
// BindContext without the session knowing about trinkets.
func (f *RegistryFactory) Subscribe(trinketID uint64, eventType string) {
	f.ctx.Subscribe(trinketID, eventType)
}
func (f *RegistryFactory) Unsubscribe(trinketID uint64, eventType string) {
	f.ctx.Unsubscribe(trinketID, eventType)
}
func (f *RegistryFactory) Suppressed(fn func()) { f.ctx.Suppressed(fn) }

// --- D17 typed-conversion helpers for property appliers ---

// AsString requires a quoted string value.
func AsString(name string, v *Value, flag FlagState) (string, error) {
	if flag != FlagNone || v == nil || v.Kind != StringValue {
		return "", fmt.Errorf("%s: expected a quoted string", name)
	}
	return v.Str, nil
}

// AsWord requires a bare word (enum or identifier).
func AsWord(name string, v *Value, flag FlagState) (string, error) {
	if flag != FlagNone || v == nil || v.Kind != WordValue {
		return "", fmt.Errorf("%s: expected a bare word", name)
	}
	return v.Word, nil
}

// AsInt requires an integer-valued numeric.
func AsInt(name string, v *Value, flag FlagState) (int, error) {
	if flag != FlagNone || v == nil || v.Kind != NumberValue || !v.IsInt {
		return 0, fmt.Errorf("%s: expected an integer", name)
	}
	return int(v.Number), nil
}

// AsFloat requires a numeric (int or float).
func AsFloat(name string, v *Value, flag FlagState) (float64, error) {
	if flag != FlagNone || v == nil || v.Kind != NumberValue {
		return 0, fmt.Errorf("%s: expected a number", name)
	}
	return v.Number, nil
}

// AsBool accepts flag form (canonical) or the true/false long form
// (D12); asserted-indeterminate is rejected - properties for which
// indeterminate is meaningful read the FlagState directly (D16).
func AsBool(name string, v *Value, flag FlagState) (bool, error) {
	switch flag {
	case FlagTrue:
		return true, nil
	case FlagFalse:
		return false, nil
	case FlagIndeterminate:
		return false, fmt.Errorf("%s: indeterminate is not meaningful for this property", name)
	}
	if v != nil && v.Kind == WordValue {
		switch v.Word {
		case "true":
			return true, nil
		case "false":
			return false, nil
		}
	}
	return false, fmt.Errorf("%s: expected a flag", name)
}
