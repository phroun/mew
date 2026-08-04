// Package ifitfits is a renderer-agnostic viewport tiling engine: an oriented
// tree of tiles that negotiates space by a minimums → pins → zoom → natural →
// shrink → mop-up waterfall, with tabbed stacks, magnify lenses, and a caret
// that steers directional navigation.
//
// The library owns the layout, not the host's focus. Every command takes an
// explicit tile Handle; the host reports focus (SetFocus) only so an active
// lens knows when to dismiss. Coordinates are whatever the host uses — the
// engine only does arithmetic on the numbers it is given.
//
// See COMMANDS.md for the mew (PawScript) command names these methods mirror.
package ifitfits

// Handle is an opaque, unique identifier for a tile (a leaf of the layout tree).
// It is distinct from a tile's ref: a ref is host content identity and may be
// shared across many tiles, while a handle names exactly one tile.
type Handle uint64

// Direction values accepted by the structural and navigation commands.
type Direction uint8

const (
	Up Direction = iota
	Down
	Left
	Right
	Before // insertion: earlier in the group's own flow
	After  // insertion: later in the group's own flow
	Prior  // reading-order previous (navigation cycle)
	Next   // reading-order next (navigation cycle)
)

// Mode is a tile's size-negotiation mode within its parent's waterfall.
type Mode uint8

const (
	Normal Mode = iota
	Zoom
	Shrink
)

// State is the tri-state used by toggling commands (Stack, Zoom, Shrink, lenses).
// The zero value is Toggle, matching the mew "omit = toggle" default.
type State uint8

const (
	Toggle State = iota
	On
	Off
)

// Orient is a group's flow direction.
type Orient uint8

const (
	LTR Orient = iota
	RTL
	TTB
	BTT
)

// Rect is an axis-aligned box in the host's coordinate space.
type Rect struct{ X, Y, W, H float64 }

// Box is one resolved, on-screen tile, handed to the host for drawing.
type Box struct {
	Tile     Handle
	Ref      string
	Rect     Rect
	Mode     Mode
	Pinned   bool
	Selected bool // the shown tab of a stacked parent
	Priority int
}

type kind uint8

const (
	leafKind kind = iota
	groupKind
)

type pinState struct {
	amount   float64
	has      bool
	enforced bool
}

type node struct {
	handle Handle // nonzero for leaves; 0 for groups
	kind   kind
	ref    string

	parent   *node
	children []*node

	// leaf intrinsics (host-provided; defaults below)
	minW, minH, natW, natH float64
	priority               int

	// box state — a leaf's own, or a stacked group's negotiation identity
	orient   Orient
	mode     Mode
	pin      pinState
	selected bool // shown tab of a stacked parent

	// scratch, recomputed each resolve
	mMinW, mMinH, mNatW, mNatH float64 // measured bounding minimums/naturals
	eff                        float64 // effective priority
	rect                       Rect    // rendered box (may be lensed)
	navRect                    Rect    // true box (pre-lens), for navigation
	hidden                     bool    // not drawn
	navHidden                  bool    // not present for navigation
	why                        string  // "omit" | "tab" | "lens"
	main, mainMin, mainNat     float64
	omit                       bool
	headerH                    float64
}

// lensState is stored by tile handle + flags (not a node pointer) so it survives
// tree normalization; the magnified node is re-derived from the tile each resolve.
type lensState struct {
	tile  Handle // the tile the lens was invoked on
	group bool   // false = monocle (magnify the tile), true = spectacle (its group)
	scope string // "screen" | "group"
}

// Viewport is a tiling layout. It is not safe for concurrent use.
type Viewport struct {
	root    *node
	w, h    float64
	caretX  float64
	caretY  float64
	focus   *node // last tile the host reported (drives lens dismissal only)
	lens    *lensState
	visCur  *node // cursor for the loose tab-cycle odometer
	handles map[Handle]*node
	nextH   Handle
	dirty   bool
}

// tunables (host-neutral defaults; the host may override per tile later).
const (
	defMinW = 90
	defMinH = 64
	defNatW = 240
	defNatH = 150
	headerH = 24
)

// NewViewport creates a viewport of the given size holding a single tile, and
// returns the viewport and that tile's handle.
func NewViewport(width, height float64) (*Viewport, Handle) {
	v := &Viewport{w: width, h: height, handles: map[Handle]*node{}, dirty: true}
	first := v.newLeaf("")
	v.root = &node{kind: groupKind, orient: LTR, children: []*node{first}}
	first.parent = v.root
	return v, first.handle
}

func (v *Viewport) newLeaf(ref string) *node {
	v.nextH++
	n := &node{
		handle: v.nextH, kind: leafKind, ref: ref,
		minW: defMinW, minH: defMinH, natW: defNatW, natH: defNatH,
	}
	v.handles[n.handle] = n
	return n
}

// newGroup builds a group over kids WITHOUT rewiring their parent pointers.
// Callers that wrap an existing node (e.g. replaceNode(s, newGroup(o, s, nl)))
// rely on that node's parent still pointing at its ORIGINAL enclosing group
// until the structural op finishes; resolve() calls rebuildParents to make every
// parent pointer consistent before any layout or navigation reads it.
func (v *Viewport) newGroup(o Orient, kids ...*node) *node {
	return &node{kind: groupKind, orient: o, children: append([]*node(nil), kids...)}
}

// ---- tree helpers ----

func (v *Viewport) tile(h Handle) *node {
	n := v.handles[h]
	if n != nil && n.kind == leafKind {
		return n
	}
	return nil
}

func walk(n *node, f func(*node)) {
	if n == nil {
		return
	}
	f(n)
	for _, c := range n.children {
		walk(c, f)
	}
}

func isH(o Orient) bool { return o == LTR || o == RTL }
func axisOf(o Orient) byte {
	if isH(o) {
		return 'H'
	}
	return 'V'
}
func defOrient(ax byte) Orient {
	if ax == 'H' {
		return LTR
	}
	return TTB
}

// stacked reports whether a group is a tabbed stack (some child is selected).
func stacked(n *node) bool {
	if n.kind != groupKind {
		return false
	}
	for _, c := range n.children {
		if c.selected {
			return true
		}
	}
	return false
}

func selectedChild(n *node) *node {
	for _, c := range n.children {
		if c.selected {
			return c
		}
	}
	if len(n.children) > 0 {
		return n.children[0]
	}
	return nil
}

func selectChild(g, child *node) {
	for _, c := range g.children {
		c.selected = c == child
	}
}

// axOf is a node's effective axis: 'Z' while stacked, else its orientation axis.
func axOf(n *node) byte {
	if n.kind == leafKind {
		return 0
	}
	if stacked(n) {
		return 'Z'
	}
	return axisOf(n.orient)
}

func firstLeaf(n *node) *node {
	var r *node
	walk(n, func(x *node) {
		if r == nil && x.kind == leafKind {
			r = x
		}
	})
	return r
}

func lastLeaf(n *node) *node {
	var r *node
	walk(n, func(x *node) {
		if x.kind == leafKind {
			r = x
		}
	})
	return r
}

func leavesOf(n *node) []*node {
	var a []*node
	walk(n, func(x *node) {
		if x.kind == leafKind {
			a = append(a, x)
		}
	})
	return a
}

func rebuildParents(root *node) {
	root.parent = nil
	walk(root, func(n *node) {
		for _, c := range n.children {
			c.parent = n
		}
	})
}

func indexOf(g, c *node) int {
	for i, x := range g.children {
		if x == c {
			return i
		}
	}
	return -1
}

// mark dirty so the next geometry query re-resolves.
func (v *Viewport) touch() { v.dirty = true }

// ---- host render / query surface ----

// SetWorkspace sets the workspace size that layout fills.
func (v *Viewport) SetWorkspace(width, height float64) {
	v.w, v.h = width, height
	v.touch()
}

// Caret returns the current caret goal in workspace coordinates.
func (v *Viewport) Caret() (x, y float64) {
	v.ensure()
	return v.caretX, v.caretY
}

// Tiles returns the resolved, on-screen tiles for the host to draw, in reading
// order.
func (v *Viewport) Tiles() []Box {
	v.ensure()
	var out []Box
	walk(v.root, func(n *node) {
		if n.kind != leafKind || n.hidden {
			return
		}
		out = append(out, Box{
			Tile: n.handle, Ref: n.ref, Rect: n.rect, Mode: n.mode,
			Pinned: n.pin.has, Selected: n.selected, Priority: n.priority,
		})
	})
	return out
}

// Tab is one entry of a stack's header, for the host to draw a tab strip. Tile is
// the leaf revealing that tab (pass it to Reveal on a click).
type Tab struct {
	Tile     Handle
	Ref      string
	Selected bool
}

// Stack is a visible tabbed stack: its box, the header reserve, and its tabs.
type Stack struct {
	Rect    Rect
	HeaderH float64
	Tabs    []Tab
}

// Stacks returns the visible tabbed stacks so the host can draw their tab strips
// (including tabs not currently shown — which Tiles omits).
func (v *Viewport) Stacks() []Stack {
	v.ensure()
	var out []Stack
	walk(v.root, func(n *node) {
		if n.kind != groupKind || !stacked(n) || n.hidden {
			return
		}
		tabs := make([]Tab, 0, len(n.children))
		for _, c := range n.children {
			var h Handle
			var ref string
			if l := focusEntry(c); l != nil {
				h, ref = l.handle, l.ref
			}
			tabs = append(tabs, Tab{Tile: h, Ref: ref, Selected: c.selected})
		}
		out = append(out, Stack{Rect: n.rect, HeaderH: n.headerH, Tabs: tabs})
	})
	return out
}

// Reveal makes a tile the shown tab of every stacked ancestor, so it is no longer
// hidden behind a tab. Returns the tile.
func (v *Viewport) Reveal(tile Handle) Handle {
	t := v.tile(tile)
	if t == nil {
		return 0
	}
	for n := t; n != nil && n.parent != nil; n = n.parent {
		if stacked(n.parent) && !n.selected {
			selectChild(n.parent, n)
		}
	}
	v.touch()
	return tile
}

// SetMetrics sets a tile's intrinsic minimum and natural sizes (host content
// hints). Zero values are left unchanged.
func (v *Viewport) SetMetrics(tile Handle, minW, minH, natW, natH float64) {
	n := v.tile(tile)
	if n == nil {
		return
	}
	if minW > 0 {
		n.minW = minW
	}
	if minH > 0 {
		n.minH = minH
	}
	if natW > 0 {
		n.natW = natW
	}
	if natH > 0 {
		n.natH = natH
	}
	v.touch()
}

// SetPriority sets a tile's overflow priority (higher survives longer).
func (v *Viewport) SetPriority(tile Handle, p int) {
	if n := v.tile(tile); n != nil {
		n.priority = p
		v.touch()
	}
}

// ---- refs ----

// Content returns the ref currently carried by a tile.
func (v *Viewport) Content(tile Handle) string {
	if n := v.tile(tile); n != nil {
		return n.ref
	}
	return ""
}

// Set replaces a tile's ref.
func (v *Viewport) Set(tile Handle, ref string) {
	if n := v.tile(tile); n != nil {
		n.ref = ref
	}
}

// Get returns every tile carrying ref. When includeHidden is false, tiles that
// are not currently on screen (hidden tabs or overflow omissions) are skipped.
func (v *Viewport) Get(ref string, includeHidden bool) []Handle {
	if !includeHidden {
		v.ensure()
	}
	var out []Handle
	walk(v.root, func(n *node) {
		if n.kind == leafKind && n.ref == ref && (includeHidden || !n.hidden) {
			out = append(out, n.handle)
		}
	})
	return out
}
