package trinkets

import (
	"fmt"

	"github.com/phroun/kittytk/protocol"
)

// Wire registration for ListView.
//
// Rows are either written down -- shared virtual items (items_protocol.go);
// nesting is a treeview affair -- or NAMED, and a list reads one or the other:
//
//	new listview items={
//	    new item caption="Alpha"
//	    new item caption="Beta"
//	} selected=0
//
//	new listview source="source:mail.incoming"
//	new listview source="bundle:objectLibrary@2.1.0" display="subject" value="id"
func init() {
	protocol.RegisterType("listview", &protocol.TypeSpec{
		Events: map[string]protocol.EventDesc{
			"change": protocol.NewEventDesc("The selection moved.").
				Field("trinket", "uint", "The list's object ID.").
				Field("selected", "int", "Index of the newly selected row, or -1 for none."),
			"activate": protocol.NewEventDesc("A row was activated — double-clicked, or Enter on the selection.").
				Field("trinket", "uint", "The list's object ID.").
				Field("selected", "int", "Index of the activated row."),
		},
		New: func() any { return NewListView() },
		ID: func(t any) uint64 {
			return uint64(t.(*ListView).ObjectID())
		},
		Bind: func(ctx *protocol.BindContext, target any) {
			l := target.(*ListView)
			id := uint64(l.ObjectID())
			l.SetOnCurrentChanged(func(index int) {
				ctx.EmitEvent(protocol.NewEvent("change").
					WithUint("trinket", id).WithInt("selected", index))
			})
			l.SetOnItemActivated(func(index int) {
				ctx.EmitEvent(protocol.NewEvent("activate").
					WithUint("trinket", id).WithInt("selected", index))
			})
		},
		Props: map[string]protocol.Property{
			// Where the rows come from, when they are not written down here.
			//
			// `source:` names a live source somebody registered; `bundle:`, or
			// the bare form, names a bundle to be found and assembled. They are
			// the two namespaces a bundle's own includes use, so the language
			// says this one way rather than two.
			"source": protocol.NewProperty("string", wprop("source",
				func(_ *protocol.BindContext, l *ListView, v *protocol.Value, f protocol.FlagState) error {
					s, err := protocol.AsString("source", v, f)
					if err != nil {
						return err
					}
					return l.SetSourceByName(s)
				})).Tip("Where the rows come from: source:<name>, or a bundle key."),

			// Which field of a record the list SHOWS, and which it means. One
			// field until somebody says otherwise.
			"display": stringProp("display", func(l *ListView, s string) { l.SetFields(s, "") }).
				Tip("The record field shown in each row."),
			"value": stringProp("value", func(l *ListView, s string) { l.SetFields("", s) }).
				Tip("The record field a row stands for, where it is not the one shown."),

			"selected": intProp("selected", (*ListView).SetCurrentIndex).Tip("Selected row index (-1 = none).").Def("-1"),
			"ledger":   boolProp("ledger", (*ListView).SetLedger).Tip("Alternate non-selected rows in the ledger colors.").Def("false"),
			"items": protocol.NewCollection(func(parent, child any) error {
				l, ok := parent.(*ListView)
				if !ok {
					return fmt.Errorf("listview: wrong parent type %T", parent)
				}
				it, ok := child.(*wireItem)
				if !ok {
					return fmt.Errorf("listview: items must be items, got %T", child)
				}
				if len(it.children) != 0 {
					return fmt.Errorf("listview: items cannot nest (use a treeview)")
				}
				l.AddTextItem(it.caption)
				return nil
			}).Members("item").Tip("The rows in the list."),
		},
		Destroy: func(t any) error {
			return destroyTrinket(t.(*ListView))
		},
	})
}
