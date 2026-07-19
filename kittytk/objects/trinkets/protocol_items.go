package trinkets

import (
	"fmt"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/protocol"
)

// The virtual `item` type unifies list-shaped children (D13): combobox
// entries, listview rows, and treeview nodes are all items; trees nest
// them with children={} blocks:
//
//	new treeview children={
//	    fruit=new item caption="Fruit" expanded children={
//	        new item caption="Apple"
//	        new item caption="Pear"
//	    }
//	}
//
// Items carry real ObjectIDs (same allocation space as trinkets), and
// correlation keys name them like anything else - `set tree.fruit
// caption="…"` and `destroy tree.fruit` work after construction. An
// item starts as a record; when a treeview adopts it, it becomes a
// live proxy for the TreeItem it produced, so later set/destroy
// route to the visible tree.
type wireItem struct {
	id       uint64
	caption  string
	expanded bool
	children []*wireItem

	// Live backrefs, filled in when a treeview adopts the item.
	node *TreeItem
	view *TreeView
}

// SetWireID receives the factory-assigned identity (protocol calls
// this for virtual targets at construction).
func (it *wireItem) SetWireID(id uint64) { it.id = id }

func init() {
	protocol.RegisterType("item", &protocol.TypeSpec{
		Virtual: true,
		New:     func() any { return &wireItem{} },
		Props: map[string]protocol.Property{
			"caption": protocol.NewProperty("string", wprop("caption", func(_ *protocol.BindContext, it *wireItem, v *protocol.Value, f protocol.FlagState) error {
				s, err := protocol.AsString("caption", v, f)
				if err != nil {
					return err
				}
				it.caption = s
				if it.node != nil {
					it.node.Text = s
					// A caption change under an active visual sort can
					// move the row: the trinket re-sorts itself (the
					// viewport follows the selection if it was in view).
					if it.view != nil && it.view.sorted {
						it.view.resortKeepingSelection()
					} else {
						it.refresh()
					}
				}
				return nil
			})).Tip("Row or node display text."),
			"expanded": protocol.NewProperty("flag", wprop("expanded", func(_ *protocol.BindContext, it *wireItem, v *protocol.Value, f protocol.FlagState) error {
				b, err := protocol.AsBool("expanded", v, f)
				if err != nil {
					return err
				}
				it.expanded = b
				if it.node != nil {
					it.node.Expanded = b
					it.refresh()
				}
				return nil
			})).Tip("Node expanded (tree nodes).").Def("false"),
		},
		Append: func(parent, child any) error {
			p := parent.(*wireItem)
			c, ok := child.(*wireItem)
			if !ok {
				return fmt.Errorf("item: children must be items, got %T", child)
			}
			p.children = append(p.children, c)
			if p.node != nil {
				// Late append onto an already-live item (set k
				// children={...}): grow the real tree too.
				p.node.AddChild(c.bind(p.view))
				p.refresh()
			}
			return nil
		},
		Destroy: func(t any) error {
			it := t.(*wireItem)
			if it.node == nil || it.view == nil {
				return nil // never adopted; nothing visible to remove
			}
			if parent := it.node.Parent; parent != nil {
				parent.RemoveChild(it.node)
			} else {
				it.view.RemoveRootItem(it.node)
			}
			it.refresh()
			it.node, it.view = nil, nil
			return nil
		},
	})
}

// bind converts the wire item subtree into TreeItems carrying the
// wire identity, and records live backrefs for later set/destroy.
func (it *wireItem) bind(view *TreeView) *TreeItem {
	node := NewTreeItem(it.caption)
	if it.id != 0 {
		node.ID = core.ObjectID(it.id)
	}
	node.Expanded = it.expanded
	it.node = node
	it.view = view
	for _, c := range it.children {
		node.AddChild(c.bind(view))
	}
	return node
}

func (it *wireItem) refresh() {
	if it.view != nil {
		// Structure or expansion changed: the flattened row list must
		// be rebuilt before the next paint or index-based selection.
		it.view.rebuildFlatList()
		it.view.Update()
	}
}
