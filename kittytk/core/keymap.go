package core

import (
	"sort"
	"sync"

	"github.com/phroun/key-sequence-processor/keyseq"
)

// A KeyRegistry is a key-to-command table: a key or key sequence as the input
// layer reports it, mapped to the name of the command it runs. It is the whole
// of what a keymap is — there is one active at a time, "default" unless a
// scope overrides it, and the toolkit's own bindings are entries in it like
// anyone else's.
//
// A registry is not a dispatch mechanism. It answers what a key MEANS; who
// acts on that meaning is the existing chain's business, unchanged.
type KeyRegistry struct {
	mu   sync.RWMutex
	name string
	// bindings maps a key to every command it can mean. Several, because one
	// key means different things in different situations and the registry is
	// not the place that decides which: "Up" nudges a window while its title
	// bar is focused and steps a list otherwise, and both are true at once.
	// A context keeps whichever of them the situation actually offers, so at
	// most one survives anywhere it is asked.
	bindings map[string][]boundCommand
	revision uint64
	// serial counts bindings as they are ADDED, so every (key, command) pair
	// carries where it came in the order. That is the answer to "several keys
	// mean this, which one do I SHOW?": the newest, because the newest is the
	// one that was configured last -- the defaults first, then the ini file
	// over them, then whatever the host declared itself.
	//
	// It is registration order, deliberately not context-build order: a
	// context is built and rebuilt constantly as focus moves, and which key a
	// menu advertises must not depend on when someone last clicked something.
	serial uint64
}

// boundCommand is one meaning of one key: what it does, when it was bound,
// and what the environment hints on its line had to say about it.
type boundCommand struct {
	command string
	serial  uint64
	// weight is the environment preference (see keyhints.go): positive where
	// the hints matched this environment, negative where they named another
	// one, zero where the line said nothing about where it belongs. It
	// outranks the serial, so a keymap's own (mac) line beats a later unhinted
	// one on a Mac -- and loses to it everywhere else.
	weight int
}

// outranks reports whether a should be advertised over b: the environment's
// own spelling first, and among equals the one bound last.
func (a boundCommand) outranks(b boundCommand) bool {
	if a.weight != b.weight {
		return a.weight > b.weight
	}
	return a.serial > b.serial
}

// A Binding is one LINE of a keymap: a key, and every command it can mean. A
// keymap is a list of them rather than a map, because the order they are
// written in is part of what they say -- among several keys that mean the same
// thing, the one written LAST is the one advertised for it (see KeyForCommand).
// A map has no order and would leave that to Go's iteration.
type Binding struct {
	Key      string
	Commands []string
}

// NewKeyRegistry creates a registry from a key-to-command table. The name is
// for diagnostics — "default", "purfecterm-captured" — and has no behaviour.
func NewKeyRegistry(name string, bindings []Binding) *KeyRegistry {
	r := &KeyRegistry{name: name, bindings: make(map[string][]boundCommand, len(bindings))}
	// Serials follow the ORDER THE BINDINGS ARE WRITTEN IN, which is why a
	// keymap is a list: the table reads top to bottom, and where several keys
	// mean one command the last of them is the one advertised for it. A key
	// may appear more than once (each line adds a meaning), and anything bound
	// later still -- an ini file, a host -- outranks all of it.
	env := CurrentKeymapEnvironment()
	for _, b := range bindings {
		key, weight, keep := keyHints(b.Key, env)
		if !keep {
			continue // required an environment this is not: not bound at all
		}
		for _, cmd := range b.Commands {
			if cmd == "" {
				continue
			}
			r.serial++
			r.bindings[key] = append(r.bindings[key], boundCommand{command: cmd, serial: r.serial, weight: weight})
		}
	}
	return r
}

// NewKeyRegistryFromMap builds a registry from an unordered table, for a
// caller that has one -- a parsed [mappings] section, say. A map has no order
// and serials are an order, so this imposes one (keys ascending) rather than
// leaving what a menu advertises to Go's map iteration. Prefer() is how such a
// caller says which spelling to advertise; a keymap written in source should
// use NewKeyRegistry and say it by the order it writes.
func NewKeyRegistryFromMap(name string, bindings map[string][]string) *KeyRegistry {
	keys := make([]string, 0, len(bindings))
	for k := range bindings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	ordered := make([]Binding, 0, len(keys))
	for _, k := range keys {
		ordered = append(ordered, Binding{Key: k, Commands: bindings[k]})
	}
	return NewKeyRegistry(name, ordered)
}

// Name returns the registry's name.
func (r *KeyRegistry) Name() string {
	if r == nil {
		return ""
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.name
}

// Revision is bumped by every edit. A context built from this registry records
// the revision it was built at, so staleness is a comparison rather than a
// subscription — nothing has to remember to notify anybody.
func (r *KeyRegistry) Revision() uint64 {
	if r == nil {
		return 0
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.revision
}

// Bind REPLACES everything a key means with one command. An empty command
// unbinds it entirely, which is how a user turns a default off without having
// to know what it was.
func (r *KeyRegistry) Bind(key, command string) {
	key, weight, keep := keyHints(key, CurrentKeymapEnvironment())
	r.mu.Lock()
	defer r.mu.Unlock()
	if command == "" || !keep {
		// A binding this environment is not for is a binding it does not have.
		// Unbinding it as well is deliberate: "(only_mac) ^W = something" says
		// ^W is the Mac's, so off a Mac ^W is left to whatever else claims it.
		delete(r.bindings, key)
	} else {
		r.serial++
		r.bindings[key] = []boundCommand{{command: command, serial: r.serial, weight: weight}}
	}
	r.revision++
}

// AddBinding gives a key one more meaning, for the situations that do not
// overlap with the ones it already has.
func (r *KeyRegistry) AddBinding(key, command string) {
	if command == "" {
		return
	}
	key, weight, keep := keyHints(key, CurrentKeymapEnvironment())
	if !keep {
		return // required an environment this is not
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, b := range r.bindings[key] {
		if b.command == command {
			return // already means this; it keeps the serial it came in with
		}
	}
	r.serial++
	r.bindings[key] = append(r.bindings[key], boundCommand{command: command, serial: r.serial, weight: weight})
	r.revision++
}

// Prefer binds a key to a command AND makes it the newest binding of that
// command, which is to say the one shown wherever a command has to name a key.
// It is the difference between "this key also does that" (AddBinding, which
// leaves an existing pair's place in the order alone) and "this is the
// spelling to advertise" -- a macOS host declaring the Command-key spelling of
// Cut over the Control one, say, without unbinding either.
func (r *KeyRegistry) Prefer(key, command string) {
	if command == "" {
		return
	}
	key, weight, keep := keyHints(key, CurrentKeymapEnvironment())
	if !keep {
		return // required an environment this is not
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.serial++
	for i, b := range r.bindings[key] {
		if b.command == command {
			r.bindings[key][i].serial = r.serial
			if weight > r.bindings[key][i].weight {
				r.bindings[key][i].weight = weight
			}
			r.revision++
			return
		}
	}
	r.bindings[key] = append(r.bindings[key], boundCommand{command: command, serial: r.serial, weight: weight})
	r.revision++
}

// KeysFor returns every key bound to a command, NEWEST FIRST — the most
// recently bound spelling leads, so a caller that wants one key can take the
// first and a caller that wants them all still gets them all. A command may
// have several keys, or none: the coarse window move is bound to Ctrl, Meta
// and Super arrows alike, and a command nothing names is simply not reachable
// from the keyboard.
func (r *KeyRegistry) KeysFor(command string) []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var found []boundCommand
	for k, bs := range r.bindings {
		for _, b := range bs {
			if b.command == command {
				// The key travels as the command field; the rank is what is
				// wanted from the binding itself.
				found = append(found, boundCommand{command: k, serial: b.serial, weight: b.weight})
				break
			}
		}
	}
	sort.Slice(found, func(i, j int) bool { return found[i].outranks(found[j]) })
	keys := make([]string, 0, len(found))
	for _, f := range found {
		keys = append(keys, f.command)
	}
	return keys
}

// Binds reports whether this keymap gives a key any meaning at all, whatever
// the situation. It is the question the menu bar asks before taking a chord
// for an accelerator: a key the keymap has spoken for belongs to the keymap,
// and the bar takes its next candidate letter instead.
//
// Deliberately not "does the current situation offer a command on it". That
// answer moves as the focus moves, and an underline that wanders around the
// menu bar as you click about is worse than an accelerator on the second-best
// letter. The keymap is the same wherever you are, so the assignment is too.
func (r *KeyRegistry) Binds(key string) bool {
	if r == nil || key == "" {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.bindings[key]) > 0
}

// KeyForCommand returns the ONE key to show for a command: the newest binding
// of it, or "" when nothing is bound. It is KeysFor's first entry, named for
// what it is used for — a menu item advertising the key that runs it.
//
// A menu should prefer the KeyContext's answer, which is this narrowed to what
// the situation actually offers; this one is for the callers with no context
// in hand.
func (r *KeyRegistry) KeyForCommand(command string) string {
	if keys := r.KeysFor(command); len(keys) > 0 {
		return keys[0]
	}
	return ""
}

// A KeyContext is the set of actions available right now, ready to resolve a
// key against. There is one at a time per scope: not a slice per widget, but
// everything the current situation offers.
//
// It is stateful, because a sequence is: feeding it the first key of a chord
// holds that prefix until the chord completes or is abandoned.
type KeyContext struct {
	mu       sync.Mutex
	proc     *keyseq.Processor
	commands map[string]bool
	revision uint64
	// ranks is how the bindings this context kept were ordered in the registry
	// -- environment preference and registration order both -- carried over so
	// the context can answer which key to SHOW for a command without going
	// back to it. Keys added here later (the formed accelerators) continue
	// above everything the registry had, since they are the newest thing in
	// the room.
	ranks map[string]boundCommand
	added uint64
	// matched is the whole sequence the last successful Resolve consumed, not
	// just its final key. A command that carries no identity of its own -- the
	// menu accelerators all resolve to one command -- needs the key to say
	// which one fired, and for a chord that means the chord, not the last
	// keystroke of it.
	matched string
}

// BuildContext derives a context from the registry, carrying only the bindings
// whose command is in commands. Everything else in the registry is invisible
// here — which is the point: a key bound to something this situation cannot do
// should not resolve to it.
//
// The processor runs in resolution-only mode, so nothing is dispatched.
// Resolve reports the command and the caller acts, exactly as the switch it
// replaces did.
func (r *KeyRegistry) BuildContext(commands []string) *KeyContext {
	set := make(map[string]bool, len(commands))
	for _, c := range commands {
		set[c] = true
	}
	ctx := &KeyContext{
		proc:     keyseq.NewProcessor(nil),
		commands: set,
		revision: r.Revision(),
		ranks:    map[string]boundCommand{},
	}
	mappings := map[string]string{}
	if r != nil {
		r.mu.RLock()
		for k, bs := range r.bindings {
			// At most one of a key's meanings is on offer here; the rest
			// belong to situations this is not. Where a trinket genuinely
			// answers to several forms, its own case accepts them all and it
			// decides from its state.
			for _, b := range bs {
				if set[b.command] {
					mappings[k] = b.command
					ctx.ranks[k] = b
					if b.serial > ctx.added {
						ctx.added = b.serial
					}
					break
				}
			}
		}
		r.mu.RUnlock()
	}
	ctx.proc.SetMappings(mappings)
	return ctx
}

// Revision reports the registry revision this context was built at.
func (c *KeyContext) Revision() uint64 {
	if c == nil {
		return 0
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.revision
}

// Add binds a key to a command inside an already-built context, for entries
// that are formed rather than configured — the menu accelerators, whose keys
// depend on the menu titles rather than on the registry.
func (c *KeyContext) Add(key, command string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.commands[command] = true
	m := c.proc.GetAllMappings()
	m[key] = command
	c.proc.SetMappings(m)
	c.added++
	if c.ranks == nil {
		c.ranks = map[string]boundCommand{}
	}
	c.ranks[key] = boundCommand{command: command, serial: c.added}
}

// KeyForCommand returns the key to SHOW for a command here: of the keys bound
// to it that this situation actually offers, the one bound most recently.
//
// This is the question a menu item asks. It never advertises a key the
// situation cannot run -- a context carries only what is on offer, so a
// binding that belongs to some other situation is not a candidate -- and among
// the ones it can, the newest wins, which is the ini file over the defaults
// and the host over both. Empty when nothing here runs the command.
func (c *KeyContext) KeyForCommand(command string) string {
	if c == nil || command == "" {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	best, bestRank := "", boundCommand{}
	for key, cmd := range c.proc.GetAllMappings() {
		if cmd != command {
			continue
		}
		rank := c.ranks[key]
		// Ties (a context assembled without ranks) fall back to the greater
		// spelling, so the answer is never a coin toss on map order.
		if best == "" || rank.outranks(bestRank) ||
			(rank == bestRank && key > best) {
			best, bestRank = key, rank
		}
	}
	return best
}

// ClearAccelerators drops every formed accelerator from this context, leaving
// the configured bindings alone.
//
// The menu bar calls this before it works out which accelerators it can have,
// because the question it asks -- has something CLAIMED this chord? -- must
// not be answered by the accelerators it formed last time. Without it the bar
// reads its own previous assignment as a clash and mutes every accelerator it
// had. It also drops entries for menus that no longer exist.
func (c *KeyContext) ClearAccelerators() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	m := c.proc.GetAllMappings()
	changed := false
	for k, cmd := range m {
		if cmd == CommandAppAccelerator {
			delete(m, k)
			changed = true
		}
	}
	if changed {
		c.proc.SetMappings(m)
	}
}

// Claims reports whether a key or sequence already resolves to something here.
// This is what a menu accelerator asks before forming: a chord something else
// has claimed is not the accelerator's to take.
func (c *KeyContext) Claims(key string) bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.proc.GetAllMappings() {
		if k == key {
			return true
		}
	}
	return false
}

// Resolve reports the command a key means here, "" for none. A key that opens
// a longer sequence resolves to nothing yet and the prefix is held, which is
// why the context is stateful.
func (c *KeyContext) Resolve(key string) string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	pending := c.proc.GetActiveSequence()
	cmd := c.proc.ProcessKey(key).Command
	if cmd != "" {
		if pending != "" {
			c.matched = pending + " " + key
		} else {
			c.matched = key
		}
	}
	return cmd
}

// MatchedSequence returns the whole sequence the last successful Resolve
// consumed. It is how a command that carries no identity says which one it
// was: every menu accelerator resolves to the same command, and the key is
// what distinguishes them.
func (c *KeyContext) MatchedSequence() string {
	if c == nil {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.matched
}

// Abandon drops a partly-typed sequence.
func (c *KeyContext) Abandon() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.proc.ClearActiveSequence()
}

// CommandAppAccelerator is the command a formed menu accelerator resolves to.
//
// It carries no identity: the KEY does that, and the menu bar looks up which
// menu the letter belongs to exactly as it always has. Deliberately not
// registered as a script command — it is a routing artifact, not a feature,
// and a script that wants the Help menu should say so rather than pretend to
// press a key. The leading underscore marks it internal.
const CommandAppAccelerator = "_app_accel"

// DefaultAcceleratorChord is the pattern menu accelerators are formed from
// when nothing configures one.
const DefaultAcceleratorChord = "M-*"

var (
	keymapMu         sync.Mutex
	defaultRegistry  *KeyRegistry
	acceleratorChord = DefaultAcceleratorChord
)

// DefaultKeyRegistry returns the process-wide "default" registry, the one a
// scope resolves against unless something overrides it.
//
// It is PARSED from DefaultKeymapConfig, which is the toolkit's keymap written
// in the configuration language rather than in Go. Nothing the default says is
// therefore something a user's [mappings] section could not have said, and the
// two cannot fall out of step, because there is only one of them.
func DefaultKeyRegistry() *KeyRegistry {
	keymapMu.Lock()
	defer keymapMu.Unlock()
	if defaultRegistry == nil {
		defaultRegistry = NewKeyRegistry("default", ParseKeymap(DefaultKeymapConfig))
	}
	return defaultRegistry
}

// AcceleratorChord returns the pattern menu accelerators are formed from.
func AcceleratorChord() string {
	keymapMu.Lock()
	defer keymapMu.Unlock()
	return acceleratorChord
}

// ApplyHostKeymap overlays a host's configuration onto the default registry —
// the [mappings] section as ParseKeymap read it, and [window]
// accelerator_chord — in the order the file wrote them.
//
// Every line ADDS a meaning to its key. Only a BLANK command takes anything
// away, and it takes away everything the key meant, so a file that wants to
// say a key over from scratch blanks it first:
//
//	Space =                     ; Space now means nothing
//	Space = trinket_type_space
//	Space = trinket_activate
//
// A key the file does not name keeps what it had.
//
// A line is applied with Prefer rather than AddBinding, so restating a binding
// the registry already has is NOT a no-op: it takes a fresh serial. It has to,
// because a line means the same thing in a user's file as it does in
// DefaultKeymapConfig, and there a line's PLACE is a statement — among several
// keys for one command, the last one written is the one menus advertise. A
// file that writes
//
//	Return = trinket_activate
//	Space  = trinket_activate
//
// is saying "advertise Space", and it says that whether or not both keys
// already meant activate. Ignoring a restated line would make the same text
// mean two different things depending on which side of the config boundary it
// was written on.
//
// Where the line lands in the key's own list of meanings does not move, so
// what a keystroke DOES is unchanged; only what a menu shows for the command
// can change. Restating the whole table therefore lands back on it: every
// serial is reissued, but in the same order, so every ranking survives.
//
// A blank chord leaves the default in place; to turn chord accelerators off,
// configure a pattern with no "*" in it. The bare letters a focused menu bar
// answers to are ordinary typing and are unaffected either way.
func ApplyHostKeymap(mappings []Binding, chord string) {
	r := DefaultKeyRegistry()
	for _, b := range mappings {
		for _, cmd := range b.Commands {
			if cmd == "" {
				r.Bind(b.Key, "")
				continue
			}
			r.Prefer(b.Key, cmd)
		}
	}
	if chord != "" {
		keymapMu.Lock()
		acceleratorChord = chord
		keymapMu.Unlock()
	}
}

// A UIState is a situation the keyboard can be in. It is what a context is
// keyed on — not which trinket has focus, since the states that matter are not
// all trinkets: a window's title bar is a mode of the window, and a dropped-
// down menu is a mode of the bar.
//
// Nothing has to register anything. Each state's command set is what the code
// handling that state already switches on; this table just says it in one
// place instead of leaving it implicit in a gate somewhere.
type UIState int

const (
	// StateNormal is the ordinary situation: a window or the desktop, with no
	// mode claiming the keyboard.
	StateNormal UIState = iota
	// StateTitleBarFocused is a window whose title bar has focus, where the
	// arrows move and size the window. Sixteen bindings exist only here —
	// which is why this is a state and not a property of a trinket.
	StateTitleBarFocused
)

// stateCommands lists what each state ADDS to StateNormal. States compound:
// a focused title bar still closes on M-F4.
var stateCommands = map[UIState][]string{
	StateNormal: {
		CmdWindowMaximizeToggle, CmdWindowClose, CmdAppMenu, CmdAppHelp,
		CmdWindowNext, CmdWindowPrior,
		CmdWindowMDINext, CmdWindowMDIPrior,
		CmdAppMinimize, CmdAppQuit,
		CmdAppHide, CmdAppHideOthers, CmdAppShowAll, CmdDesktopExit,
		// The standard Edit menu's commands are deliberately NOT here. They
		// act on whatever has the keyboard, so they belong to the focused
		// trinket's context, not to the frame's -- and a frame that claimed
		// them would take M-a away from the &Alphabet menu's accelerator. The
		// Edit items resolve their keys through the focus chain instead (see
		// FindKeyForCommand).
		CmdGUIScaleDown, CmdGUIScaleUp, CmdGUIScaleReset,
		CmdFocusNext, CmdFocusPrior,
	},
	StateTitleBarFocused: {
		CmdWindowCancelResize,
		CmdWindowMoveFineUp, CmdWindowMoveFineDown,
		CmdWindowMoveFineLeft, CmdWindowMoveFineRight,
		CmdWindowSizeFineUp, CmdWindowSizeFineDown,
		CmdWindowSizeFineLeft, CmdWindowSizeFineRight,
		CmdWindowMoveUp, CmdWindowMoveDown,
		CmdWindowMoveLeft, CmdWindowMoveRight,
		CmdWindowSizeUp, CmdWindowSizeDown,
		CmdWindowSizeLeft, CmdWindowSizeRight,
	},
}

// CommandsForState returns everything a state offers, its own additions on top
// of the ordinary set.
func CommandsForState(state UIState) []string {
	out := append([]string(nil), stateCommands[StateNormal]...)
	if state != StateNormal {
		out = append(out, stateCommands[state]...)
	}
	return out
}

// BuildStateContext derives the context for a UI state: the bindings for the
// commands that state offers, and nothing else. A key bound to something the
// state cannot do stays unclaimed here, which is what keeps an arrow key from
// being swallowed by a window-move binding that only applies with the title
// bar focused.
func (r *KeyRegistry) BuildStateContext(state UIState) *KeyContext {
	return r.BuildContext(CommandsForState(state))
}

// TrinketKeys is the small amount of state a trinket needs to resolve keys
// through the registry instead of matching strings. Embed it, declare what the
// trinket can do, and switch on the command.
//
// The context is built lazily and rebuilt when the registry moves on, which is
// a revision comparison rather than a subscription — a trinket does not have
// to be told that a binding changed, it notices the next time it is asked.
type TrinketKeys struct {
	mu       sync.Mutex
	commands []string
	ctx      *KeyContext
	rev      uint64
	built    bool
	// owner is the trinket these keys belong to, which is how the registry in
	// force where it SITS is found (see keyscope.go). Wired by TrinketBase.Init
	// for anything that embeds both; a holder that is not a trinket -- a focus
	// manager, a tear-off host -- says so itself.
	owner Trinket
	// reg is the registry the current context was built from, so a context
	// built under one keymap is rebuilt when another comes into force. Focus
	// moving into a trinket that took the keyboard for itself changes which
	// registry answers without changing any registry's revision.
	reg *KeyRegistry
}

// SetKeyOwner names the trinket these keys belong to, so they resolve through
// the registry in force where it sits rather than the process-wide default.
func (t *TrinketKeys) SetKeyOwner(owner Trinket) {
	t.mu.Lock()
	if t.owner != owner {
		t.owner = owner
		t.built = false
	}
	t.mu.Unlock()
}

// registry is the keymap these keys resolve against: the one in force where
// the owning trinket sits, or the default when they belong to no trinket.
func (t *TrinketKeys) registry() *KeyRegistry {
	if t.owner == nil {
		return DefaultKeyRegistry()
	}
	// A window or the desktop resolves for where the FOCUS is, not for where
	// it sits: its own frame commands stand down while something inside it has
	// the keyboard on its own terms.
	if f, ok := t.owner.(KeyRegistryFocuser); ok {
		if r := f.FocusedKeyRegistry(); r != nil {
			return r
		}
	}
	return FindKeyRegistry(t.owner)
}

// SetCommands declares everything this trinket can carry out. A key bound to
// anything else stays unclaimed here and falls through untouched, which is
// what keeps a list from swallowing an arrow that belongs to a window.
//
// Declare every form that makes sense: where a trinket steps in one dimension
// and does not care whether the word for it is "prior" or "up", offering both
// lets either binding reach it.
func (t *TrinketKeys) SetCommands(commands ...string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.commands = commands
	t.built = false
}

// KeyCommand resolves a key to one of this trinket's commands, or "" when the
// key means nothing here. A key that opens a longer sequence resolves to
// nothing yet and the prefix is held.
func (t *TrinketKeys) KeyCommand(key string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	r := t.registry()
	if !t.built || t.rev != r.Revision() || t.reg != r {
		t.ctx = r.BuildContext(t.commands)
		t.rev = r.Revision()
		t.reg = r
		t.built = true
	}
	return t.ctx.Resolve(key)
}

// AbandonKeySequence drops a partly-typed sequence.
// KeyForCommand reports which key means a command in THIS trinket's context,
// or "" when it does not offer the command at all. It is what a menu item's
// column asks its way up the focus chain (see FindKeyForCommand).
func (t *TrinketKeys) KeyForCommand(command string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	r := t.registry()
	if !t.built || t.rev != r.Revision() || t.reg != r {
		t.ctx = r.BuildContext(t.commands)
		t.rev = r.Revision()
		t.reg = r
		t.built = true
	}
	return t.ctx.KeyForCommand(command)
}

func (t *TrinketKeys) AbandonKeySequence() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ctx.Abandon()
}
