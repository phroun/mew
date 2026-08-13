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
//
// It is a LIST, and the order matters at exactly one point: where several keys
// mean the same command, the one written LAST is the one a menu advertises for
// it. Both keep working -- the order decides what is SHOWN, never what runs --
// so where the reading order and the conventional spelling disagree (F2 before
// F10, Space before Return) the entry is placed to say which one to show and
// the comment says why.
var defaultBindings = []Binding{
	// Windows and the desktop.
	{"M-F10", []string{CmdWindowMaximizeToggle}},
	// The application-vs-window split every desktop draws the same way:
	// M-F4 and ^Q end the APPLICATION, ^F4 and ^W close one WINDOW of it.
	// (^F4 and C-F4 are the same key spelled two ways, as are ^Q and C-Q.)
	{"M-F4", []string{CmdAppQuit}},
	{"^Q", []string{CmdAppQuit}},
	{"^F4", []string{CmdWindowClose}},
	{"^W", []string{CmdWindowClose}},
	// The menu key twice over. F2 is there to be REACHABLE -- on a Mac every
	// function key wants fn, which sits bottom-left, so F10 across at the far
	// right is a two-handed press while F2 is on the same side of the
	// keyboard. F10 is LAST because it is the convention every desktop shares,
	// and last is what gets advertised: both open the menu, one is what a menu
	// item says about itself.
	{"F2", []string{CmdAppMenu}},
	{"F10", []string{CmdAppMenu}},
	// Help, where every desktop puts it.
	{"F1", []string{CmdAppHelp}},
	{"M-Tab", []string{CmdWindowNext}},
	{"M-S-Tab", []string{CmdWindowPrior}},
	{"C-Tab", []string{CmdWindowMDINext}},
	{"C-S-Tab", []string{CmdWindowMDIPrior}},
	{"s-M", []string{CmdAppMinimize}},
	{"s-Minus", []string{CmdGUIScaleDown}},
	{"s-Plus", []string{CmdGUIScaleUp}},
	{"s-0", []string{CmdGUIScaleReset}},

	// Focus, which belongs to no one trinket.
	{"Tab", []string{CmdFocusNext}},
	{"S-Tab", []string{CmdFocusPrior}},

	// Keys that mean one thing to a window's title bar and another to
	// whatever has focus otherwise.
	{"Esc", []string{CmdWindowCancelResize, CmdTrinketCancel}},
	// Return begins an edit where one is on offer and activates otherwise;
	// Space only ever activates, which is why a tree's Space expands a branch
	// where its Return opens the row editor.
	//
	// Return is the HOME ROW's key. It and the keypad's are two physical keys
	// with two names -- keyseq deliberately does not alias them, and this is
	// the one that was meant. Bind "Enter" as well to give the keypad the same
	// meaning; nothing does so by default.
	//
	// Space is written FIRST and Return second, so activation advertises
	// Return: both do it, and Return is the one people mean by "press enter".
	{"Space", []string{CmdTrinketActivate}},
	{"Return", []string{CmdTrinketEdit, CmdTrinketActivate}},

	// The bare arrows also carry the FINE resize, for a splitter: nudging a
	// divider is resizing, and a splitter is the one thing that resizes with
	// no modifier at all. A title bar offers both families and takes the move,
	// since that is listed first; a splitter offers only the size family and
	// so is the only place the second meaning is ever reached.
	// Each arrow names its own direction first and the sequence synonym
	// second. A list means the two identically and shares one case, so the
	// order costs it nothing; a GRID means them separately -- the dock's Up
	// crosses a row where its Left steps one entry -- and there the arrow that
	// points that way has to win.
	{"Up", []string{CmdWindowMoveFineUp, CmdWindowSizeFineUp, CmdTrinketItemUp, CmdTrinketItemPrior}},
	{"Down", []string{CmdWindowMoveFineDown, CmdWindowSizeFineDown, CmdTrinketItemDown, CmdTrinketItemNext}},
	// Collapse and expand belong to Minus and Plus alone. The arrows are the
	// generic movement, which a tree happens to implement as
	// collapse-or-walk-up -- so there is nothing here for the two to fight
	// over, and no precedence rule needed to separate them.
	{"Left", []string{CmdWindowMoveFineLeft, CmdWindowSizeFineLeft, CmdTrinketItemLeft, CmdTrinketItemPrior}},
	{"Right", []string{CmdWindowMoveFineRight, CmdWindowSizeFineRight, CmdTrinketItemRight, CmdTrinketItemNext}},

	{"S-Up", []string{CmdWindowSizeFineUp, CmdTrinketSelUp, CmdTerminalScrollUp}},
	{"S-Down", []string{CmdWindowSizeFineDown, CmdTrinketSelDown, CmdTerminalScrollDown}},
	// The shifted arrows also carry the classic tree movement, ahead of the
	// generic item step: in an editable grid the plain arrows walk the
	// edit-target column, and this is how collapse-or-walk-out keeps a key
	// there. A trinket that does not offer it falls through to item_left as
	// before.
	{"S-Left", []string{CmdWindowSizeFineLeft, CmdTrinketSelLeft, CmdTrinketCollapseOrEnclosing, CmdTrinketItemLeft}},
	{"S-Right", []string{CmdWindowSizeFineRight, CmdTrinketSelRight, CmdTrinketExpandOrDescend, CmdTrinketItemRight}},

	{"Home", []string{CmdTrinketBeg}},
	{"End", []string{CmdTrinketEnd}},
	{"S-Home", []string{CmdTrinketSelBeg, CmdTerminalScrollBeg}},
	{"S-End", []string{CmdTrinketSelEnd, CmdTerminalScrollEnd}},

	{"PageUp", []string{CmdTrinketPagePrior}},
	{"PageDown", []string{CmdTrinketPageNext}},
	// The shifted paging keys belong to a terminal's scrollback alone --
	// nothing else answers to them, which is why they are bound to one
	// command rather than added to a list.
	{"S-PageUp", []string{CmdTerminalScrollPagePrior}},
	{"S-PageDown", []string{CmdTerminalScrollPageNext}},

	// Editing, where a trinket holds text.
	{"Backspace", []string{CmdTrinketDelPrior, CmdTrinketEnclosing}},
	{"Delete", []string{CmdTrinketDelNext}},
	{"^U", []string{CmdTrinketDelLine}},
	// ^A is the Emacs home cycle where a trinket offers it, and a plain
	// beginning-of-line where it does not. First listed wins, so a text field
	// -- which offers both -- gets the cycle, and a list gets the plain move.
	{"^A", []string{CmdTrinketBegOrSelectAll, CmdTrinketBeg}},
	{"^E", []string{CmdTrinketEnd}},
	{"S-^A", []string{CmdTrinketSelBeg}},
	{"S-^E", []string{CmdTrinketSelEnd}},
	{"M-a", []string{CmdTrinketSelectAll}},

	// Trees.
	{"Plus", []string{CmdTrinketExpand}},
	{"Minus", []string{CmdTrinketCollapse}},
	{"Asterisk", []string{CmdTrinketExpandAll}},
	{"Slash", []string{CmdTrinketCollapseAll}},

	// Dropping a combo box open.
	{"F4", []string{CmdTrinketOpen}},

	// The coarse window move and size, and the same step on a splitter, which
	// is resizing something too.
	// The modified arrows carry the coarse size the same way the bare ones
	// carry the fine one, and for the same reason: a splitter's big step.
	// The beginning/end meanings on the modified arrows are for whatever runs
	// along that axis -- a tab bar's first and last tab -- and sit after the
	// scroll, which is what a list means by the same key.
	{"C-Up", []string{CmdWindowMoveUp, CmdWindowSizeUp, CmdTrinketScrollUp, CmdTrinketBeg}},
	{"M-Up", []string{CmdWindowMoveUp, CmdWindowSizeUp, CmdTrinketScrollUp, CmdTrinketOpen, CmdTrinketBeg}},
	{"s-Up", []string{CmdWindowMoveUp}},
	{"C-Down", []string{CmdWindowMoveDown, CmdWindowSizeDown, CmdTrinketScrollDown, CmdTrinketEnd}},
	{"M-Down", []string{CmdWindowMoveDown, CmdWindowSizeDown, CmdTrinketScrollDown, CmdTrinketOpen, CmdTrinketEnd}},
	{"s-Down", []string{CmdWindowMoveDown}},
	{"C-Left", []string{CmdWindowMoveLeft, CmdWindowSizeLeft, CmdTrinketBeg}},
	{"M-Left", []string{CmdWindowMoveLeft, CmdWindowSizeLeft, CmdTrinketBeg}},
	{"s-Left", []string{CmdWindowMoveLeft}},
	{"C-Right", []string{CmdWindowMoveRight, CmdWindowSizeRight, CmdTrinketEnd}},
	{"M-Right", []string{CmdWindowMoveRight, CmdWindowSizeRight, CmdTrinketEnd}},
	{"s-Right", []string{CmdWindowMoveRight}},

	{"C-S-Up", []string{CmdWindowSizeUp}},
	{"M-S-Up", []string{CmdWindowSizeUp}},
	{"S-s-Up", []string{CmdWindowSizeUp}},
	{"C-S-Down", []string{CmdWindowSizeDown}},
	{"M-S-Down", []string{CmdWindowSizeDown}},
	{"S-s-Down", []string{CmdWindowSizeDown}},
	{"C-S-Left", []string{CmdWindowSizeLeft}},
	{"M-S-Left", []string{CmdWindowSizeLeft}},
	{"S-s-Left", []string{CmdWindowSizeLeft}},
	{"C-S-Right", []string{CmdWindowSizeRight}},
	{"M-S-Right", []string{CmdWindowSizeRight}},
	{"S-s-Right", []string{CmdWindowSizeRight}},

	// The MDI meanings are the tab-strip cycle, which is what Ctrl+PageUp/Down
	// does everywhere tabs exist. A scrolling trinket means the page step by
	// the same key and is listed first, so nothing that scrolls changes.
	{"C-PageUp", []string{CmdTrinketPagePrior, CmdWindowMDIPrior}},
	{"C-PageDown", []string{CmdTrinketPageNext, CmdWindowMDINext}},
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
	// Applied in key order, so the serials a file's bindings land on are the
	// same every run: what a menu advertises must not depend on Go's map
	// iteration. (Every one of them still outranks every default, which is the
	// part that matters -- the file is later than the table it overlays.)
	keys := make([]string, 0, len(mappings))
	for k := range mappings {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		r.Bind(k, mappings[k])
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
func (t *TrinketKeys) AbandonKeySequence() {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.ctx.Abandon()
}
