// Package trinkets provides standard UI trinkets for KittyTK.
package trinkets

import (
	"fmt"
	"time"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/style"
	"github.com/phroun/serval"
)

// TreeItem represents an item in a TreeView.
type TreeItem struct {
	// ID is the item's stable object identity, allocated from the
	// same space as trinket ObjectIDs. Protocol-built items keep the
	// wire ID they were created under, so events, set, and destroy
	// address the same number the reply surfaced.
	ID core.ObjectID

	Text string
	// Icon is the NAME of a registered icon (style.RegisterIcon), not a
	// picture. A name nothing has registered draws nothing.
	Icon    string
	Data    interface{} // User data
	Enabled bool

	// ReadOnly holds this row out of the row editor, whatever its columns
	// allow. Editability is otherwise a column's answer -- every row in an
	// editable column can be edited -- and a list often holds rows that are
	// not the same kind of thing as the rest: a heading, a total, something
	// standing for the machine itself. Such a row is still selectable and
	// still reads normally; it is only not written in.
	ReadOnly bool
	Expanded bool
	Parent   *TreeItem
	Children []*TreeItem

	// Kids is how many children a SOURCE said this row has, for a row the tree
	// did not make. Zero means nobody said, and then the children themselves
	// answer -- which is what a tree of its own items has always done.
	//
	// It exists so that IsLeaf goes on being the one question fourteen places
	// ask. A row out of a source has no Children slice while it is collapsed,
	// so counting the slice would draw every closed node as a leaf; a row that
	// nobody could count is negative, which draws the twisty and finds out on
	// opening.
	Kids int

	// rowKey is the source's identity for a row that came from one. A made row
	// has none: its key is its ObjectID.
	rowKey *serval.Value

	// rowMark is the segment this row's MARK is filed under, which is not always
	// its identity: a level with a standing is marked by PATH, and the tree is
	// what says which -- see serval's TreeSource.MarkedByPath. Read once, when
	// the row is learned, because the record with the path in it is in hand then
	// and never afterwards.
	rowMark string

	// rowKind is which KIND of row this is, as the tree said. It decides which
	// mapping reads the record's members, so an edit needs it to know which member
	// it is editing -- and like the mark, it is only in hand while the record is.
	rowKind string

	// rowDepth is how deep the source said this row stands, and rowChain the mark
	// segments from the root down to it.
	//
	// **Both used to be worked out and neither can be, for a WINDOW.** The depth
	// became parentage and the chain walked back up it, which works for a view
	// holding the whole pre-order -- every ancestor is above its descendant -- and
	// is exactly what a view holding rows forty to eighty declined to hold. So both
	// are read off the row, where the walk that knew them wrote them down. See
	// serval's TreeFields.Chain.
	rowDepth int
	rowChain []string

	// Values holds this item's data-column cell text, keyed by
	// TreeColumn.ID (see SetValue/Value in treeview_columns.go).
	Values map[string]string
	// numValues caches each cell's numeric equivalent, parsed once in
	// SetValue so numeric sorts never re-convert text per comparison.
	numValues map[string]float64
}

// NewTreeItem creates a new tree item.
func NewTreeItem(text string) *TreeItem {
	return &TreeItem{
		ID:      core.NextObjectID(),
		Text:    text,
		Enabled: true,
	}
}

// AddChild adds a child item.
func (t *TreeItem) AddChild(child *TreeItem) {
	child.Parent = t
	t.Children = append(t.Children, child)
}

// RemoveChild removes a child item.
func (t *TreeItem) RemoveChild(child *TreeItem) {
	for i, c := range t.Children {
		if c == child {
			t.Children = append(t.Children[:i], t.Children[i+1:]...)
			child.Parent = nil
			break
		}
	}
}

// IsLeaf returns whether this item has no children.
//
// A row out of a SOURCE answers from Kids, because its Children slice holds only
// what is visible and a collapsed node's is empty -- counting it would draw every
// closed node as a leaf. A row the tree made itself, and a source's row nobody
// could count, fall through to the slice, which is what this has always done.
func (t *TreeItem) IsLeaf() bool {
	if t.Kids != 0 {
		return false
	}
	return len(t.Children) == 0
}

// Key is the SOURCE's identity for this row, and empty for a row the tree made
// itself.
//
// It is what an application hangs its own knowledge of a row off, where the rows
// are a source's and the items were made to draw them: `Data` is the field for a
// row the caller built, and a row it never built has nowhere to have been given
// one. Keying a map by this instead says the same thing about a row that comes
// and goes as a subtree closes and opens.
func (t *TreeItem) Key() string {
	if t.rowKey == nil {
		return ""
	}
	return serval.Key(t.rowKey)
}

// Level returns the nesting level (0 for root items).
//
// **A row out of a SOURCE answers from what the source said**, and a row the tree
// made itself from its parentage. The same split `IsLeaf` makes, and for the same
// reason: a view holding a window of a tree holds no ancestor of the rows at the
// top of it, so walking up would report the window's edge as the top of the tree
// and draw a row forty deep flush against the margin.
func (t *TreeItem) Level() int {
	if t.rowKey != nil {
		return t.rowDepth
	}
	level := 0
	for p := t.Parent; p != nil; p = p.Parent {
		level++
	}
	return level
}

// TreeView displays a hierarchical tree of items.
type TreeView struct {
	core.TrinketBase
	core.TrinketKeys
	core.AccessibleTrinket

	// What this view was told was wrong, and whether it says so itself. See
	// trouble.go.
	troubled

	rootItems []*TreeItem
	// currentIndex is WHERE the chosen row stands, and -1 for a row the view
	// cannot place -- which is a row scrolled past, or one a reorder moved outside
	// the window, and is not the same as nothing being chosen. See chosen.
	currentIndex int
	scrollOffset int

	// chosen is the row the reader chose, as an IDENTITY.
	//
	// **Which is what a selection IS, and what an index only stands for.** An index
	// is a fact about the window: a resort moves the row, a node opening above it
	// moves the row, and scrolling away stops the view holding it at all -- and an
	// index kept as the authority quietly named a different row after any of the
	// three. The identity survives all of them, and `resolve` puts the index back
	// whenever the row is somewhere the spine can find it.
	//
	// Nil for nothing chosen, and nil for a BLANK: a row the view knows is there
	// and knows nothing else about has no identity to hold, so choosing one chooses
	// its place and the identity arrives with the record.
	chosen *serval.Value

	// bones is what the view knows about where its rows are: a few runs of
	// identities and depths anchored by position, plus how long the sequence is.
	// It is a WINDOW and never a log -- see spine.go -- which is what lets a tree
	// of a hundred thousand rows be read forty at a time.
	bones spine
	// asks numbers the stretches this view has asked for, so an answer for
	// somewhere the reader has since left can be recognised and dropped.
	asks asking

	// walks says this sequence will not jump to a POSITION, so the only question it
	// answers is one carrying on from a record.
	//
	// **Told rather than assumed.** It was asked to begin at a position and said it
	// began somewhere else, which is what a source walking its own body does -- there
	// being no index into a sequence somebody else named. An application that honours
	// `from` is never found out this way and is never made to walk.
	//
	// It only ever becomes true. A source that could not skip once will not learn to,
	// and a sequence stated afresh is a fresh spine and a fresh question. See window.
	walks bool

	// Where the rows come from (see treesource.go). A tree given no source
	// makes one out of its own items, so the rows are read out of a sequence
	// either way rather than walked for by the view.
	source serval.Source // declared: what SetSource was given
	made   *serval.TreeSource
	// grown is a tree built out of a declared source's own TREE HINT, where it
	// said one and was not already a tree. What a sequence is stated over is this
	// where it exists; `Source` still answers what the caller handed in.
	grown *serval.TreeSource
	// hintLabel is the field a hinted source said holds a record's own name,
	// which is the caption's weakest rung.
	hintLabel string
	// saidHint is a shape somebody TOLD this view its source's records are, for a
	// source that cannot say for itself -- one across a connection. See
	// SetTreeHint.
	saidHint serval.TreeHint
	set      serval.DataSet
	restate  bool
	// arrivals counts the sources this view has been pointed at, so a
	// subscription taken against one it has left knows to do nothing. See
	// arrival.go.
	arrivals int
	// byID leads a row's key back to the very item the caller handed in,
	// because everything reading a row compares pointers.
	byID map[core.ObjectID]*TreeItem
	// fromSource holds the items built out of a declared source's records,
	// keyed by the row's own identity, so the same row leads to the same pointer
	// across a rebuild.
	fromSource map[string]*TreeItem

	// fromTop is what stands at the top of a declared source, which is what
	// RootItems answers while one is being read. Kept apart from rootItems,
	// which is the caller's own list and is promised back.
	fromTop []*TreeItem
	// kinds is what each kind of row puts in the columns (see treemap.go),
	// keyed by the name its serval.NodeType is registered under. serval holds
	// the types; the view holds the mapping, because serval must not learn what
	// a column is.
	kinds map[string]NodeMap

	// Appearance
	indentWidth int // Characters per indent level

	// Double-click detection
	lastClickTime  int64 // Unix nano
	lastClickIndex int

	// Mouse state
	isDragging            bool
	scrollbarDragging     bool // Whether scrollbar thumb is being dragged
	scrollbarThumbHovered bool // Whether the pointer is over the thumb
	scrollbarDragStart    int  // Y position where drag started
	scrollbarDragOffset   int  // Scroll offset when drag started

	// Smooth (pixel-surface) scrollbar drag: the thumb follows the
	// pointer at unit granularity while scrollOffset snaps to whole
	// rows. scrollbarGrabOff is where the press landed within the
	// thumb; scrollbarThumbPos is the unsnapped thumb origin.
	smoothScrollbarDrag bool
	scrollbarGrabOff    float64
	scrollbarThumbPos   float64

	// Fractional rows carried between trackpad wheel events.
	wheelAccum float64

	// Multi-column state (see treeview_columns.go). The tree itself is
	// the KEY column; data columns render item cell values beside it.
	columns    []*TreeColumn
	showHeader bool
	showKey    bool      // key column shown as the first visible column
	keyCaption string    // header caption over the key (tree) column
	ledger     bool      // alternate non-selected rows in LedgerOdd/LedgerEven
	treeLines  bool      // connector lines + leaf glyphs in the indent space
	fitWidth   bool      // true: squeeze to width (no hscroll); false: pan
	fixedBegin int       // visible columns pinned outside the hscroll region,
	fixedEnd   int       // counted from the run's beginning and its end
	keyWidth   core.Unit // key column width in scroll mode (0 = default)
	hScroll    core.Unit // horizontal scroll offset in units

	// Divider drag-resize state (nil colDragCol = the key column).
	// colDragInvert: the divider is sizing the column to its RIGHT
	// (slack lives to the left), so the delta applies negated.
	colDragging   bool
	colDragCol    *TreeColumn
	colDragStartX core.Unit
	colDragStartW core.Unit
	colDragInvert bool

	// Composite fit-mode drag: the grabbed line moves by resizing the
	// two columns astride it against the slack pool (the auto-fill
	// key's spare width when the key shows, else the blank width right
	// of the last column), capped so the layout never starts
	// reclaiming from unrelated columns mid-drag - no other line ever
	// moves contrary to the drag direction. Snapshot widths keep each
	// move idempotent from the press state.
	colDragFit        bool
	colDragSlackRight bool        // pool right of the line (key hidden)
	colDragL          *TreeColumn // nil = the key column (divider 0)
	colDragR          *TreeColumn
	colDragLW         core.Unit
	colDragRW         core.Unit
	colDragPool       core.Unit // slack consumable before reclaim would kick in

	// Horizontal scrollbar (footer row) drag state.
	hbarDragging     bool
	hbarThumbHovered bool // pointer over the footer thumb (hover color)
	hbarDragStartX   core.Unit
	hbarDragStartHS  core.Unit

	// In-place row editing (see treeview_edit.go). The editor is a
	// spun-into-existence TextInput floating over one cell; the tree
	// keeps real focus and forwards input while it is up.
	rowEditing     bool
	editCol        *TreeColumn
	editLastCol    *TreeColumn // resumed on the next edit session
	editItem       *TreeItem
	editOrig       string
	editBox        *TextInput // free-text cells
	editCombo      *ComboBox  // enum cells (closed until Space/click)
	editComboMagic bool       // combo row 0 is the not-in-enum original
	editMouseDown  bool
	keyEditable    bool // SetEditable: the KEY column joins the edit ring
	onCellEdited   func(item *TreeItem, column *TreeColumn, value string)

	// Click-to-edit: a drag-free click on an editable cell of the
	// already-selected row flips straight into edit mode.
	clickEditItem *TreeItem
	clickEditCol  *TreeColumn
	clickEditX    core.Unit
	clickEditY    core.Unit

	// Sort state (visual; the trinket reorders its row list, the app's
	// item order is untouched): sorted=false means unsorted, and
	// sortLevels is the ordered run the comparison walks -- first level
	// decides, the next settles its ties, and so on. The first level is
	// the one the header indicator sits on and the one a header
	// activation cycles: ascending -> descending -> unsorted.
	sorted          bool
	sortLevels      []SortLevel
	onSortRequested func(sorted bool, sortedBy int, descending bool)

	// Column-chooser button/menu state (the [=] in the header corner).
	chooserHovered bool
	chooserOpen    bool
	chooserMenu    *Menu

	// Internal header focus zone: the tree is ONE trinket in the app
	// tab order, but runs its own focus machine (the window title-bar
	// pattern): hzBar lights the whole header as one stop, Enter drills
	// into hzItems (Tab cycles column captions then the chooser), and
	// tabbing past the chooser lands in hzContent - the tree proper.
	headerZone     int // hzContent / hzBar / hzItems
	headerFocusIdx int // index into header stops when hzItems

	// Callbacks
	onCurrentChanged func(item *TreeItem)
	onItemActivated  func(item *TreeItem)
	onItemExpanded   func(item *TreeItem)
	onItemCollapsed  func(item *TreeItem)
}

// NewTreeView creates a new tree view.
func NewTreeView() *TreeView {
	t := &TreeView{
		currentIndex:   -1,
		indentWidth:    2,
		lastClickIndex: -1,
		showKey:        true,
		fitWidth:       true,
	}
	t.TrinketBase = *core.NewTrinketBase()
	t.declareCommands()
	t.Init(t) // Enable polymorphic focus handling
	t.SetFocusPolicy(core.StrongFocus)
	t.SetAccessibleRole(core.RoleTree)
	// A cut cell reads on in place: its tooltip stands exactly where the
	// text is and runs past the boundary that cut it.
	t.SetTooltipSide(core.TooltipOver)
	return t
}

// declareCommands says what this tree can carry out. It is asked again
// whenever the direction changes, because the pair of tree-walk commands it
// answers to is one of the things the direction settles: both meanings of a
// shifted arrow are bound to the key, and declaring one of them is what
// decides which the key reaches (see core.CmdTrinketCollapseLeftOrEnclosing).
func (t *TreeView) declareCommands() {
	// The push that steps ON into the tree, and the one that steps back out:
	// leftward and rightward swap where the tree reads right to left.
	back, on := core.CmdTrinketCollapseLeftOrEnclosing, core.CmdTrinketExpandRightOrDescend
	if core.ChromeMirrored(t) {
		back, on = core.CmdTrinketCollapseRightOrEnclosing, core.CmdTrinketExpandLeftOrDescend
	}
	t.SetCommands(
		core.CmdTrinketItemPrior, core.CmdTrinketItemUp,
		core.CmdTrinketItemNext, core.CmdTrinketItemDown,
		core.CmdTrinketItemLeft, core.CmdTrinketItemRight,
		// Unbound by default: a keymap that wants a left which never
		// collapses maps these instead of the arrows.
		core.CmdTrinketColumnLeft, core.CmdTrinketColumnRight,
		// Newly mappable: before these the only way to sort or reach the
		// column chooser from the keyboard was a walk through the header
		// focus zones. All unbound by default.
		core.CmdTrinketExpandedToggle,
		core.CmdTrinketSortAscending, core.CmdTrinketToggleSortAscending,
		core.CmdTrinketSortDescending, core.CmdTrinketToggleSortDescending,
		core.CmdTrinketSortOff,
		core.CmdTrinketSortModeNext, core.CmdTrinketSortModePrior,
		core.CmdTrinketChooser,
		core.CmdTrinketScrollUp, core.CmdTrinketScrollDown,
		core.CmdTrinketPagePrior, core.CmdTrinketPageNext,
		core.CmdTrinketBeg, core.CmdTrinketEnd,
		// Enter begins the row edit; Space activates, which for a tree means
		// expanding or collapsing the branch.
		core.CmdTrinketEdit, core.CmdTrinketActivate,
		// Minus and Plus collapse and expand WITHOUT walking the tree, which
		// is what separates them from the arrows.
		core.CmdTrinketCollapse, core.CmdTrinketExpand, core.CmdTrinketExpandAll,
		// The classic arrow movement under its own name, which is what the
		// SHIFTED arrows keep doing in an editable grid -- there the plain
		// ones walk the edit-target column instead.
		back, on,
		// The header focus zones and the row editor: Tab walks between the
		// header stops (and between the editor's columns), Escape backs out
		// of whichever of them is up. Neither reaches the content switch,
		// which has no case for either.
		core.CmdFocusNext, core.CmdFocusPrior, core.CmdTrinketCancel,
	)
}

// DirectionChanged is core.DirectionObserver: the direction this tree reads
// has moved, whether it was set here or on something above. Which of the
// shifted arrows' two meanings the tree answers to is derived from it, so it
// says what it can do again.
func (t *TreeView) DirectionChanged() {
	t.declareCommands()
}

// AddRootItem adds a root item to the tree.
func (t *TreeView) AddRootItem(item *TreeItem) {
	item.Parent = nil
	t.rootItems = append(t.rootItems, item)
	t.moved()
	if t.currentIndex < 0 && t.rowCount() > 0 {
		t.SetCurrentIndex(0)
	}
	t.Update()
}

// RemoveRootItem removes a root item from the tree.
func (t *TreeView) RemoveRootItem(item *TreeItem) {
	for i, r := range t.rootItems {
		if r == item {
			t.rootItems = append(t.rootItems[:i], t.rootItems[i+1:]...)
			break
		}
	}
	t.moved()
	if t.currentIndex >= t.rowCount() {
		t.currentIndex = t.rowCount() - 1
	}
	t.Update()
}

// RemoveItem removes an item from wherever it sits -- a root item, or a child
// of another item -- and rebuilds what the tree draws.
func (t *TreeView) RemoveItem(item *TreeItem) {
	if item == nil {
		return
	}
	if item.Parent == nil {
		t.RemoveRootItem(item)
		return
	}
	item.Parent.RemoveChild(item)
	t.moved()
	if t.currentIndex >= t.rowCount() {
		t.currentIndex = t.rowCount() - 1
	}
	t.Update()
}

// Clear removes all items.
func (t *TreeView) Clear() {
	t.rootItems = nil
	t.bones = spine{}
	t.currentIndex = -1
	t.scrollOffset = 0
	t.Update()
}

// RootItems returns all root items: the ones a tree was given, or what stands at
// the top of a DECLARED source.
//
// The two are kept apart, so a tree handed a source and then handed nil gets its
// own items back untouched. Which one this answers is which one the tree is
// reading.
func (t *TreeView) RootItems() []*TreeItem {
	if t.source != nil {
		return t.fromTop
	}
	return t.rootItems
}

// CurrentItem returns the currently focused item.
func (t *TreeView) CurrentItem() *TreeItem {
	if t.currentIndex >= 0 && t.currentIndex < t.rowCount() {
		return t.rowAt(t.currentIndex)
	}
	// **A row the view cannot place is still the row that was chosen**, and the item
	// is what every caller here holds -- a handler, the row editor, a selection
	// restored after a resort. Answering nil because the position is unknown would
	// report a scroll as a deselection.
	if t.chosen == nil {
		return nil
	}
	if t.source != nil {
		return t.fromSource[serval.Key(t.chosen)]
	}
	if t.chosen.IsInt {
		return t.byID[objectIDOf(t.chosen)]
	}
	return nil
}

// SetCurrentItem sets the current item.
//
// An item the view is not HOLDING cannot be found, which is a row somebody
// scrolled past rather than a row that is not there. Where it is a source's row
// the spine knows it by identity, and that is asked first -- so naming a row keeps
// working for as far as the view remembers, rather than only for what is on
// screen.
func (t *TreeView) SetCurrentItem(item *TreeItem) {
	if at, ok := t.positionOf(item); ok {
		t.SetCurrentIndex(at)
	}
}

// positionOf is where an item stands, and false for one outside what the view
// holds.
func (t *TreeView) positionOf(item *TreeItem) (int, bool) {
	if item == nil {
		return 0, false
	}
	if item.rowKey != nil {
		return t.bones.posOf(item.rowKey)
	}
	// A made row's identity is its ObjectID, which is what makeSource keyed it by.
	return t.bones.posOf(treeKey(item.ID))
}

// CurrentIndex is where the chosen row stands, and -1 for one the view cannot
// place.
//
// **-1 does not mean nothing is chosen.** A row scrolled past, or moved outside the
// window by a reorder, is still chosen and is still what CurrentItem answers; what
// is not known is where it stands. A caller that wants to know whether anything is
// chosen asks CurrentItem.
func (t *TreeView) CurrentIndex() int {
	return t.currentIndex
}

// SetCurrentIndex sets the current index.
func (t *TreeView) SetCurrentIndex(index int) {
	if index < -1 || index >= t.rowCount() {
		return
	}
	if t.currentIndex == index {
		return
	}

	t.currentIndex = index
	// The identity is the authority, so it is taken at the same moment. A blank has
	// none to take, and resolve fills it in when the record arrives.
	t.chosen = nil
	if index >= 0 {
		if id, held := t.bones.idAt(index); held {
			t.chosen = id
		}
	}
	t.ensureVisible(index)
	t.Update()

	// Notify parent scroll containers to scroll this item into view.
	// This is needed for keyboard navigation when the TreeView is inside
	// a ScrollArea and the selected item moves outside the visible area.
	// For mouse clicks, SetFocusWithoutScroll() prevents unwanted scrolling.
	if sp, ok := t.treeHostSpan(); ok && index >= 0 {
		metrics := t.EffectiveCellMetrics()
		cw := metrics.UnitsPerCellWidth
		item := t.drawRow(index)

		// What has to be in view is the row's own content: one column of
		// indent still showing, then the expander and the caption behind
		// it, measured along the run and placed where it is drawn.
		at := core.Unit(item.Level()*t.indentWidth+treeLeftPadCells) * cw
		if at > 0 {
			at -= cw
		}
		w := 2*cw + t.MeasureText(t.CellRun(item.Text))
		if room := sp.w - at; w > room {
			w = room
		}

		// The visual Y position of this item, after the internal scroll.
		itemY := t.rowsTop() + core.Unit(index-t.scrollOffset)*metrics.UnitsPerCellHeight

		t.ScrollRectIntoView(core.UnitRect{
			X:      t.treeRunX(sp, at, w),
			Y:      itemY,
			Width:  w,
			Height: metrics.UnitsPerCellHeight,
		})
	}

	// Announce selection change for accessibility
	if index >= 0 && index < t.rowCount() {
		if am := core.FindAccessibilityManager(t); am != nil {
			item := t.drawRow(index)
			state := ""
			if !item.IsLeaf() {
				if item.Expanded {
					state = ", expanded"
				} else {
					state = ", collapsed"
				}
			}
			am.AnnouncePolite(fmt.Sprintf("%s, tree item%s, level %d", item.Text, state, item.Level()+1))
		}
	}

	// A blank row names nothing, so a handler expecting a row is told nothing
	// rather than told about a placeholder. resolve tells it when the record lands.
	if t.onCurrentChanged != nil && index >= 0 {
		if item := t.rowAt(index); item != nil {
			t.onCurrentChanged(item)
		}
	}
}

// collapseOrEnclosing closes an open item, or climbs to the one enclosing it:
// a step BACK along the tree, whichever arrow asked for it.
func (t *TreeView) collapseOrEnclosing(current *TreeItem) bool {
	if current != nil {
		if current.Expanded && !current.IsLeaf() {
			t.CollapseItem(current)
		} else if current.Parent != nil {
			t.SetCurrentItem(current.Parent)
		}
	}
	return true
}

// expandOrDescend opens a closed item, or steps into the one already open: a
// step ON along the tree.
func (t *TreeView) expandOrDescend(current *TreeItem) bool {
	if current != nil {
		if !current.Expanded && !current.IsLeaf() {
			t.ExpandItem(current)
		} else if current.Expanded && len(current.Children) > 0 {
			t.SetCurrentItem(current.Children[0])
		}
	}
	return true
}

// ExpandItem expands an item to show its children.
//
// **What it costs is a SHIFT and not an invalidation.** Every level of a tree is a
// data set of its own, so opening a node makes no record untrue and reorders
// nothing: it changes which levels the walk visits, and so where the rows below
// this one stand. `opened` moves what the view holds by that much where it can say
// how much, and forgets from here DOWN where it cannot -- nothing above this row
// moved, whatever happens beneath it.
//
// Which is also why the twisty flips and the blank rows appear at once, before any
// answer: laying out `Kids` rows the instant the mark moves IS the shift.
func (t *TreeView) ExpandItem(item *TreeItem) {
	if item.IsLeaf() || item.Expanded {
		return
	}

	// Save currently selected item
	var selectedItem *TreeItem
	if t.currentIndex >= 0 && t.currentIndex < t.rowCount() {
		selectedItem = t.rowAt(t.currentIndex)
	}
	at, known := t.positionOf(item)

	// The marks where a source was declared, the field where the tree made its
	// own -- one mechanism said from two sides, because a declared source has no
	// items to carry a field in from.
	if !t.tellMarks(item, true) {
		item.Expanded = true
		t.moved() // the items themselves changed, so the made source is remade
	} else if known {
		t.opened(at, item)
		t.window(t.asking())
		t.clampScrollOffset()
	} else {
		// A row the view is not holding: there is no position to shift from, so
		// there is nothing to keep.
		t.moved()
	}

	// Restore selection by finding the same item in new flat list
	t.restoreSelectionByItem(selectedItem)
	t.Update()

	// Announce expansion for accessibility
	if am := core.FindAccessibilityManager(t); am != nil {
		am.AnnouncePolite(fmt.Sprintf("%s, expanded", item.Text))
	}

	if t.onItemExpanded != nil {
		t.onItemExpanded(item)
	}
}

// CollapseItem collapses an item to hide its children.
func (t *TreeView) CollapseItem(item *TreeItem) {
	if !item.Expanded {
		return
	}

	// Save currently selected item
	var selectedItem *TreeItem
	if t.currentIndex >= 0 && t.currentIndex < t.rowCount() {
		selectedItem = t.rowAt(t.currentIndex)
	}
	at, known := t.positionOf(item)

	if !t.tellMarks(item, false) {
		item.Expanded = false
		t.moved()
	} else if known {
		// Closing is the same move the other way about, and it is EXACT wherever
		// the view holds the end of the subtree -- the rows going are the ones in
		// hand. See closing.
		t.closed(at)
		t.window(t.asking())
		t.clampScrollOffset()
	} else {
		t.moved()
	}

	// Restore selection by finding the same item in new flat list
	// If selected item is no longer visible (was in collapsed subtree),
	// select the item that was collapsed
	if !t.restoreSelectionByItem(selectedItem) {
		// Selected item no longer visible, select the collapsed item
		t.restoreSelectionByItem(item)
	}
	t.Update()

	// Announce collapse for accessibility
	if am := core.FindAccessibilityManager(t); am != nil {
		am.AnnouncePolite(fmt.Sprintf("%s, collapsed", item.Text))
	}

	if t.onItemCollapsed != nil {
		t.onItemCollapsed(item)
	}
}

// restoreSelectionByItem chooses the given item and reports whether the view could
// say where it stands.
//
// **It chooses either way.** A row a reorder moved outside the window is still the
// row the reader chose, and forgetting it because the view cannot currently place it
// would lose a selection to a scroll. False says the index is unknown, not that the
// selection is gone -- so a caller with a fallback row still has one, and one
// without simply waits for resolve.
func (t *TreeView) restoreSelectionByItem(item *TreeItem) bool {
	if item == nil {
		return false
	}
	t.chosen = identityOf(item)
	at, ok := t.positionOf(item)
	if !ok {
		t.currentIndex = -1
		return false
	}
	t.currentIndex = at
	return true
}

// resolve puts the index back where the chosen row has turned up again.
//
// Called when a window lands, which is the moment the answer to "where is it now"
// can have changed. A row that is still nowhere the spine can find leaves the index
// at -1 and the selection where it was: held, and waiting.
func (t *TreeView) resolve() {
	if t.chosen == nil {
		return
	}
	if at, held := t.bones.posOf(t.chosen); held {
		t.currentIndex = at
		return
	}
	t.currentIndex = -1
}

// identityOf is a row's identity: the source's where it came from one, and the
// item's own ObjectID for a row the tree made -- which is what makeSource keyed it
// by, so one question answers both shapes.
func identityOf(item *TreeItem) *serval.Value {
	if item == nil {
		return nil
	}
	if item.rowKey != nil {
		return item.rowKey
	}
	return treeKey(item.ID)
}

// movingFrom is the position a movement key starts from.
//
// **Three states here and not two**, which is the whole reason the identity is kept
// apart from the index.
//
//	placed        start from where it stands
//	chosen, not   still a selection, so a movement key cannot treat it as none --
//	placed        and the only honest position for it is where the reader is
//	              LOOKING. Starting from nought would answer a press of Down by
//	              jumping to the top of the sequence.
//	nothing       before the first row, so Down chooses the first one, which is
//	chosen        what it has always done.
func (t *TreeView) movingFrom() int {
	switch {
	case t.currentIndex >= 0:
		return t.currentIndex
	case t.chosen != nil:
		return t.scrollOffset
	}
	return -1
}

// ToggleItem toggles the expanded state of an item.
func (t *TreeView) ToggleItem(item *TreeItem) {
	if item.Expanded {
		t.CollapseItem(item)
	} else {
		t.ExpandItem(item)
	}
}

// ExpandAll expands all items.
func (t *TreeView) ExpandAll() {
	// Save currently selected item
	var selectedItem *TreeItem
	if t.currentIndex >= 0 && t.currentIndex < t.rowCount() {
		selectedItem = t.rowAt(t.currentIndex)
	}

	// One mark for a declared source, which is the whole point of `openAll`
	// being a state: expanding a million rows costs one entry.
	if src := t.marks(); src != nil {
		src.ExpandAll()
	} else {
		t.expandRecursive(t.rootItems)
	}
	t.moved()

	// Restore selection by finding the same item
	t.restoreSelectionByItem(selectedItem)
	t.Update()
}

func (t *TreeView) expandRecursive(items []*TreeItem) {
	for _, item := range items {
		item.Expanded = true
		t.expandRecursive(item.Children)
	}
}

// CollapseAll collapses all items.
func (t *TreeView) CollapseAll() {
	// Save currently selected item
	var selectedItem *TreeItem
	if t.currentIndex >= 0 && t.currentIndex < t.rowCount() {
		selectedItem = t.rowAt(t.currentIndex)
	}

	if src := t.marks(); src != nil {
		src.CollapseAll()
	} else {
		t.collapseRecursive(t.rootItems)
	}
	t.moved()

	// Restore selection - if item is no longer visible, select first root
	if !t.restoreSelectionByItem(selectedItem) && t.rowCount() > 0 {
		t.currentIndex = 0
	}
	t.Update()
}

func (t *TreeView) collapseRecursive(items []*TreeItem) {
	for _, item := range items {
		item.Expanded = false
		t.collapseRecursive(item.Children)
	}
}

// SetIndentWidth sets the indent width per level.
func (t *TreeView) SetIndentWidth(width int) {
	t.indentWidth = width
	t.Update()
}

// SetOnCurrentChanged sets the current changed callback.
func (t *TreeView) SetOnCurrentChanged(handler func(item *TreeItem)) {
	t.onCurrentChanged = handler
}

// SetOnItemActivated sets the item activated callback.
func (t *TreeView) SetOnItemActivated(handler func(item *TreeItem)) {
	t.onItemActivated = handler
}

// SetOnItemExpanded sets the item expanded callback.
func (t *TreeView) SetOnItemExpanded(handler func(item *TreeItem)) {
	t.onItemExpanded = handler
}

// SetOnItemCollapsed sets the item collapsed callback.
func (t *TreeView) SetOnItemCollapsed(handler func(item *TreeItem)) {
	t.onItemCollapsed = handler
}

// SetBounds resizes the tree and re-clamps its scroll state (the
// embedded base cannot dispatch HandleResize to us - the ScrollArea
// override pattern).
func (t *TreeView) SetBounds(bounds core.UnitRect) {
	old := t.Bounds().Size()
	t.TrinketBase.SetBounds(bounds)
	if old != bounds.Size() {
		t.HandleResize(old, bounds.Size())
	}
}

// HandleResize re-clamps the scroll state: growing the view while
// scrolled down must pull the content back into the freed space (the
// scrollbar vanishes WITH the blank it would explain), and a stale
// horizontal pan snaps back the same way.
func (t *TreeView) HandleResize(oldSize, newSize core.UnitSize) {
	t.clampScrollOffset()
	if t.hScroll > 0 {
		if lay := t.columnLayout(); t.hScroll > lay.maxHScroll {
			t.hScroll = lay.maxHScroll
		}
	}
	t.Update()
}

// clampScrollOffset ensures scrollOffset is within valid bounds.
func (t *TreeView) clampScrollOffset() {
	if t.rowCount() == 0 {
		t.scrollOffset = 0
		return
	}

	visibleCount := t.visibleCount()
	maxScroll := t.rowCount() - visibleCount
	if maxScroll < 0 {
		maxScroll = 0
	}
	if t.scrollOffset > maxScroll {
		t.scrollOffset = maxScroll
	}
	if t.scrollOffset < 0 {
		t.scrollOffset = 0
	}
}

// ensureVisible ensures the given index is visible. Degenerate bounds
// (zero height, e.g. before the first layout) are left alone - there
// is no viewport to scroll into yet, and adjusting would push a bogus
// offset that survives the real layout.
func (t *TreeView) ensureVisible(index int) {
	if index < 0 {
		return
	}
	visibleCount := t.visibleCount()
	if visibleCount <= 0 {
		return
	}
	if index < t.scrollOffset {
		t.scrollOffset = index
	} else if index >= t.scrollOffset+visibleCount {
		t.scrollOffset = index - visibleCount + 1
	}
}

// SizeHint returns the size a tree asks for when nothing sets one (see
// defaultSizeCells).
func (t *TreeView) SizeHint() core.UnitSize {
	metrics := t.EffectiveCellMetrics()
	return core.UnitSize{
		Width:  metrics.UnitsPerCellWidth * defaultTreeWidthCells,
		Height: metrics.UnitsPerCellHeight * defaultSizeCells,
	}
}

// Paint renders the tree view.
func (t *TreeView) Paint(p *core.Painter) {
	if t.multiColumn() {
		t.paintMulti(p)
		return
	}
	bounds := t.Bounds()
	scheme := t.GetScheme()
	focused := t.HasFocus()
	metrics := t.EffectiveCellMetrics()

	// Draw background using list colors
	bgStyle := style.DefaultStyle().WithFg(scheme.GetListFG()).WithBg(scheme.GetListBG())
	p.FillRect(core.UnitRect{Width: bounds.Width, Height: bounds.Height}, ' ', bgStyle)

	// Asked once, and before anything is MEASURED: rowCount makes sure the window is
	// there, and the answer can change what there is to measure -- a refusal takes a
	// row out of the rows' own area, and a frame that measured first would draw
	// itself as though nothing had been refused. Asking per row would run the
	// spine's scan down the whole viewport for an answer that cannot change while
	// this paint is being drawn.
	drawn := t.rowCount()

	// A refusal is drawn FIRST and takes its row out of what the rows have: it is
	// not an overlay, it stands where a row would have stood. See trouble.go.
	top := t.rowsTop()
	if h := t.troubleHeight(metrics); h > 0 {
		paintTrouble(p, &t.TrinketBase, scheme,
			troubleRow(core.UnitRect{
				Y:      t.headerHeight(),
				Width:  bounds.Width,
				Height: bounds.Height - t.headerHeight(),
			}, h),
			t.trouble.Reason)
	}

	visibleCount := t.visibleCount()

	// GUI: paint one extra partial row into any leftover strip rather
	// than leaving it blank (never counted as visible for scrolling).
	rows := visibleCount
	if p.Graphical() && t.scrollOffset+visibleCount < drawn &&
		top+core.Unit(visibleCount)*metrics.UnitsPerCellHeight < bounds.Height {
		rows++
	}

	// Draw items
	for i := 0; i < rows; i++ {
		itemIndex := t.scrollOffset + i
		if itemIndex >= drawn {
			break
		}

		item := t.drawRow(itemIndex)
		itemY := top + core.Unit(i)*metrics.UnitsPerCellHeight

		// Determine style
		var s style.CellStyle
		if !item.Enabled {
			s = style.DefaultStyle().WithFg(scheme.GetDisabledTextFG()).WithBg(scheme.GetListBG())
		} else if itemIndex == t.currentIndex {
			if focused {
				s = scheme.GetFocusedListItem()
			} else {
				s = scheme.GetSelectedListItem()
			}
		} else if t.ledger {
			// Ledger banding (non-selected rows only), 1-based: the
			// first row is odd.
			if itemIndex%2 == 0 {
				s = scheme.GetLedgerOdd()
			} else {
				s = scheme.GetLedgerEven()
			}
		} else {
			// Unselected items
			s = style.DefaultStyle().WithFg(scheme.GetListFG()).WithBg(scheme.GetListBG())
		}

		// Draw row background
		p.FillRect(core.UnitRect{
			X:      0,
			Y:      itemY,
			Width:  bounds.Width,
			Height: metrics.UnitsPerCellHeight,
		}, ' ', s)

		// One tree cell across the row, beside the scrollbar's lane: the
		// same apparatus the multi-column presentation draws in its host
		// span, over a span the width of the content.
		t.paintTreeCell(p, item, t.rowSpan(), itemY, s, s, metrics, t.EffectiveFont(), item.Text)
	}

	// Draw scrollbar if needed
	if t.rowCount() > visibleCount {
		t.paintScrollbar(p, visibleCount)
	}
}

// visibleCount returns the number of visible content rows (the header
// row and the horizontal-scrollbar footer row are not content rows).
// Never negative: squeezed below its own chrome the tree simply has
// zero content rows (a negative count reaches make() in the paint
// path and panics).
func (t *TreeView) visibleCount() int {
	bounds := t.Bounds()
	metrics := t.EffectiveCellMetrics()
	n := int((bounds.Height - t.rowsTop() - t.footerHeight()) / metrics.UnitsPerCellHeight)
	if n < 0 {
		n = 0
	}
	return n
}

// rowsTop is where the rows begin: under the header where there is one, and under the
// refusal line where there is one of those (see trouble.go).
//
// A refusal stands BELOW the header and above the rows: the header says what the
// columns are, which is still true, and the line says what the rows are not, which is
// what stands in their place. Everything about the rows is reckoned from here, so a
// refusal appearing moves them down and takes one off the end rather than covering the
// first one over.
func (t *TreeView) rowsTop() core.Unit {
	return t.headerHeight() + t.troubleHeight(t.EffectiveCellMetrics())
}

// rowUnder is the visible row a tree-local y lands on, counted from the rows' own top
// edge, and -1 where it lands on the header or the refusal line above them.
func (t *TreeView) rowUnder(y core.Unit) int {
	metrics := t.EffectiveCellMetrics()
	if metrics.UnitsPerCellHeight <= 0 {
		return -1
	}
	if y -= t.rowsTop(); y < 0 {
		return -1
	}
	return int(y / metrics.UnitsPerCellHeight)
}

// treeHostSpan is the span the tree apparatus is drawn in: the key column's,
// or - with the key hidden - the first visible data column's, and the whole
// row in the single-column presentation. ok=false when the host has been
// panned out of sight, where there is nothing of the tree to hit.
func (t *TreeView) treeHostSpan() (colSpan, bool) {
	if !t.multiColumn() {
		return t.rowSpan(), true
	}
	lay := t.columnLayout()
	host := t.treeHostColumn()
	for _, sp := range lay.spans {
		if sp.col == nil || (host != nil && sp.col == host) {
			return sp, true
		}
	}
	return colSpan{}, false
}

// rowSpan is the single-column presentation's one span: the whole row, less
// the scrollbar's lane when there is a scrollbar, so a caption never runs
// under the bar.
func (t *TreeView) rowSpan() colSpan {
	w := t.Bounds().Width
	if t.rowCount() > t.visibleCount() {
		w -= t.EffectiveCellMetrics().UnitsPerCellWidth
	}
	if w < 0 {
		w = 0
	}
	return colSpan{x: core.LeadingX(t, t.Bounds().Width, 0, w), w: w, divX: -1}
}

// laneX is the column the vertical scrollbar stands in: the TRAILING edge,
// which is the near side of the screen where the tree reads right to left.
// The column band starts on the other side of it (see columnLayout).
func (t *TreeView) laneX() core.Unit {
	w := t.Bounds().Width
	lane := t.EffectiveCellMetrics().UnitsPerCellWidth
	return core.LeadingX(t, w, w-lane, lane)
}

// onLane reports whether a tree-local x is in the scrollbar's column. The lane
// is one column wherever it stands, so what puts a press on it is being IN the
// column rather than past its near edge.
func (t *TreeView) onLane(x core.Unit) bool {
	at := t.laneX()
	return x >= at && x < at+t.EffectiveCellMetrics().UnitsPerCellWidth
}

// scrollbarGeometry returns scrollbar dimensions and thumb position.
// Returns: scrollbarX, thumbStart, thumbHeight, trackHeight (all in rows)
func (t *TreeView) scrollbarGeometry(visibleCount int) (scrollbarX core.Unit, thumbStart, thumbHeight, trackHeight int) {
	totalItems := t.rowCount()

	scrollbarX = t.laneX()
	trackHeight = visibleCount

	if totalItems <= visibleCount {
		// No scrolling needed - thumb fills track
		thumbStart = 0
		thumbHeight = trackHeight
		return
	}

	// Calculate thumb height - proportional to visible/total, minimum 1 row
	thumbHeight = visibleCount * visibleCount / totalItems
	if thumbHeight < 1 {
		thumbHeight = 1
	}

	// Calculate thumb position
	// The thumb should only be at position 0 when scrollOffset is 0
	// The thumb should only be at the bottom when scrollOffset is at max
	maxScroll := totalItems - visibleCount
	scrollableTrack := trackHeight - thumbHeight

	if maxScroll > 0 && scrollableTrack > 0 {
		// Map scroll position to thumb position, ensuring extremes are only at extremes
		thumbStart = t.scrollOffset * scrollableTrack / maxScroll

		// Ensure thumb doesn't go to extremes unless scroll is at extremes
		if t.scrollOffset > 0 && thumbStart == 0 {
			thumbStart = 1
		}
		if t.scrollOffset < maxScroll && thumbStart >= scrollableTrack {
			thumbStart = scrollableTrack - 1
		}
	}

	return
}

// scrollbarUnits returns the scrollbar track length, thumb length,
// and thumb origin in units for pixel surfaces - the same proportions
// as scrollbarGeometry without row quantization. Mid-drag the thumb
// origin is the smooth (pointer-tracked) position.
func (t *TreeView) scrollbarUnits(visibleCount int) (trackU, thumbU, posU float64) {
	metrics := t.EffectiveCellMetrics()
	trackU = float64(core.Unit(visibleCount) * metrics.UnitsPerCellHeight)
	totalItems := t.rowCount()
	if totalItems <= visibleCount || visibleCount <= 0 {
		return trackU, trackU, 0
	}
	thumbU = trackU * float64(visibleCount) / float64(totalItems)
	if thumbU < 8 {
		thumbU = 8
	}
	if thumbU > trackU {
		thumbU = trackU
	}
	scrollable := trackU - thumbU
	maxScroll := totalItems - visibleCount
	if t.scrollbarDragging && t.smoothScrollbarDrag {
		posU = t.scrollbarThumbPos
	} else if maxScroll > 0 {
		posU = float64(t.scrollOffset) * scrollable / float64(maxScroll)
	}
	if posU < 0 {
		posU = 0
	}
	if posU > scrollable {
		posU = scrollable
	}
	return trackU, thumbU, posU
}

// paintScrollbar draws a vertical scrollbar.
func (t *TreeView) paintScrollbar(p *core.Painter, visibleCount int) {
	scheme := t.GetScheme()
	metrics := t.EffectiveCellMetrics()
	trackStyle := scheme.GetScrollbar()
	thumbStyle := scheme.GetScrollbarThumbState(false, t.scrollbarThumbHovered && p.Graphical())

	// Pixel surfaces: a single hairline stripe blended at 50%
	// opacity behind, and one solid full-opacity rectangle for the
	// thumb, at unit granularity - same treatment as the combobox
	// popup lane.
	// The track runs beside the ROWS, so it starts below the header row (when one is
	// shown) and below a refusal line (when there is one).
	headerH := t.rowsTop()

	if p.Graphical() {
		// No track stripe: the hairline reads as another column
		// divider next to the real ones. The bare thumb is the bar.
		_, thumbU, posU := t.scrollbarUnits(visibleCount)
		p.FillRect(core.UnitRect{
			X:      t.laneX() + 1,
			Y:      headerH + core.Unit(posU+0.5),
			Width:  metrics.UnitsPerCellWidth - 2,
			Height: core.Unit(thumbU + 0.5),
		}, ' ', thumbStyle.WithBg(thumbStyle.Fg))
		return
	}

	scrollbarX, thumbStart, thumbHeight, trackHeight := t.scrollbarGeometry(visibleCount)

	// Draw scrollbar track (the ScrollArea's shaded fill, not a line).
	for i := 0; i < trackHeight; i++ {
		y := headerH + core.Unit(i)*metrics.UnitsPerCellHeight
		p.DrawCell(scrollbarX, y, '░', trackStyle)
	}

	// Draw scrollbar thumb
	for i := 0; i < thumbHeight; i++ {
		y := headerH + core.Unit(thumbStart+i)*metrics.UnitsPerCellHeight
		p.DrawCell(scrollbarX, y, '█', thumbStyle)
	}
}

// HandleKeyPress handles keyboard input.
func (t *TreeView) HandleKeyPress(event core.KeyPressEvent) bool {
	// Resolved ONCE and handed down: this trinket asks in five places (the
	// chooser, the row editor, the header zones, the edit target, and its own
	// switch below) and feeding the sequence processor the same keystroke
	// five times would advance a chord's prefix by five.
	cmd := t.KeyCommand(event.Key)

	// The open column-chooser menu takes keys first (the tree retains
	// focus and forwards - the menu bar pattern).
	if t.handleChooserKey(event, cmd) {
		return true
	}
	// The open row editor takes everything next (see treeview_edit.go).
	if t.handleEditKey(event, cmd) {
		return true
	}
	// The internal header focus zone consumes its navigation (including
	// the content zone's S-Tab back into the bar) before content keys.
	if t.handleHeaderFocusKey(cmd) {
		return true
	}
	// In an editable grid, Left/Right rotate the Enter-target column
	// (the FocusedListItem cell) without opening the editor.
	if t.handleEditTargetKey(cmd) {
		return true
	}

	current := t.CurrentItem()
	// Where a movement starts, which is not always where the selection is: a row the
	// view cannot place still needs Down to mean the row below the reader. See
	// movingFrom.
	from := t.movingFrom()

	switch cmd {
	case core.CmdTrinketItemPrior, core.CmdTrinketItemUp:
		if from > 0 {
			t.SetCurrentIndex(from - 1)
		}
		return true

	case core.CmdTrinketScrollUp:
		// Jump by 5 items, scrolling to maintain relative position
		if from > 0 {
			delta := 5
			newIndex := from - delta
			if newIndex < 0 {
				newIndex = 0
			}
			actualDelta := from - newIndex
			// Scroll by same amount to maintain relative position
			newScroll := t.scrollOffset - actualDelta
			if newScroll < 0 {
				newScroll = 0
			}
			t.scrollOffset = newScroll
			t.SetCurrentIndex(newIndex)
		}
		return true

	case core.CmdTrinketItemNext, core.CmdTrinketItemDown:
		if from < t.rowCount()-1 {
			t.SetCurrentIndex(from + 1)
		}
		return true

	case core.CmdTrinketScrollDown:
		// Jump by 5 items, scrolling to maintain relative position
		if from < t.rowCount()-1 {
			delta := 5
			newIndex := from + delta
			if newIndex >= t.rowCount() {
				newIndex = t.rowCount() - 1
			}
			actualDelta := newIndex - from
			// Scroll by same amount to maintain relative position
			visibleCount := t.visibleCount()
			maxScroll := t.rowCount() - visibleCount
			if maxScroll < 0 {
				maxScroll = 0
			}
			newScroll := t.scrollOffset + actualDelta
			if newScroll > maxScroll {
				newScroll = maxScroll
			}
			t.scrollOffset = newScroll
			t.SetCurrentIndex(newIndex)
		}
		return true

	// Shift+Left/Right always mean the classic tree navigation, even
	// on editable grids where the plain arrows rotate the Enter-target
	// column (handleEditTargetKey lets shifted arrows through).
	case core.CmdTrinketExpandedToggle:
		if current != nil && !current.IsLeaf() {
			t.ToggleItem(current)
			return true
		}
		return false

	case core.CmdTrinketSortAscending:
		return t.SortAscending()
	case core.CmdTrinketSortDescending:
		return t.SortDescending()
	case core.CmdTrinketSortOff:
		return t.SortOff()
	case core.CmdTrinketToggleSortAscending:
		return t.ToggleSortAscending()
	case core.CmdTrinketToggleSortDescending:
		return t.ToggleSortDescending()
	case core.CmdTrinketSortModeNext:
		return t.SortModeNext()
	case core.CmdTrinketSortModePrior:
		return t.SortModePrior()
	case core.CmdTrinketChooser:
		return t.OpenColumnChooser()

	case core.CmdTrinketColumnLeft:
		return t.moveEnterTargetColumn(t.arrowStep(-1))

	case core.CmdTrinketColumnRight:
		return t.moveEnterTargetColumn(t.arrowStep(1))

	// The named commands say the ACT, so they mean the same thing whichever
	// way the tree reads. The arrows name a side of the screen, and a tree
	// grows away from the edge it reads from -- so which of the two acts an
	// arrow asks for is the direction's answer. item_left and item_right are
	// also the grid's column walk, which handleEditTargetKey took above; what
	// reaches here is the classic movement.
	case core.CmdTrinketCollapseLeftOrEnclosing, core.CmdTrinketCollapseRightOrEnclosing:
		return t.collapseOrEnclosing(current)

	case core.CmdTrinketExpandLeftOrDescend, core.CmdTrinketExpandRightOrDescend:
		return t.expandOrDescend(current)

	case core.CmdTrinketItemLeft:
		if core.ChromeMirrored(t) {
			return t.expandOrDescend(current)
		}
		return t.collapseOrEnclosing(current)

	case core.CmdTrinketItemRight:
		if core.ChromeMirrored(t) {
			return t.collapseOrEnclosing(current)
		}
		return t.expandOrDescend(current)

	case core.CmdTrinketBeg:
		if t.rowCount() > 0 {
			t.SetCurrentIndex(0)
		}
		return true

	case core.CmdTrinketEnd:
		if t.rowCount() > 0 {
			t.SetCurrentIndex(t.rowCount() - 1)
		}
		return true

	case core.CmdTrinketPagePrior:
		pageSize := t.visibleCount()
		newIndex := t.movingFrom() - pageSize
		if newIndex < 0 {
			newIndex = 0
		}
		t.SetCurrentIndex(newIndex)
		return true

	case core.CmdTrinketPageNext:
		pageSize := t.visibleCount()
		newIndex := t.movingFrom() + pageSize
		if newIndex >= t.rowCount() {
			newIndex = t.rowCount() - 1
		}
		t.SetCurrentIndex(newIndex)
		return true

	case core.CmdTrinketEdit:
		// With editable columns, Enter opens the in-place row editor;
		// without any, it behaves exactly like Space.
		if t.startRowEdit() {
			// Entering edit DIRECTLY on a choice cell pops its
			// drop-down - the arrowed target advertised a picker and
			// Enter accepted the offer. (Tabbing to a choice cell
			// from another column stays closed.)
			if t.editCombo != nil {
				t.editCombo.OpenFromKeyboard()
			}
			return true
		}
		if current != nil {
			if !current.IsLeaf() {
				t.ToggleItem(current)
			}
			if t.onItemActivated != nil {
				t.onItemActivated(current)
			}
		}
		return true

	case core.CmdTrinketActivate:
		// On a CHOICE Enter-target, Space enters edit and pops the
		// drop-down (a text target keeps Space's classic toggle -
		// Space never begins a text edit).
		if col := t.enterTargetColumn(); col != nil && col != treeKeyColumn &&
			len(col.Enum) > 0 && t.headerZone == hzContent && t.startRowEdit() {
			if t.editCombo != nil {
				t.editCombo.OpenFromKeyboard()
			}
			return true
		}
		if current != nil {
			if !current.IsLeaf() {
				t.ToggleItem(current)
			}
			if t.onItemActivated != nil {
				t.onItemActivated(current)
			}
		}
		return true

	case core.CmdTrinketExpandAll:
		// Expand all
		t.ExpandAll()
		return true

	case core.CmdTrinketCollapse:
		// Collapse current
		if current != nil && !current.IsLeaf() {
			t.CollapseItem(current)
		}
		return true

	case core.CmdTrinketExpand:
		// Expand current
		if current != nil && !current.IsLeaf() {
			t.ExpandItem(current)
		}
		return true
	}

	return false
}

// HandleMousePress handles mouse clicks.
func (t *TreeView) HandleMousePress(event core.MousePressEvent) bool {
	// A right-click inside the open row editor belongs to the editor:
	// it opens the TextInput's own context menu (Cut/Copy/Paste/...).
	if event.Button == core.RightButton && t.rowEditing && t.editBox != nil {
		if r, ok := t.editorRect(); ok &&
			event.X >= r.X && event.X < r.X+r.Width &&
			event.Y >= r.Y && event.Y < r.Y+r.Height {
			ev := event
			ev.X -= r.X
			ev.Y -= r.Y
			return t.editBox.HandleMousePress(ev)
		}
	}
	if event.Button != core.LeftButton {
		return false
	}

	// Clear any stale drag state from previous incomplete drags
	t.isDragging = false
	t.scrollbarDragging = false

	// Check if click is within our bounds
	bounds := t.Bounds()
	if event.X < 0 || event.Y < 0 || event.X >= bounds.Width || event.Y >= bounds.Height {
		return false
	}

	t.SetFocusWithoutScroll() // Use without-scroll variant since click proves visibility
	metrics := t.EffectiveCellMetrics()

	// Row editor: a press inside it edits text; a press anywhere else
	// ACCEPTS the value and the click proceeds. Also note whether this
	// press is a click-to-edit candidate (an editable cell of the
	// already-selected row).
	if t.handleEditMousePress(event) {
		return true
	}
	t.noteClickEditPress(event)

	// Header band: chooser button and divider drags (multi-column).
	if t.handleMultiPress(event) {
		return true
	}
	// Footer band: the reserved horizontal-scrollbar row.
	if t.handleHBarPress(event) {
		return true
	}
	contentY := event.Y - t.rowsTop()

	// The refusal line stands between the header and the rows: it is something to
	// READ, so a press on it is neither a press on a row nor one on the bar.
	if event.Y >= t.headerHeight() && contentY < 0 {
		return false
	}

	// Check if click is on scrollbar
	_, thumbStart, thumbHeight, _ := t.scrollbarGeometry(t.visibleCount())
	if t.onLane(event.X) && t.rowCount() > t.visibleCount() {
		clickedRow := int(contentY / metrics.UnitsPerCellHeight)

		// Pixel surfaces anchor the drag to the grab point within
		// the unit-granular thumb.
		if core.FindSmoothPositioning(t.Self()) {
			_, thumbU, posU := t.scrollbarUnits(t.visibleCount())
			pos := float64(contentY)
			if pos >= posU && pos < posU+thumbU {
				t.scrollbarDragging = true
				t.smoothScrollbarDrag = true
				t.isDragging = false
				t.scrollbarGrabOff = pos - posU
				t.scrollbarThumbPos = posU
				return true
			}
			// Track click falls through to page up/down below,
			// keyed off the smooth thumb position.
			if pos < posU {
				clickedRow = thumbStart - 1
			} else {
				clickedRow = thumbStart + thumbHeight
			}
		}

		// Check if on thumb
		if clickedRow >= thumbStart && clickedRow < thumbStart+thumbHeight {
			// Start scrollbar drag - clear content drag flag
			t.scrollbarDragging = true
			t.isDragging = false
			t.scrollbarDragStart = clickedRow
			t.scrollbarDragOffset = t.scrollOffset
			return true
		}

		// Click on track - page up or page down
		visibleCount := t.visibleCount()
		if clickedRow < thumbStart {
			// Page up
			t.scrollOffset -= visibleCount
			if t.scrollOffset < 0 {
				t.scrollOffset = 0
			}
		} else {
			// Page down
			maxScroll := t.rowCount() - visibleCount
			t.scrollOffset += visibleCount
			if t.scrollOffset > maxScroll {
				t.scrollOffset = maxScroll
			}
		}
		t.Update()
		return true
	}

	// Click on tree content (beside the scrollbar)
	if t.onLane(event.X) {
		return false // Click is in the lane, not the content area
	}

	// Calculate which item was clicked
	clickedRow := int(contentY / metrics.UnitsPerCellHeight)
	clickedIndex := t.scrollOffset + clickedRow

	// Only process if click is on a valid item
	if event.X >= 0 && event.X < bounds.Width && contentY >= 0 && clickedIndex >= 0 && clickedIndex < t.rowCount() {
		item := t.rowAt(clickedIndex)
		if item == nil {
			// A blank: the view knows a row stands here and nothing else, so there
			// is nothing to select, expand or hand to a handler. The answer will
			// arrive and the next click will land on a row.
			return true
		}

		// Check if clicked on expand/collapse indicator. In the
		// multi-column presentation the tree lives in its host span -
		// the key column, or (key hidden) the first visible data
		// column - which may be panned; the painter's own arithmetic
		// says where the glyph ended up.
		if sp, ok := t.treeHostSpan(); ok {
			ix, iw := t.treeExpanderRect(sp, item)
			if event.X >= ix && event.X < ix+iw && !item.IsLeaf() {
				t.ToggleItem(item)
				return true
			}
		}

		// A content click re-zones the internal header focus to content.
		t.headerZone = hzContent

		// Start content drag - clear scrollbar drag flag
		t.isDragging = true
		t.scrollbarDragging = false

		// Check for double-click (400ms threshold)
		now := time.Now().UnixNano()
		isDoubleClick := t.lastClickIndex == clickedIndex &&
			(now-t.lastClickTime) < int64(400*time.Millisecond)

		// Update click tracking
		t.lastClickTime = now
		t.lastClickIndex = clickedIndex

		if isDoubleClick {
			// A double click on an EDITABLE cell belongs to click-to-
			// edit (the release opens the editor): suppress the expand/
			// collapse so both don't fire at once. The classic toggle
			// stays on non-editable parts of the row and on the tree
			// cell's indent/expander region left of the caption text
			// (noteClickEditPress never claims those).
			if t.clickEditItem != nil {
				t.lastClickIndex = -1
				return true
			}
			// Double-click: toggle expand/collapse if not leaf, then activate
			if !item.IsLeaf() {
				t.ToggleItem(item)
			}
			if t.onItemActivated != nil {
				t.onItemActivated(item)
			}
			// Reset double-click state
			t.lastClickIndex = -1
			return true
		}

		t.SetCurrentIndex(clickedIndex)
		// The click also selects the COLUMN as the Enter target when
		// it lands on an editable cell; a click on a non-editable
		// cell keeps the previous target and changes only the row.
		if col := t.editableColumnAt(event.X, item); col != nil {
			t.editLastCol = col
		}
		return true
	}

	// Click is in content area but not on a valid item
	return false
}

// overScrollbarThumb reports whether a widget-local point lies on the
// vertical scrollbar thumb.
func (t *TreeView) overScrollbarThumb(x, y core.Unit) bool {
	visibleCount := t.visibleCount()
	if t.rowCount() <= visibleCount {
		return false
	}
	bounds := t.Bounds()
	if x < 0 || y < 0 || x >= bounds.Width || y >= bounds.Height {
		return false
	}
	_, thumbStart, thumbHeight, _ := t.scrollbarGeometry(visibleCount)
	if !t.onLane(x) {
		return false
	}
	contentY := y - t.rowsTop() // the track starts below the header and any refusal
	if core.FindSmoothPositioning(t.Self()) {
		_, thumbU, posU := t.scrollbarUnits(visibleCount)
		pos := float64(contentY)
		return pos >= posU && pos < posU+thumbU
	}
	row := int(contentY / t.EffectiveCellMetrics().UnitsPerCellHeight)
	return row >= thumbStart && row < thumbStart+thumbHeight
}

// HandleMouseMove handles mouse drag to sweep selection.
func (t *TreeView) HandleMouseMove(event core.MouseMoveEvent) bool {
	// A trinket that answers moves itself still owes the offer of what it
	// could not show; the base makes it for everything that does not.
	t.TrackTooltipHover(core.UnitPoint{X: event.X, Y: event.Y})
	// Track scrollbar-thumb hover. Hover is a no-button affordance: while a
	// button is held (a drag begun elsewhere passing over) don't light the
	// thumb - unless this tree owns the scrollbar drag.
	if over := t.scrollbarDragging || (event.Buttons == 0 && t.overScrollbarThumb(event.X, event.Y)); over != t.scrollbarThumbHovered {
		t.scrollbarThumbHovered = over
		t.Update()
	}
	// Footer horizontal scrollbar thumb: the same hover convention.
	if over := t.hbarDragging || (event.Buttons == 0 && t.overHBarThumb(event.X, event.Y)); over != t.hbarThumbHovered {
		t.hbarThumbHovered = over
		t.Update()
	}
	// Chooser-button hover follows the standard convention.
	t.updateChooserHover(event)

	// If we don't have focus, we shouldn't be processing drags
	// (another trinket got the click and we have stale drag state)
	if !t.HasFocus() {
		t.isDragging = false
		t.scrollbarDragging = false
		t.colDragging = false
		t.colDragCol = nil
		t.colDragFit = false
		t.colDragL, t.colDragR = nil, nil
		t.hbarDragging = false
		return false
	}

	// Text-selection drag inside the row editor.
	if t.handleEditMouseMove(event) {
		return true
	}
	// Column divider drag (multi-column resize).
	if t.handleMultiMove(event) {
		return true
	}
	// Footer horizontal-scrollbar thumb drag.
	if t.handleHBarMove(event) {
		return true
	}

	metrics := t.EffectiveCellMetrics()
	contentY := event.Y - t.rowsTop()

	// Handle scrollbar thumb drag
	// Note: Once drag is captured on press, we don't check horizontal bounds during drag
	if t.scrollbarDragging {
		// Smooth drag: the thumb follows the pointer in units, the
		// scroll offset snaps to the nearest whole row.
		if t.smoothScrollbarDrag {
			visibleCount := t.visibleCount()
			trackU, thumbU, _ := t.scrollbarUnits(visibleCount)
			scrollable := trackU - thumbU
			newPos := float64(contentY) - t.scrollbarGrabOff
			if newPos < 0 {
				newPos = 0
			}
			if newPos > scrollable {
				newPos = scrollable
			}
			t.scrollbarThumbPos = newPos
			maxScroll := t.rowCount() - visibleCount
			newOffset := 0
			if scrollable > 0 && maxScroll > 0 {
				newOffset = int(newPos*float64(maxScroll)/scrollable + 0.5)
			}
			t.scrollOffset = newOffset
			// The thumb moves even when the snapped offset does not.
			t.Update()
			return true
		}

		currentRow := int(contentY / metrics.UnitsPerCellHeight)
		rowDelta := currentRow - t.scrollbarDragStart

		visibleCount := t.visibleCount()
		totalItems := t.rowCount()
		maxScroll := totalItems - visibleCount

		if maxScroll > 0 {
			_, _, thumbHeight, trackHeight := t.scrollbarGeometry(visibleCount)
			scrollableTrack := trackHeight - thumbHeight

			if scrollableTrack > 0 {
				// Convert row delta to scroll offset delta
				scrollDelta := rowDelta * maxScroll / scrollableTrack
				newOffset := t.scrollbarDragOffset + scrollDelta

				// Clamp
				if newOffset < 0 {
					newOffset = 0
				} else if newOffset > maxScroll {
					newOffset = maxScroll
				}

				if newOffset != t.scrollOffset {
					t.scrollOffset = newOffset
					t.Update()
				}
			}
		}
		return true
	}

	// Handle tree item drag
	// Note: Once drag is captured on press, we don't check horizontal bounds during drag
	if !t.isDragging {
		return false
	}

	row := int(contentY / metrics.UnitsPerCellHeight)
	index := t.scrollOffset + row

	// Clamp to valid range
	if index < 0 {
		index = 0
	} else if index >= t.rowCount() {
		index = t.rowCount() - 1
	}

	if index >= 0 {
		if index != t.currentIndex {
			t.SetCurrentIndex(index)
		}
		// Drag-selection also tracks the COLUMN target over editable
		// cells (non-editable cells keep the previous target). It
		// never auto-enters edit mode: armClickEdit's slop check
		// rejects a moved release, combo cells included.
		if col := t.editableColumnAt(event.X, t.rowAt(index)); col != nil {
			t.editLastCol = col
		}
	}

	return true
}

// HandleMouseRelease handles mouse release.
// Only consumes the event when a drag was actually in progress:
// containers broadcast releases to every child, so an unconditional
// true here would starve sibling trinkets of their release.
func (t *TreeView) HandleMouseRelease(event core.MouseReleaseEvent) bool {
	if t.handleEditMouseRelease(event) {
		return true
	}
	// A drag-free release over the press-time candidate flips the
	// cell straight into edit mode.
	t.armClickEdit(event)
	if t.handleMultiRelease(event) {
		return true
	}
	if t.isDragging || t.scrollbarDragging {
		if t.scrollbarDragging {
			// Same stale-hover guard as the footer thumb: recompute from
			// the release point, and clear outright in TUI where no move
			// events arrive to do it later.
			t.scrollbarThumbHovered = core.FindGraphicalFrames(t.Self()) &&
				t.overScrollbarThumb(event.X, event.Y)
		}
		t.isDragging = false
		t.scrollbarDragging = false
		t.smoothScrollbarDrag = false
		t.Update()
		return true
	}
	return false
}

// HandleMouseWheel handles mouse wheel scrolling.
func (t *TreeView) HandleMouseWheel(event core.MouseWheelEvent) bool {
	// While a choice editor's drop-down is popped open, the tree does
	// NOT scroll - a grid moving under an anchored popup is nothing
	// but edge cases. The popup itself still takes the wheel (routed
	// to the overlay, not through here) when its items overflow.
	if t.rowEditing && t.editCombo != nil && t.editCombo.IsOpen() {
		return true
	}
	if t.rowCount() == 0 {
		return false
	}

	// Horizontal wheel pans the column scroll region (scroll mode). The wheel
	// names a direction on the screen; the pan travels along the run.
	if event.DeltaX != 0 && t.scrollHorizontally(t.panStep(core.Unit(event.DeltaX*2)*t.EffectiveCellMetrics().UnitsPerCellWidth)) {
		core.ClaimWheelGesture(event, t.HandleMouseWheel)
		return true
	}

	visibleCount := t.visibleCount()
	maxScroll := t.rowCount() - visibleCount
	if maxScroll <= 0 {
		return false
	}

	// 3 rows per notch; trackpad precise deltas accumulate so slow
	// two-finger pans still move one row at a time.
	rows := 0
	if event.PreciseY != 0 {
		t.wheelAccum += event.PreciseY * 3
		rows = int(t.wheelAccum)
		t.wheelAccum -= float64(rows)
	} else if event.DeltaY < 0 {
		rows = -3
	} else if event.DeltaY > 0 {
		rows = 3
	}
	t.scrollOffset += rows
	if t.scrollOffset < 0 {
		t.scrollOffset = 0
	}
	if t.scrollOffset > maxScroll {
		t.scrollOffset = maxScroll
	}

	core.ClaimWheelGesture(event, t.HandleMouseWheel)
	t.Update()
	return true
}

// HandleFocusIn is called when focus is gained.
func (t *TreeView) HandleFocusIn() {
	// Auto-select first item if nothing is selected
	if t.currentIndex < 0 && t.rowCount() > 0 {
		t.SetCurrentIndex(0)
	}
	// With a header, focus lands on the header BAR first (one stop);
	// Enter drills in, Tab moves on to the content. A content click
	// re-zones to content immediately (see HandleMousePress).
	if t.headerHeight() > 0 {
		t.setHeaderZone(hzBar, 0)
	} else {
		t.headerZone = hzContent
	}
	t.Update()
}

// HandleFocusOut is called when focus is lost.
func (t *TreeView) HandleFocusOut() {
	// Clear any active drag state when focus is lost
	t.isDragging = false
	t.scrollbarDragging = false
	t.headerZone = hzContent
	t.closeColumnChooser()
	// Focus moving elsewhere accepts an in-flight row edit.
	t.endRowEdit(true)
	t.cancelClickEdit()
	t.Update()
}

// AccessibleInfo returns accessibility information.
func (t *TreeView) AccessibleInfo() core.AccessibleInfo {
	info := t.AccessibleTrinket.AccessibleInfo()
	info.Role = core.RoleTree
	info.SetSize = t.rowCount()

	if t.currentIndex >= 0 && t.currentIndex < t.rowCount() {
		item := t.drawRow(t.currentIndex)
		info.PositionInSet = t.currentIndex + 1
		info.Value = item.Text
		info.Level = item.Level() + 1

		if item.Expanded {
			info.State |= core.StateExpanded
		} else if !item.IsLeaf() {
			info.State |= core.StateCollapsed
		}
	}

	if !t.IsEnabled() {
		info.State |= core.StateDisabled
	}

	return info
}
