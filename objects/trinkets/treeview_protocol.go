package trinkets

import (
	"fmt"

	"github.com/phroun/kittytk/protocol"
)

// Wire registration for TreeView. Nodes are shared virtual items
// (items_protocol.go), nested with children={} blocks and expanded
// flags:
//
//	new treeview children={
//	    new item caption="Fruit" expanded children={
//	        new item caption="Apple"
//	        new item caption="Pear"
//	    }
//	    new item caption="Roots"
//	}
//
// Items are first-class wire objects: each carries an ObjectID, and
// correlation keys name them (`fruit=new item …` → `tree.fruit`, then
// `set tree.fruit caption="…"`, `destroy tree.fruit`). Selection
// events report the item's identity as item=<id> alongside the
// visible-row index.
func init() {
	protocol.RegisterType("treeview", &protocol.TypeSpec{
		New: func() any { return NewTreeView() },
		ID: func(t any) uint64 {
			return uint64(t.(*TreeView).ObjectID())
		},
		Bind: func(ctx *protocol.BindContext, target any) {
			tv := target.(*TreeView)
			id := uint64(tv.ObjectID())
			emit := func(evType string, item *TreeItem) {
				if item == nil {
					return
				}
				ctx.EmitEvent(protocol.NewEvent(evType).
					WithUint("trinket", id).
					WithUint("item", uint64(item.ID)).
					WithInt("selected", tv.CurrentIndex()))
			}
			tv.SetOnCurrentChanged(func(item *TreeItem) { emit("change", item) })
			tv.SetOnItemActivated(func(item *TreeItem) { emit("activate", item) })
			tv.SetOnItemExpanded(func(item *TreeItem) {
				if item == nil {
					return
				}
				ctx.EmitEvent(protocol.NewEvent("expand").
					WithUint("trinket", id).
					WithUint("item", uint64(item.ID)).
					WithFlag("expanded", protocol.FlagTrue))
			})
			tv.SetOnItemCollapsed(func(item *TreeItem) {
				if item == nil {
					return
				}
				ctx.EmitEvent(protocol.NewEvent("expand").
					WithUint("trinket", id).
					WithUint("item", uint64(item.ID)).
					WithFlag("expanded", protocol.FlagFalse))
			})
			tv.SetOnSortRequested(func(sorted bool, sortedBy int, descending bool) {
				flag := func(b bool) protocol.FlagState {
					if b {
						return protocol.FlagTrue
					}
					return protocol.FlagFalse
				}
				ctx.EmitEvent(protocol.NewEvent("sort").
					WithUint("trinket", id).
					WithFlag("sorted", flag(sorted)).
					WithInt("sortedby", sortedBy).
					WithFlag("descending", flag(descending)))
			})
			tv.SetOnCellEdited(func(item *TreeItem, col *TreeColumn, value string) {
				colIdx := -1
				for i, c := range tv.columns {
					if c == col {
						colIdx = i
						break
					}
				}
				ctx.EmitEvent(protocol.NewEvent("edit").
					WithUint("trinket", id).
					WithUint("item", uint64(item.ID)).
					WithInt("column", colIdx).
					WithString("value", value))
			})
		},
		Props: treeViewProps(),
		Append: func(parent, child any) error {
			tv, ok := parent.(*TreeView)
			if !ok {
				return fmt.Errorf("treeview: wrong parent type %T", parent)
			}
			switch c := child.(type) {
			case *wireItem:
				tv.AddRootItem(c.bind(tv))
				return nil
			case *wireColumn:
				c.bind(tv)
				return nil
			case *wireCollection:
				// A collection is packaging: adopt each member as if
				// appended directly.
				for _, m := range c.members {
					col, ok := m.(*wireColumn)
					if !ok {
						return fmt.Errorf("treeview: collection members must be columns, got %T", m)
					}
					col.bind(tv)
				}
				return nil
			}
			return fmt.Errorf("treeview: children must be items or columns, got %T", child)
		},
		Destroy: func(t any) error {
			return destroyTrinket(t.(*TreeView))
		},
	})
}
