package trinkets

import (
	"fmt"
	"strconv"

	"github.com/phroun/kittytk/protocol"
)

// Wire registration for the multi-column TreeView surface: a generic
// `collection` container (a keyed grouping of objects), the `column`
// descriptor with its per-item `cell` values, and the treeview's
// column-related properties. Columns nest like everything else (D13):
//
//	new treeview caption="Name" showheader children={
//	    new column id=size caption="Size" width=10 align=right sortable
//	    new column id=kind caption="Kind" width=12
//	    new item caption="Report.txt"
//	}
//
// Cell values are column-major, per the design: each column owns a
// collection of values keyed by item. Items are wire objects with
// numeric IDs (surfaced by correlation keys at build time), so a
// second batch fills the data in:
//
//	set w.tree.sizecol children={ new cell item=123 value="311 KB" }
//
// (Within one build script the item IDs are not yet known, so apps
// build the tree first, read the reply's IDs, then send values.)

// wireCollection is the generic keyed grouping: an ordered list of
// member objects, indexed by each member's own key when it has one
// (a column's id, a cell's item). Parents that accept a collection
// adopt its members as if appended directly - it is packaging, not
// a trinket.
type wireCollection struct {
	of      string // advisory: what the members are ("columns", ...)
	members []any
}

// wireKeyed lets collection members expose their map key.
type wireKeyed interface{ WireKey() string }

// wireOption is one enum choice: key=... value="...". Collections of
// options back a column's enum= property.
type wireOption struct {
	key   string
	value string
}

func (o *wireOption) WireKey() string { return o.key }

// enumOptions converts this collection's members into the column enum
// option list (every member must be an option).
func (c *wireCollection) enumOptions() ([]TreeEnumOption, error) {
	opts := make([]TreeEnumOption, 0, len(c.members))
	for _, m := range c.members {
		o, ok := m.(*wireOption)
		if !ok {
			return nil, fmt.Errorf("enum: collection members must be options, got %T", m)
		}
		opts = append(opts, TreeEnumOption{Key: o.key, Value: o.value})
	}
	return opts, nil
}

// Member returns the member stored under key ("" keys are skipped).
func (c *wireCollection) Member(key string) any {
	for _, m := range c.members {
		if k, ok := m.(wireKeyed); ok && k.WireKey() == key {
			return m
		}
	}
	return nil
}

// wireColumn is the virtual column record/proxy (same lifecycle as
// wireItem: a record until a treeview adopts it, live afterward).
type wireColumn struct {
	id      uint64
	col     TreeColumn // accumulated definition (record state)
	cells   []*wireCell
	haveDef bool

	// Live backrefs once adopted.
	live *TreeColumn
	view *TreeView
}

func (c *wireColumn) SetWireID(id uint64) { c.id = id }
func (c *wireColumn) WireKey() string     { return c.col.ID }

// target returns the column being mutated (live one once adopted).
func (c *wireColumn) target() *TreeColumn {
	if c.live != nil {
		return c.live
	}
	return &c.col
}

func (c *wireColumn) refresh() {
	if c.view != nil {
		c.view.Update()
	}
}

// bind adopts the record into a treeview.
func (c *wireColumn) bind(view *TreeView) {
	col := c.col // copy the accumulated definition
	view.AddColumn(&col)
	c.live = &col
	c.view = view
	for _, cell := range c.cells {
		cell.apply(c)
	}
}

// wireCell is one column-major data value: item=<wire id> value="...".
type wireCell struct {
	item  uint64
	value string
	owner *wireColumn
}

func (c *wireCell) WireKey() string { return strconv.FormatUint(c.item, 10) }

// apply routes the value onto the live item once the column is bound.
func (c *wireCell) apply(owner *wireColumn) {
	c.owner = owner
	if owner == nil || owner.view == nil || owner.live == nil || c.item == 0 {
		return
	}
	if it := owner.view.itemByID(c.item); it != nil {
		it.SetValue(owner.live.ID, c.value)
		// Under an active visual sort a new value can move rows: the
		// trinket re-sorts itself (selection tracks the item).
		if owner.view.sorted {
			owner.view.resortKeepingSelection()
		} else {
			owner.refresh()
		}
	}
}

// itemByID finds an item anywhere in the tree by its wire identity.
func (t *TreeView) itemByID(id uint64) *TreeItem {
	var walk func(items []*TreeItem) *TreeItem
	walk = func(items []*TreeItem) *TreeItem {
		for _, it := range items {
			if uint64(it.ID) == id {
				return it
			}
			if found := walk(it.Children); found != nil {
				return found
			}
		}
		return nil
	}
	return walk(t.rootItems)
}

func colProp(name string, set func(c *wireColumn, v *protocol.Value, f protocol.FlagState) error) protocol.PropertyApplier {
	return wprop(name, func(_ *protocol.BindContext, c *wireColumn, v *protocol.Value, f protocol.FlagState) error {
		if err := set(c, v, f); err != nil {
			return err
		}
		c.refresh()
		return nil
	})
}

func colString(name string, set func(col *TreeColumn, s string)) protocol.Property {
	return protocol.NewProperty("string", colProp(name, func(c *wireColumn, v *protocol.Value, f protocol.FlagState) error {
		s, err := protocol.AsString(name, v, f)
		if err != nil {
			return err
		}
		set(c.target(), s)
		return nil
	}))
}

func colInt(name string, set func(col *TreeColumn, n int)) protocol.Property {
	return protocol.NewProperty("int", colProp(name, func(c *wireColumn, v *protocol.Value, f protocol.FlagState) error {
		n, err := protocol.AsInt(name, v, f)
		if err != nil {
			return err
		}
		set(c.target(), n)
		return nil
	}))
}

func colFlag(name string, set func(col *TreeColumn, b bool)) protocol.Property {
	return protocol.NewProperty("flag", colProp(name, func(c *wireColumn, v *protocol.Value, f protocol.FlagState) error {
		b, err := protocol.AsBool(name, v, f)
		if err != nil {
			return err
		}
		set(c.target(), b)
		return nil
	}))
}

func init() {
	// The generic keyed grouping. Parents that understand collections
	// adopt the members; `of` is advisory documentation on the wire.
	protocol.RegisterType("collection", &protocol.TypeSpec{
		Virtual: true,
		New:     func() any { return &wireCollection{} },
		Props: map[string]protocol.Property{
			"of": protocol.NewProperty("word", wprop("of", func(_ *protocol.BindContext, c *wireCollection, v *protocol.Value, f protocol.FlagState) error {
				w, err := protocol.AsWord("of", v, f)
				if err != nil {
					return err
				}
				c.of = w
				return nil
			})).Tip("Advisory member kind (columns, cells, ...)."),
		},
		Append: func(parent, child any) error {
			p := parent.(*wireCollection)
			p.members = append(p.members, child)
			return nil
		},
	})

	protocol.RegisterType("column", &protocol.TypeSpec{
		Virtual: true,
		// The record starts from the SAME defaults NewTreeColumn gives
		// Go callers (resizable, optional, left-aligned) - the wire's
		// documented defaults must be the actual defaults, or wire-built
		// columns silently lose the divider drag and the [=] chooser.
		New: func() any {
			return &wireColumn{col: TreeColumn{
				Width: 8, MinWidth: 3, Align: "left",
				Resizable: true, Optional: true, SortProxy: -1,
			}}
		},
		Props: map[string]protocol.Property{
			"id": protocol.NewProperty("word", colProp("id", func(c *wireColumn, v *protocol.Value, f protocol.FlagState) error {
				w, err := protocol.AsWord("id", v, f)
				if err != nil {
					return err
				}
				c.target().ID = w
				return nil
			})).Tip("Stable key cell values are stored under."),
			"caption":   colString("caption", func(c *TreeColumn, s string) { c.Caption = s }).Tip("Header caption."),
			"width":     colInt("width", func(c *TreeColumn, n int) { c.Width = n }).Tip("Width in text cells.").Def("8"),
			"min_width": colInt("min_width", func(c *TreeColumn, n int) { c.MinWidth = n }).Tip("Minimum width in text cells.").Def("3"),
			"max_width": colInt("max_width", func(c *TreeColumn, n int) { c.MaxWidth = n }).Tip("Maximum width in text cells (0 = unbounded).").Def("0"),
			"align": protocol.NewProperty("enum", colProp("align", func(c *wireColumn, v *protocol.Value, f protocol.FlagState) error {
				w, err := protocol.AsWord("align", v, f)
				if err != nil {
					return err
				}
				switch w {
				case "left", "center", "right":
					c.target().Align = w
					return nil
				}
				return fmt.Errorf("align: expected left, center, or right")
			})).OneOf("left", "center", "right").Def("left").Tip("Cell text alignment."),
			"resizable": colFlag("resizable", func(c *TreeColumn, b bool) { c.Resizable = b }).Tip("Header divider drag-resizes this column.").Def("true"),
			"hidden":    colFlag("hidden", func(c *TreeColumn, b bool) { c.Hidden = b }).Tip("Column is not displayed.").Def("false"),
			"optional":  colFlag("optional", func(c *TreeColumn, b bool) { c.Optional = b }).Tip("Column appears in the [=] show/hide chooser.").Def("true"),
			"sortable":  colFlag("sortable", func(c *TreeColumn, b bool) { c.Sortable = b }).Tip("Header click requests a sort on this column.").Def("false"),
			"numeric":   colFlag("numeric", func(c *TreeColumn, b bool) { c.Numeric = b }).Tip("Sort by each cell's numeric equivalent (parsed once per value).").Def("false"),
			"sortproxy": colInt("sortproxy", func(c *TreeColumn, n int) { c.SortProxy = n }).Tip("Column index whose values actually sort when this column is chosen (-1 = itself).").Def("-1"),
			"editable":  colFlag("editable", func(c *TreeColumn, b bool) { c.Editable = b }).Tip("Cells in this column can be edited in place (Enter opens the row editor).").Def("false"),
			"enum": protocol.NewProperty("int", wprop("enum", func(ctx *protocol.BindContext, c *wireColumn, v *protocol.Value, f protocol.FlagState) error {
				n, err := protocol.AsInt("enum", v, f)
				if err != nil {
					return err
				}
				coll, ok := ctx.LookupRef(uint64(n)).(*wireCollection)
				if !ok {
					return fmt.Errorf("enum: %d is not a collection on this connection", n)
				}
				opts, err := coll.enumOptions()
				if err != nil {
					return err
				}
				c.target().Enum = opts
				c.refresh()
				return nil
			})).Tip("Wire ID of a collection of option objects; the cell editor becomes a choice box."),
			"enum_store": protocol.NewProperty("enum", colProp("enum_store", func(c *wireColumn, v *protocol.Value, f protocol.FlagState) error {
				w, err := protocol.AsWord("enum_store", v, f)
				if err != nil {
					return err
				}
				switch w {
				case "key", "value":
					c.target().EnumStore = w
					return nil
				}
				return fmt.Errorf("enum_store: expected key or value")
			})).OneOf("key", "value").Def("value").Tip("What a chosen option stores in the data field (cells always DISPLAY the option value)."),
		},
		Append: func(parent, child any) error {
			p := parent.(*wireColumn)
			switch c := child.(type) {
			case *wireCell:
				p.cells = append(p.cells, c)
				c.apply(p)
				return nil
			case *wireCollection:
				for _, m := range c.members {
					cell, ok := m.(*wireCell)
					if !ok {
						return fmt.Errorf("column: collection members must be cells, got %T", m)
					}
					p.cells = append(p.cells, cell)
					cell.apply(p)
				}
				return nil
			}
			return fmt.Errorf("column: children must be cells, got %T", child)
		},
	})

	protocol.RegisterType("option", &protocol.TypeSpec{
		Virtual: true,
		New:     func() any { return &wireOption{} },
		Props: map[string]protocol.Property{
			"key": protocol.NewProperty("word", wprop("key", func(_ *protocol.BindContext, o *wireOption, v *protocol.Value, f protocol.FlagState) error {
				w, err := protocol.AsWord("key", v, f)
				if err != nil {
					return err
				}
				o.key = w
				return nil
			})).Tip("Stable option key."),
			"value": protocol.NewProperty("string", wprop("value", func(_ *protocol.BindContext, o *wireOption, v *protocol.Value, f protocol.FlagState) error {
				s, err := protocol.AsString("value", v, f)
				if err != nil {
					return err
				}
				o.value = s
				return nil
			})).Tip("Displayed option value."),
		},
	})

	protocol.RegisterType("cell", &protocol.TypeSpec{
		Virtual: true,
		New:     func() any { return &wireCell{} },
		Props: map[string]protocol.Property{
			"item": protocol.NewProperty("int", wprop("item", func(_ *protocol.BindContext, c *wireCell, v *protocol.Value, f protocol.FlagState) error {
				n, err := protocol.AsInt("item", v, f)
				if err != nil {
					return err
				}
				c.item = uint64(n)
				c.apply(c.owner)
				return nil
			})).Tip("Wire ID of the item this value belongs to."),
			"value": protocol.NewProperty("string", wprop("value", func(_ *protocol.BindContext, c *wireCell, v *protocol.Value, f protocol.FlagState) error {
				s, err := protocol.AsString("value", v, f)
				if err != nil {
					return err
				}
				c.value = s
				c.apply(c.owner)
				return nil
			})).Tip("Cell text for this column and item."),
		},
	})
}

// treeViewProps is the treeview's full wire property map: the original
// properties plus the multi-column surface.
func treeViewProps() map[string]protocol.Property {
	return map[string]protocol.Property{
		"selected":     intProp("selected", (*TreeView).SetCurrentIndex).Tip("Selected visible-row index.").Def("-1"),
		"indent_width": intProp("indent_width", (*TreeView).SetIndentWidth).Tip("Indent width per tree level."),

		"caption":    stringProp("caption", (*TreeView).SetKeyCaption).Tip("Header caption over the key (tree) column."),
		"editable":   boolProp("editable", (*TreeView).SetEditable).Tip("The key (tree) column joins the row editor (edits the item caption).").Def("false"),
		"showheader": boolProp("showheader", (*TreeView).SetShowHeader).Tip("Show the column header row.").Def("false"),
		"ledger":     boolProp("ledger", (*TreeView).SetLedger).Tip("Alternate non-selected rows in the ledger colors.").Def("false"),
		"treelines":  boolProp("treelines", (*TreeView).SetTreeLines).Tip("Connector lines in the indent space; leaf items get a glyph too.").Def("false"),
		"showkey":    boolProp("showkey", (*TreeView).SetShowKey).Tip("Show the key (tree) column first.").Def("true"),
		"fit_width":  boolProp("fit_width", (*TreeView).SetFitWidth).Tip("Squeeze columns to the width (no horizontal scrolling).").Def("true"),
		"key_width":  intProp("key_width", (*TreeView).SetKeyWidth).Tip("Key column width in text cells (scroll mode).").Def("20"),
		"fixed_left": intProp("fixed_left", func(t *TreeView, n int) {
			t.SetFixedColumns(n, t.fixedRight)
		}).Tip("Visible columns pinned outside horizontal scrolling, from the left.").Def("0"),
		"fixed_right": intProp("fixed_right", func(t *TreeView, n int) {
			t.SetFixedColumns(t.fixedLeft, n)
		}).Tip("Visible columns pinned outside horizontal scrolling, from the right.").Def("0"),
		"sorted": boolProp("sorted", func(t *TreeView, b bool) {
			t.SetSorted(b, t.sortedBy, t.sortDescending)
		}).Tip("Show the sort indicator.").Def("false"),
		"sortedby": intProp("sortedby", func(t *TreeView, n int) {
			t.SetSorted(t.sorted, n, t.sortDescending)
		}).Tip("Sort column: -1 = the key column, else a column index.").Def("-1"),
		"descending": boolProp("descending", func(t *TreeView, b bool) {
			t.SetSorted(t.sorted, t.sortedBy, b)
		}).Tip("Sort direction indicator points down.").Def("false"),
	}
}
