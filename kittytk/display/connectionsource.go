package display

// The Connections window's rows, as a source the tree reads.
//
// The window used to WRITE its rows: two batches of protocol text, one building
// the items and one filling every cell of every column against the ids the first
// one surfaced. That works, and it means the window and the tree each hold the
// same facts -- the row on one side, the cell text on the other -- with nothing
// but care keeping them agreeing.
//
// Here the rows are a SOURCE and the tree reads them. What the window holds is
// the `connectionsRow` slice it always held; what the tree draws is two lists of
// records over it, flattened by serval into the visible rows. A cell is no longer
// something anybody sets: it is a field of a record, read through the mapping.
//
// # Two kinds of row, two sources
//
//	peers   a client, or this host                 the top level
//	apps    an app a client has presented          children by `peer`
//
// Two sources rather than one because that is what they are: an app is not a
// client with a name in it, and the criterion that gathers a client's apps is an
// adjacency list on the client's fingerprint. The kinds are what the mapping is
// per, and both happen to want the same one here -- a `connectionsRow` is a
// `connectionsRow` -- which is worth noticing rather than hiding: the machinery
// costs nothing where the kinds agree.
//
// # The field names ARE the column names
//
// Nothing is declared about which field fills which column, because each field is
// named after the column it fills. That is the identity rung of the mapping and it
// is the ordinary case; the two things it cannot say are said instead:
//
//	Caption    the nickname, which is not a data column
//	ReadOnly   `fixed`, which is not a column at all
//
// **`fixed` is per ROW and not per kind**, which is why the mapping reads it off
// the record. A client may be renamed and this host may not, and they are the same
// kind of row: what decides is the row, so the row is what says.
//
// # Order is the store's, until somebody clicks a header
//
// `seq` carries the order the two stores put the rows in -- this host first, then
// the clients that have a rule in the order they were decided about, then the ones
// merely admitted -- because that order is not any field's and cannot be recovered
// from one. A click on a header tells the tree to order its levels otherwise, and
// the view does the telling: see `TreeView.tellOrder`.
//
// # And a change is TOLD
//
// Nothing here notices. A standing chosen in the pane rewrites the store, the row,
// and then the records -- `Restate` on the list it belongs to, `Stale` on the tree
// that flattens it, `Reread` on the view that holds the sequence. Three sayings at
// three doors, and not one poll between them.

import (
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/serval"
)

// The kind an app row is of. The top level's kind is the default one, which has
// no name -- it being what a tree of one shape would use for everything.
const appsKind = "apps"

// The fields a row carries. The six data ones are named after the columns they
// fill, so the mapping has nothing to say about them.
const (
	rowName  = "name"  // the caption: a nickname, or an app's own name
	rowFixed = "fixed" // this row takes no nickname
	rowPeer  = "peer"  // the client an app was presented by
	rowSeq   = "seq"   // where the stores put this row among its siblings
)

// rowKeyOf is a row's identity as the sources state it: a client by its
// fingerprint, and an app by the client's fingerprint and the name it connected
// as.
//
// **An app's name alone is not an identity.** Two clients may each present an
// Editor, and they are two rows; a key that is only the name would make them one,
// and the tree would draw one of them twice.
func rowKeyOf(r connectionsRow) *serval.Value {
	if r.app == "" {
		return serval.NewText(r.identity)
	}
	return serval.NewText(r.identity + "\x00" + r.app)
}

// connectionsFields is one row as a record. Every field a column wants, named
// after that column, plus the three the tree and the mapping ask for.
func connectionsFields(r connectionsRow, seq int) serval.Record {
	return serval.Record{
		serval.Named(rowName, r.name),
		serval.Named("lastseen", seenWord(r)),
		serval.Named("storage", storageWord(r)),
		serval.Named("permission", permWord(r)),
		serval.Named("identity", identityWord(r)),
		serval.Named("stamp", stampWord(r)),
		// A byte count goes out as a NUMBER, because that is what the hidden
		// column means and what the shown one hands its sorting to. As text, 9K
		// would sort after 10M.
		serval.Named("bytes", r.bytes),
		// This host is named by its certificate and an app by what it connected
		// as. Neither is the user's to rewrite.
		serval.Named(rowFixed, r.self || r.app != ""),
		serval.Named(rowSeq, seq),
	}
}

// connectionsRecords is the two levels as rows: the top level, and every app of
// every client.
func connectionsRecords(rows []connectionsRow) (peers, apps []serval.Row) {
	for i, r := range rows {
		peers = append(peers, serval.NewRow(rowKeyOf(r), connectionsFields(r, i)))
		for j, c := range r.children {
			fields := append(connectionsFields(c, j), serval.Named(rowPeer, c.identity))
			apps = append(apps, serval.NewRow(rowKeyOf(c), fields))
		}
	}
	return peers, apps
}

// connectionsTree states the source the window's tree reads.
//
// Each level is ordered by `seq`, which is the order the two stores put the rows
// in. A click on a header replaces that, per kind, and the view is what says so.
func connectionsTree(peers, apps *serval.ListSource) (*serval.TreeSource, error) {
	return serval.NewTreeSource(serval.TreeOptions{
		Source: peers,
		Spec:   &serval.Spec{Sort: []serval.SortLevel{{Field: rowSeq}}},
		Types: serval.NodeTypes{
			Default: &serval.NodeType{Then: serval.Always(appsKind)},
			Named: map[string]*serval.NodeType{
				appsKind: {
					Source: apps,
					Children: serval.Sorted(serval.ChildrenByKey(rowPeer),
						serval.SortLevel{Field: rowSeq}),
				},
			},
		},
	})
}

// connectionsMap is what a row of either kind puts in the columns: the caption,
// and whether it may be renamed.
//
// The six data columns are not here, each field being named after the column it
// fills -- which is the identity rung, and is the whole reason this is two lines
// rather than a table.
func connectionsMap() trinkets.NodeMap {
	return trinkets.NodeMap{
		Caption:  trinkets.CellMap{Value: rowName},
		ReadOnly: rowFixed,
	}
}

// readRows states the sources and gives them to the tree, with everything open.
//
// Everything, because a client's apps were drawn expanded from the start and the
// window is two levels deep: what a collapsed client would hide is one app row.
func (v *connectionsView) readRows(rows []connectionsRow) bool {
	v.rows = rows
	peers, apps := connectionsRecords(rows)
	v.peers = serval.NewListSource(peers)
	v.apps = serval.NewListSource(apps)

	made, err := connectionsTree(v.peers, v.apps)
	if err != nil {
		return false
	}
	v.made = made
	v.tree.SetKindMap("", connectionsMap())
	v.tree.SetKindMap(appsKind, connectionsMap())
	v.tree.SetSource(made)
	made.ExpandAll()
	v.tree.Reread()
	return true
}

// told says the rows have changed, and is the whole of what a mutation costs.
//
// **Three sayings at three doors, and not one poll.** The lists are restated, the
// tree that flattens them is told they are stale, and the view that holds the
// sequence reads it again. Nothing here works out that anything has changed:
// whoever changed the row is the one who knows, and this is them saying so.
//
// The sequence is not re-stated, so the items the rows lead to keep the pointers
// the selection and the row editor hold.
func (v *connectionsView) told() {
	peers, apps := connectionsRecords(v.rows)
	v.peers.Restate(peers)
	v.apps.Restate(apps)
	v.made.Stale()
	v.tree.Reread()
	v.d.RequestUpdate()
}

// rowOf is what a tree row stands for.
//
// **By the row's KEY and not by `Data`.** The items are the tree's own, made to
// draw a source's rows, so there was never a moment for the window to have hung
// anything on one -- and a row that comes and goes as a subtree closes and opens
// would have to be given it again each time. The key is the same string on both
// sides of that.
func (v *connectionsView) rowOf(item *trinkets.TreeItem) *connectionsRow {
	if item == nil {
		return nil
	}
	return v.byKey[item.Key()]
}

// byKey indexes the rows the way the source keys them, so a tree row leads back
// to the client or app it is about.
func (v *connectionsView) index() {
	v.byKey = map[string]*connectionsRow{}
	for i := range v.rows {
		r := &v.rows[i]
		v.byKey[serval.Key(rowKeyOf(*r))] = r
		for j := range r.children {
			c := &r.children[j]
			v.byKey[serval.Key(rowKeyOf(*c))] = c
		}
	}
}
