package trinkets

import (
	"fmt"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/protocol"
)

// Wire registration for ComboBox (see docs/property-vocabulary.md).
// Per D13's unification, combobox entries are children of the shared
// virtual `item` type (items_protocol.go):
//
//	new combobox children={new item caption="A"; new item caption="B"} selected=1
//
// Note: selected must follow the items that make it valid (properties
// apply in order).

func init() {
	protocol.RegisterType("combobox", &protocol.TypeSpec{
		New: func() any { return NewComboBox() },
		ID: func(t any) uint64 {
			return uint64(t.(*ComboBox).ObjectID())
		},
		Bind: func(ctx *protocol.BindContext, target any) {
			c := target.(*ComboBox)
			id := uint64(c.ObjectID())
			c.SetOnCurrentIndexChanged(func(index int) {
				ctx.EmitEvent(protocol.NewEvent("change").
					WithUint("trinket", id).WithInt("selected", index))
			})
		},
		Props: map[string]protocol.Property{
			"selected":    intProp("selected", (*ComboBox).SetCurrentIndex).Tip("Selected index (-1 = none).").Def("-1"),
			"editable":    boolProp("editable", (*ComboBox).SetEditable).Tip("Allow typing a custom value.").Def("false"),
			"placeholder": stringProp("placeholder", (*ComboBox).SetPlaceholder).Tip("Empty-field hint text."),
			"max_visible": intProp("max_visible", (*ComboBox).SetMaxVisibleItems).Tip("Max dropdown rows shown."),
		},
		Append: func(parent, child any) error {
			c, ok := parent.(*ComboBox)
			if !ok {
				return fmt.Errorf("combobox: wrong parent type %T", parent)
			}
			it, ok := child.(*wireItem)
			if !ok {
				return fmt.Errorf("combobox: children must be items, got %T", child)
			}
			if len(it.children) != 0 {
				return fmt.Errorf("combobox: items cannot nest")
			}
			c.AddItem(it.caption)
			return nil
		},
	})
}

// ensure ComboBox still satisfies core.Trinket for regTrinket users
var _ core.Trinket = (*ComboBox)(nil)
