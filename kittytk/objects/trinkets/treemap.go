package trinkets

// What a kind of row puts in each of a view's columns.
//
// A column is the VIEW's name for a thing that different sources spell
// differently: a host's rows say `bytes` and a window's say `size`, and the view
// wants one column called Size. So the mapping is per KIND OF ROW, which is what
// a `serval.NodeType` is -- and it lives here rather than there, because serval
// must not learn what a column is any more than it knows about a twisty.
//
// # Two indirections, and they compose
//
// They are easy to confuse and they do different jobs:
//
//	SortProxy   column -> column, the VIEW's own. Sort the Size column by the
//	            hidden SizeBytes column: a locale key, a natural-sort key, a
//	            manual rank -- a genuinely different datum from what is shown.
//	a mapping   column -> field, the SOURCE's. Which of THIS kind's fields fills
//	            column M.
//
// **The proxy resolves first, then the mapping.** Getting it the other way round
// produces nonsense the moment a graft is involved: a grafted kind never hears
// about the proxy, and is only ever asked which of its fields fills column M.
//
// # The caption always exists
//
// A row has a label beside its twisty whether or not it has any columns, and it
// can only be HIDDEN, never absent. So every kind's mapping has at least that one
// entry to make, and a grafted kind sharing no data columns with its parent still
// draws. That is a guarantee rather than a fallback.
//
// # Four rungs, and the first two need almost nothing
//
//	NodeMap{}                       the IDENTITY: column `size` is field `size`
//	NodeMap{Dotted: true}           column `size` is field `.size`
//	NodeMap{Positional: true}       `.0` is the caption, `.1` the first column
//	InOrder(".caption", ".size")    a list, lined up with the columns
//	NodeMap{Caption:, Columns:}     value, display and collation per column
//
// **The identity is the one to expect.** A column and a field wanting the same
// name is the ordinary case, so a name means itself and nothing has to be
// declared -- which is what a tree making its own source relies on, and why every
// existing example goes on working without hearing the word.
//
// Dotted is the identity with a dot, and it exists because **a bundle's fields
// wear one**: read under serval's `Whole`, a record that is a list names its
// members `.caption` and `.size`, so a column called `size` wants `.size`. That
// has caught this author before. It is a flag rather than a name per column
// because it is a property of the READING and not of any one column.
//
// Positional is for a source whose records are TUPLES -- a delimited file, a PSL
// list -- and it is one field on a struct rather than a name per column. Both of
// serval's loaders spell a positional member the same way, `.0` and `.1` and so
// on, so there is one answer and not two.
//
// A list is the rung above: a caller who knows the field names and only wants
// them lined up says them once, in column order, and never writes a map key.
//
// # A column that matches nothing is an empty cell
//
// Whatever rung is in play, a column whose field the record has not got reads as
// `undefined` -- which is what serval answers for a field a record lacks
// everywhere else, and is a guarantee rather than a silence. The cell draws empty
// and a sort on it gathers those rows at one end of that level, separated by the
// levels after it. Nothing is guessed, nothing falls back to a different rung,
// and a grafted kind sharing only some of the columns simply leaves the rest
// blank. That is free, and guessing would not be.

import (
	"strconv"

	"github.com/phroun/serval"
)

// treeCaption is the field a made row's caption goes in -- the label beside the
// twisty, which every row has.
const treeCaption = "caption"

// A CellMap says which of a source's fields fills one column.
type CellMap struct {
	// Value is the field that SORTS and filters. It is the typed one: a size in
	// bytes rather than "1.2 MB", so nothing has to be parsed back out of a
	// rendering to compare it.
	Value string

	// Display is the field that is DRAWN. Empty means Value, which is the common
	// case -- a plain list of strings has nothing to tell them apart.
	Display string

	// Collate is the collation a sort on this column uses: empty, exact, fold or
	// natural. Empty takes `fold`, which is what the view's own comparison did
	// with `strings.ToLower`.
	//
	// `natural` is worth knowing about: it compares digit runs as numbers, so
	// `item2` sorts before `item10` where folding puts `item10` first.
	Collate string
}

// sortField is what a sort on this cell names: the value where there is one, and
// the display otherwise. Sorting by what is drawn is worse than sorting by what
// is meant, and better than not sorting.
func (c CellMap) sortField() string {
	if c.Value != "" {
		return c.Value
	}
	return c.Display
}

// showField is what is drawn: the display where there is one, and the value
// otherwise.
func (c CellMap) showField() string {
	if c.Display != "" {
		return c.Display
	}
	return c.Value
}

// A NodeMap is what one kind of row puts in the columns.
type NodeMap struct {
	// Caption fills the key column, and always exists.
	Caption CellMap

	// Columns is the rest, keyed by TreeColumn.ID.
	//
	// **By ID and not by index**, because a kind is configured once and columns
	// are added, removed and reordered afterwards. The proxy stays an index,
	// being resolved in the same breath as the columns it points between.
	Columns map[string]CellMap

	// Fields lines a source's fields up with the columns: the first names the
	// CAPTION and each one after it a column, in the order the columns were
	// declared. It is the low-friction rung for a caller who knows the names and
	// only wants them in order -- see InOrder.
	//
	// An explicit entry in Columns wins over it, so a caller can line everything
	// up and then say more about one column.
	Fields []string

	// Dotted makes the identity wear a dot: column `size` is field `.size`, and
	// the caption is `.caption`. It is what a BUNDLE wants, a record read under
	// serval's `Whole` naming its members with one.
	Dotted bool

	// Positional takes the fields by position: `.0` is the caption and `.n` is
	// the nth column, which is how both of serval's loaders name the members of
	// a record that is a tuple.
	//
	// It is a flag rather than a generated list of names on purpose: a column
	// added afterwards is picked up without the mapping being restated.
	Positional bool

	// Icon is the field naming a registered icon, where a kind has one.
	Icon string
}

// InOrder lines a source's fields up with the view's columns: the first names the
// caption, and each one after it a column in declared order.
//
// The shortest thing that works for a source whose field names are known and
// whose records are not tuples, which is most of them.
func InOrder(fields ...string) NodeMap { return NodeMap{Fields: fields} }

// positionName is a record's nth member, spelled the way serval's loaders spell
// one. `delimited.go` and `psl.go` agree on it, so a positional mapping means one
// thing rather than two.
func positionName(i int) string { return "." + strconv.Itoa(i) }

// SetKindMap says what one kind of row puts in the columns.
//
// The kind is the name a `serval.NodeType` is registered under, and the empty
// name is the default kind -- which is the top level's, and a tree of one shape's
// only one.
func (t *TreeView) SetKindMap(kind string, m NodeMap) {
	if t.kinds == nil {
		t.kinds = map[string]NodeMap{}
	}
	t.kinds[kind] = m
	t.touched()
	t.Update()
}

// KindMap is what that kind puts in the columns, and the zero one where nothing
// was declared -- which reads as the identity mapping.
func (t *TreeView) KindMap(kind string) NodeMap { return t.kinds[kind] }

// cellOf is the field pair for one column under one kind, and the identity where
// nothing was declared. A nil column is the KEY column, which is the caption.
// The rungs are tried most-specific first, so a caller may line everything up
// and then say more about one column without the general answer overriding the
// particular one.
func (t *TreeView) cellOf(kind string, col *TreeColumn) CellMap {
	m := t.kinds[kind]
	if col == nil {
		switch {
		case m.Caption.sortField() != "":
			return m.Caption
		case len(m.Fields) > 0:
			return CellMap{Value: m.Fields[0]}
		case m.Positional:
			return CellMap{Value: positionName(0)}
		case m.Dotted:
			return CellMap{Value: "." + treeCaption}
		}
		return CellMap{Value: treeCaption}
	}
	if c, ok := m.Columns[col.ID]; ok && c.sortField() != "" {
		return c
	}
	// The caption took the first field, so column 0 takes the second.
	if i := t.columnIndex(col); i >= 0 {
		switch {
		case i+1 < len(m.Fields):
			return CellMap{Value: m.Fields[i+1]}
		case m.Positional:
			return CellMap{Value: positionName(i + 1)}
		}
	}
	if m.Dotted {
		return CellMap{Value: "." + col.ID}
	}
	return CellMap{Value: col.ID}
}

// sortFields is the view's sort levels as serval's, for one kind.
//
// **`seq` goes last, always, and that is its real job.** It was a bridge while
// the made source was not asked to sort; it is now what makes the sort STABLE,
// which is exactly what `sort.SliceStable` was doing -- rows equal on every level
// keep the order the application put them in. Unsorted, it is the whole order,
// and a tree whose items were inserted rather than appended still draws them
// where they were put.
func (t *TreeView) sortFields(kind string) []serval.SortLevel {
	out := make([]serval.SortLevel, 0, len(t.sortLevels)+1)
	if t.sorted {
		for _, lv := range t.sortLevels {
			// sortTarget resolves the PROXY, which is the view's own
			// indirection and is settled before any field is named.
			idx, numeric := t.sortTarget(lv.By)
			var col *TreeColumn
			if idx >= 0 && idx < len(t.columns) {
				col = t.columns[idx]
			}
			cell := t.cellOf(kind, col)
			level := serval.Level{Descending: lv.Descending}
			if !numeric {
				// A numeric column's value IS a number, and numbers compare as
				// numbers; a collation would say nothing about them.
				if level.Collation = cell.Collate; level.Collation == "" {
					level.Collation = serval.CollateFold
				}
			}
			out = append(out, serval.SortLevel{Field: cell.sortField(), Level: level})
		}
	}
	return append(out, serval.SortLevel{Field: treeSeq})
}

// cells is what one item carries for the sort, under the field names this kind's
// mapping gives its columns.
//
// **A numeric column's value goes out as a NUMBER**, which is the whole of why
// the made source emits anything at all. `Values` holds text, and the view's own
// comparison had to parse `"$-1,234.56 USD"` back out of a rendering every time
// it sorted; the number is what the column MEANS, and serval compares numbers
// natively.
//
// A column the row has no value in goes out as empty text rather than being left
// out. Left out it would read as `undefined`, which ranks before every string --
// a different order from the one the view has always drawn.
func (t *TreeView) cells(item *TreeItem, kind string) serval.Record {
	out := serval.Record{
		serval.Named(t.cellOf(kind, nil).sortField(), item.Text),
	}
	for _, col := range t.columns {
		cell := t.cellOf(kind, col)
		if col.Numeric {
			out = append(out, serval.Named(cell.sortField(),
				serval.NewFloat(item.NumericValue(col.ID))))
			continue
		}
		out = append(out, serval.Named(cell.sortField(), item.Value(col.ID)))
	}
	return out
}
