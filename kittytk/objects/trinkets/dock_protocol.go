package trinkets

import (
	"fmt"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/protocol"
)

// Wire registration for DockRow and the virtual dockentry. Entries
// are wire objects with real IDs (live-proxy pattern, like tree
// items): apps add them with `set dock children={e=new dockentry
// caption="Doc 1" window=1042}`, remove them with `destroy dock.e`,
// and clicks arrive as `click trinket=<entry> window=<win>` events.
type wireDockEntry struct {
	id      uint64
	caption string
	window  uint64

	ctx   *protocol.BindContext
	entry *DockEntry
	row   *DockRow
}

// SetWireID receives the factory-assigned identity.
func (e *wireDockEntry) SetWireID(id uint64) { e.id = id }

func init() {
	protocol.RegisterType("dockentry", &protocol.TypeSpec{
		Virtual: true,
		New:     func() any { return &wireDockEntry{} },
		Bind: func(ctx *protocol.BindContext, target any) {
			target.(*wireDockEntry).ctx = ctx
		},
		Props: map[string]protocol.Property{
			"caption": protocol.NewProperty("string", wprop("caption", func(_ *protocol.BindContext, e *wireDockEntry, v *protocol.Value, f protocol.FlagState) error {
				s, err := protocol.AsString("caption", v, f)
				if err != nil {
					return err
				}
				e.caption = s
				if e.entry != nil {
					e.entry.Title = s
					e.row.Update()
				}
				return nil
			})).Tip("Dock entry caption"),
			"window": protocol.NewProperty("int", wprop("window", func(_ *protocol.BindContext, e *wireDockEntry, v *protocol.Value, f protocol.FlagState) error {
				n, err := protocol.AsInt("window", v, f)
				if err != nil {
					return err
				}
				e.window = uint64(n)
				return nil
			})).Tip("Hosted window id"),
		},
		Destroy: func(t any) error {
			e := t.(*wireDockEntry)
			if e.entry != nil && e.row != nil {
				e.row.RemoveEntry(e.entry)
				e.entry, e.row = nil, nil
			}
			return nil
		},
	})

	protocol.RegisterType("dockrow", &protocol.TypeSpec{
		New: func() any { return NewDockRow() },
		ID: func(t any) uint64 {
			return uint64(t.(*DockRow).ObjectID())
		},
		Props: map[string]protocol.Property{
			"entry_width": intProp("entry_width", (*DockRow).SetEntryWidth).Tip("Width of each dock entry in units"),
		},
		Append: func(parent, child any) error {
			row, ok := parent.(*DockRow)
			if !ok {
				return fmt.Errorf("dockrow: wrong parent type %T", parent)
			}
			e, ok := child.(*wireDockEntry)
			if !ok {
				return fmt.Errorf("dockrow: children must be dockentry, got %T", child)
			}
			entry := &DockEntry{
				Title:    e.caption,
				WindowID: core.ObjectID(e.window),
			}
			ctx, entryID, winID := e.ctx, e.id, e.window
			entry.OnClick = func() {
				if ctx != nil {
					ctx.EmitEvent(protocol.NewEvent("click").
						WithUint("trinket", entryID).
						WithUint("window", winID))
				}
			}
			e.entry, e.row = entry, row
			row.AddEntry(entry)
			return nil
		},
		Destroy: func(t any) error {
			return destroyTrinket(t.(*DockRow))
		},
	})
}
