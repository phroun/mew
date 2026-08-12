package core

import (
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
	bindings map[string][]string
	revision uint64
}

// NewKeyRegistry creates a registry from a key-to-command table. The name is
// for diagnostics — "default", "purfecterm-captured" — and has no behaviour.
func NewKeyRegistry(name string, bindings map[string][]string) *KeyRegistry {
	r := &KeyRegistry{name: name, bindings: make(map[string][]string, len(bindings))}
	for k, v := range bindings {
		r.bindings[k] = append([]string(nil), v...)
	}
	return r
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
	r.mu.Lock()
	defer r.mu.Unlock()
	if command == "" {
		delete(r.bindings, key)
	} else {
		r.bindings[key] = []string{command}
	}
	r.revision++
}

// AddBinding gives a key one more meaning, for the situations that do not
// overlap with the ones it already has.
func (r *KeyRegistry) AddBinding(key, command string) {
	if command == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, c := range r.bindings[key] {
		if c == command {
			return
		}
	}
	r.bindings[key] = append(r.bindings[key], command)
	r.revision++
}

// KeysFor returns every key bound to a command, in no particular order. A
// command may have several keys, or none — the coarse window move is bound to
// Ctrl, Meta and Super arrows alike, and a command nothing names is simply not
// reachable from the keyboard.
func (r *KeyRegistry) KeysFor(command string) []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	var keys []string
	for k, cmds := range r.bindings {
		for _, c := range cmds {
			if c == command {
				keys = append(keys, k)
				break
			}
		}
	}
	return keys
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
	}
	mappings := map[string]string{}
	if r != nil {
		r.mu.RLock()
		for k, cmds := range r.bindings {
			// At most one of a key's meanings is on offer here; the rest
			// belong to situations this is not. Where a trinket genuinely
			// answers to several forms, its own case accepts them all and it
			// decides from its state.
			for _, c := range cmds {
				if set[c] {
					mappings[k] = c
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

// defaultBindings is the toolkit's own keymap — the same table the shipped
// kittytk.ini writes out under [mappings], kept here so KittyTK has a default
// registry with no configuration file present at all. The file overlays this
// rather than replacing it, so a user names only what they want to change.
//
// A key lists every command it can mean. "Up" nudges a window while its title
// bar is focused and steps a list otherwise; both are true, and the situation
// decides. A context keeps whichever meaning it offers, so at most one
// survives anywhere the key is actually asked about.
//
// A trinket that can mean several of them by one key decides between them
// itself, from its own state, exactly as it does today: a tree view's Left
// collapses an expanded row and steps to the parent otherwise, and a
// multi-column one crosses columns. Its case accepts every form that makes
// sense for it and branches inside. Nothing about that moves into the
// registry.
//
// Several keys may also share ONE command: the coarse window move answers to
// Ctrl, Meta and Super arrows alike, which is what the hand-written handler
// did before these were bindings.
var defaultBindings = map[string][]string{
	// Windows and the desktop.
	"M-F10":   {CmdWindowMaximizeToggle},
	"M-F4":    {CmdWindowClose},
	"F10":     {CmdAppMenu},
	"M-Tab":   {CmdWindowNext},
	"M-S-Tab": {CmdWindowPrior},
	"C-Tab":   {CmdWindowMDINext},
	"C-S-Tab": {CmdWindowMDIPrior},
	"s-M":     {CmdAppMinimize},
	"s-Minus": {CmdGUIScaleDown},
	"s-Plus":  {CmdGUIScaleUp},
	"s-0":     {CmdGUIScaleReset},

	// Focus, which belongs to no one trinket.
	"Tab":   {CmdFocusNext},
	"S-Tab": {CmdFocusPrior},

	// Keys that mean one thing to a window's title bar and another to
	// whatever has focus otherwise.
	"Esc": {CmdWindowCancelResize, CmdTrinketCancel},
	// Enter begins an edit where one is on offer and activates otherwise;
	// Space only ever activates, which is why a tree's Space expands a branch
	// where its Enter opens the row editor.
	"Enter": {CmdTrinketEdit, CmdTrinketActivate},
	"Space": {CmdTrinketActivate},

	"Up":   {CmdWindowMoveFineUp, CmdTrinketItemPrior, CmdTrinketItemUp},
	"Down": {CmdWindowMoveFineDown, CmdTrinketItemNext, CmdTrinketItemDown},
	// Collapse and expand belong to Minus and Plus alone. The arrows are the
	// generic movement, which a tree happens to implement as
	// collapse-or-walk-up -- so there is nothing here for the two to fight
	// over, and no precedence rule needed to separate them.
	"Left":  {CmdWindowMoveFineLeft, CmdTrinketItemLeft, CmdTrinketItemPrior},
	"Right": {CmdWindowMoveFineRight, CmdTrinketItemRight, CmdTrinketItemNext},

	"S-Up":    {CmdWindowSizeFineUp, CmdTrinketSelUp},
	"S-Down":  {CmdWindowSizeFineDown, CmdTrinketSelDown},
	"S-Left":  {CmdWindowSizeFineLeft, CmdTrinketSelLeft, CmdTrinketItemLeft},
	"S-Right": {CmdWindowSizeFineRight, CmdTrinketSelRight, CmdTrinketItemRight},

	"Home":   {CmdTrinketBeg},
	"End":    {CmdTrinketEnd},
	"S-Home": {CmdTrinketSelBeg},
	"S-End":  {CmdTrinketSelEnd},

	"PageUp":   {CmdTrinketPagePrior},
	"PageDown": {CmdTrinketPageNext},

	// Editing, where a trinket holds text.
	"Backspace": {CmdTrinketDelPrior, CmdTrinketEnclosing},
	"Delete":    {CmdTrinketDelNext},
	"^U":        {CmdTrinketDelLine},
	"^A":        {CmdTrinketBeg},
	"^E":        {CmdTrinketEnd},
	"S-^A":      {CmdTrinketSelBeg},
	"S-^E":      {CmdTrinketSelEnd},
	"M-a":       {CmdTrinketSelectAll},

	// Trees.
	"Plus":     {CmdTrinketExpand},
	"Minus":    {CmdTrinketCollapse},
	"Asterisk": {CmdTrinketExpandAll},
	"Slash":    {CmdTrinketCollapseAll},

	// Dropping a combo box open.
	"F4": {CmdTrinketOpen},

	// The coarse window move and size, and the same step on a splitter, which
	// is resizing something too.
	"C-Up": {CmdWindowMoveUp, CmdTrinketScrollUp}, "M-Up": {CmdWindowMoveUp, CmdTrinketScrollUp, CmdTrinketOpen}, "s-Up": {CmdWindowMoveUp},
	"C-Down": {CmdWindowMoveDown, CmdTrinketScrollDown}, "M-Down": {CmdWindowMoveDown, CmdTrinketScrollDown, CmdTrinketOpen}, "s-Down": {CmdWindowMoveDown},
	"C-Left": {CmdWindowMoveLeft, CmdTrinketBeg}, "M-Left": {CmdWindowMoveLeft, CmdTrinketBeg}, "s-Left": {CmdWindowMoveLeft},
	"C-Right": {CmdWindowMoveRight, CmdTrinketEnd}, "M-Right": {CmdWindowMoveRight, CmdTrinketEnd}, "s-Right": {CmdWindowMoveRight},

	"C-S-Up": {CmdWindowSizeUp}, "M-S-Up": {CmdWindowSizeUp}, "S-s-Up": {CmdWindowSizeUp},
	"C-S-Down": {CmdWindowSizeDown}, "M-S-Down": {CmdWindowSizeDown}, "S-s-Down": {CmdWindowSizeDown},
	"C-S-Left": {CmdWindowSizeLeft}, "M-S-Left": {CmdWindowSizeLeft}, "S-s-Left": {CmdWindowSizeLeft},
	"C-S-Right": {CmdWindowSizeRight}, "M-S-Right": {CmdWindowSizeRight}, "S-s-Right": {CmdWindowSizeRight},

	"C-PageUp":   {CmdTrinketPagePrior},
	"C-PageDown": {CmdTrinketPageNext},
}

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
func DefaultKeyRegistry() *KeyRegistry {
	keymapMu.Lock()
	defer keymapMu.Unlock()
	if defaultRegistry == nil {
		defaultRegistry = NewKeyRegistry("default", defaultBindings)
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
// the [mappings] section and [window] accelerator_chord. Only the keys named
// are touched, so a user's file says what it changes rather than restating the
// whole table, and an empty command unbinds.
//
// A blank chord leaves the default in place; to turn chord accelerators off,
// configure a pattern with no "*" in it. The bare letters a focused menu bar
// answers to are ordinary typing and are unaffected either way.
func ApplyHostKeymap(mappings map[string]string, chord string) {
	r := DefaultKeyRegistry()
	for k, cmd := range mappings {
		r.Bind(k, cmd)
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
		CmdWindowMaximizeToggle, CmdWindowClose, CmdAppMenu,
		CmdWindowNext, CmdWindowPrior,
		CmdWindowMDINext, CmdWindowMDIPrior,
		CmdAppMinimize,
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
	r := DefaultKeyRegistry()
	if !t.built || t.rev != r.Revision() {
		t.ctx = r.BuildContext(t.commands)
		t.rev = r.Revision()
		t.built = true
	}
	return t.ctx.Resolve(key)
}

// AbandonKeySequence drops a partly-typed sequence.
func (t *TrinketKeys) AbandonKeySequence() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ctx.Abandon()
}
