package display

// The Connections window: who has been allowed to draw on this desktop.
//
// Every row is a client this desktop has either decided about or let in, named
// by its certificate fingerprint -- exact, permanent, and unreadable -- with
// the apps it has presented beneath it. The first column is a nickname the user
// writes, which is the only part of this the user chooses; everything else the
// row says is read from the two stores behind it.
//
// The pane below the list is where a row is read and answered: what it is,
// what standing it has, and the one destructive thing that can be done to it.
// Both are settings, not reports -- choosing in the pane rewrites the store.
//
// Host-side only, and built from protocol text like the approval prompt: no
// application asked for it, none can see it, and none can be told it is open.

import (
	"crypto/tls"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/protocol"
)

// hostFingerprint reads this host's own identity without creating one. A
// desktop that has never served a tls:// connection has no certificate yet,
// and opening a window is not a reason to generate one.
func hostFingerprint() string {
	pemBytes, err := os.ReadFile(hostIdentityPath())
	if err != nil {
		return ""
	}
	cert, err := tls.X509KeyPair(pemBytes, pemBytes)
	if err != nil || len(cert.Certificate) == 0 {
		return ""
	}
	return fingerprintSHA256(cert.Certificate[0])
}

// connectionsRow is one line of the tree: a peer, or an app beneath one.
type connectionsRow struct {
	name     string // what the name column shows
	stamp    string // the whole moment this row was last here, RFC 3339
	identity string // the client this row is about; empty on this host's row
	app      string // non-empty on an app row
	self     bool   // this host, which is not a client of itself
	allow    bool   // a standing allow at this row's scope
	deny     bool   // a standing deny at this row's scope
	hostSafe string // the folder this row's client keeps its material in
	appSafe  string // the folder within it, on an app row
	bytes    int64  // what it is taking up across both trees
	children []connectionsRow
}

// measure reads what this row is taking up on disk. This host stores nothing
// for itself, and a client that has never been admitted has no folder to look
// in -- neither is nothing measured, they are nothing to measure.
func (r *connectionsRow) measure() {
	if r.self || r.hostSafe == "" {
		r.bytes = 0
		return
	}
	r.bytes = storageUsed(r.hostSafe, r.appSafe)
}

// permWord is what the Permission column says: the standing this row is at, in
// the same words the pane below offers for it. This host has no standing to be
// at, being the one granting them.
func permWord(row connectionsRow) string {
	switch {
	case row.self:
		return ""
	case row.app != "":
		return appChoices[standingOf(row)]
	}
	return hostChoices[standingOf(row)]
}

// identityWord is what the Identity column says. A fingerprint belongs to a
// client: an app has no identity of its own and is trusted through the client
// that presented it.
func identityWord(row connectionsRow) string {
	if row.app != "" {
		return ""
	}
	return row.identity
}

// storageWord is what the Storage column says: what this row is taking up
// across both trees, or nothing when it has never stored anything.
func storageWord(row connectionsRow) string { return storageSize(row.bytes) }

// The two columns nobody sees. A size is shown abbreviated and a date is shown
// as a day, and sorting on either of those sorts on the wrong thing: 9K would
// come after 10M, and two visits an hour apart would be one value. So each of
// the shown columns hands its sorting to a hidden one holding what it was
// abbreviated FROM -- the whole moment, and the byte count.
func stampWord(row connectionsRow) string { return row.stamp }

func bytesWord(row connectionsRow) string { return strconv.FormatInt(row.bytes, 10) }

// seenWord is what the Last Seen column shows: the day, which is as fine as
// anyone reads a list of clients by.
func seenWord(row connectionsRow) string { return seenDate(row.stamp) }

// connectionsRows gathers what the window shows: this host first, then every
// client this desktop knows of.
//
// Two stores answer that, and neither alone is the list. The authorizations
// store holds the clients there is a RULE about, which includes ones blocked
// before they ever drew anything; the known-clients store holds the ones that
// have been ADMITTED, which includes ones let in for a session only and so
// carrying no rule at all. A client in either belongs in the window.
func connectionsRows(store *authStore, names map[string]string, known []knownRecord) []connectionsRow {
	self := hostFingerprint()
	if self == "" {
		self = "(no identity yet -- none has been needed)"
	}
	rows := []connectionsRow{{name: "This Host", identity: self, self: true}}

	ruled := store.entries()
	byID := map[string]authEntry{}
	for _, e := range ruled {
		byID[e.identity] = e
	}
	hosts, apps := indexKnown(known)
	for _, identity := range clientOrder(ruled, known) {
		e := byID[identity]
		r := connectionsRow{
			identity: identity,
			allow:    e.allow,
			deny:     e.deny,
			hostSafe: hosts[identity].safe,
			stamp:    hosts[identity].seen,
			name:     "(unnamed)",
		}
		if n := names[identity]; n != "" {
			r.name = n
		}
		r.measure()
		for _, name := range appOrder(e, apps[identity]) {
			a := e.app(name)
			child := connectionsRow{
				name:     name,
				identity: identity,
				app:      name,
				allow:    a.allow,
				deny:     a.deny,
				hostSafe: r.hostSafe,
				appSafe:  apps[identity][name].safe,
				stamp:    apps[identity][name].seen,
			}
			child.measure()
			r.children = append(r.children, child)
		}
		rows = append(rows, r)
	}
	return rows
}

// indexKnown sorts the records into what they are about: one per client, and
// one per app of a client keyed by the name it connected as.
func indexKnown(known []knownRecord) (map[string]knownRecord, map[string]map[string]knownRecord) {
	hosts := map[string]knownRecord{}
	apps := map[string]map[string]knownRecord{}
	for _, r := range known {
		if r.app == "" {
			hosts[r.identity] = r
			continue
		}
		if apps[r.identity] == nil {
			apps[r.identity] = map[string]knownRecord{}
		}
		apps[r.identity][r.app] = r
	}
	return hosts, apps
}

// clientOrder is every client either store knows of, once each: those with a
// rule in the order they were decided about, then those without one in the
// order they were first admitted.
func clientOrder(ruled []authEntry, known []knownRecord) []string {
	var order []string
	at := map[string]bool{}
	for _, e := range ruled {
		if !at[e.identity] {
			at[e.identity] = true
			order = append(order, e.identity)
		}
	}
	for _, r := range known {
		if r.app == "" && !at[r.identity] {
			at[r.identity] = true
			order = append(order, r.identity)
		}
	}
	return order
}

// appOrder is every app of one client, from either store, in name order -- the
// only order that means anything, since one store holds the ones ruled about
// and the other the ones merely admitted.
func appOrder(e authEntry, known map[string]knownRecord) []string {
	var order []string
	at := map[string]bool{}
	for _, a := range e.apps {
		if !at[a.name] {
			at[a.name] = true
			order = append(order, a.name)
		}
	}
	for name := range known {
		if !at[name] {
			at[name] = true
			order = append(order, name)
		}
	}
	sort.Strings(order)
	return order
}

// The five columns, in units -- eight to a character cell.
//
// The four short ones are PINNED and the fingerprint SCROLLS, rather than all
// five being squeezed to the window. Squeezing has to take the space from
// somewhere, and with columns of unequal worth there is no ratio that reads
// well at every window size: a name, a date, a size and a standing are short
// and must be whole, the fingerprint is seventy-one characters and will not fit
// whatever it is given.
//
// So the short ones are held outside the scrolling region at widths that show
// them, and the fingerprint is given room for all of itself and left to scroll
// -- which is the one arrangement where nothing has to be cut short to suit
// anything else.
// Two more columns are declared and never drawn: they hold what the shown date
// and the shown size were abbreviated from, and each shown column sorts on its
// hidden one. Their indices are into the data columns in declaration order,
// which is what a column's sortproxy names.
const (
	nicknameWidth = 120 // pinned: 15 cells
	lastSeenWidth = 96  // pinned: 12 cells, enough for YYYY-MM-DD
	storageWidth  = 72  // pinned: 9 cells, enough for 1023b
	permWidth     = 136 // pinned: 17 cells, enough for Follow Host Rule
	identityWidth = 600 // scrolls: 75 cells, enough for sha256:<64 hex>
	pinnedColumns = 4   // everything but the fingerprint, counted from where the run begins

	stampColumn = 4 // the whole moment behind the Last Seen day
	bytesColumn = 5 // the byte count behind the Storage figure
)

// The three standings offered in the pane, in the order they are shown:
// refuse, defer, admit. A client defers to being asked again; an app defers to
// whatever its client was granted.
var (
	hostChoices = [3]string{"Blocked", "Prompt", "Allow All"}
	appChoices  = [3]string{"Deny", "Follow Host Rule", "Allow"}
	choiceRules = [3]string{ruleDeny, ruleNone, ruleAllow}
)

// connectionsHost is the server the window sits in front of: the two policies
// the pane above the list turns on and off, and where each of them came from.
// A window built without one shows them disabled, there being nothing for them
// to change.
type connectionsHost interface {
	PreTrustedOnly() bool
	PromptLocal() bool
	SetPolicy(name string, on bool)
	PolicyOrigin(name string) string
}

// originPhrase is the note beside a switch: where its value came from, or that
// nothing outside this session is holding it.
func originPhrase(origin string) string {
	if strings.TrimSpace(origin) == "" {
		return "(this session)"
	}
	return "(" + origin + ")"
}

// connectionsShellScript is the window: a pane, the tree, and the pane that
// reads whichever row is current. The rows go in afterwards, so their ids can
// be surfaced and their cells filled by the same two-batch pattern a client
// would use.
func connectionsShellScript() string {
	return "" +
		"w=new window title=\"Connections\" width=640 height=440 children={\n" +
		"  root=new panel layout=vbox spacing=0 children={\n" +
		"    top=new panel layout=vbox spacing=0 children={\n" +
		"      trow=new panel layout=hbox spacing=8 children={\n" +
		"        trusted=new checkbox caption=\"Allow Previously Trusted Clients Only\"\n" +
		"        tfrom=new label caption=\"\"\n" +
		"      }\n" +
		"      lrow=new panel layout=hbox spacing=8 children={\n" +
		"        loopback=new checkbox caption=\"Automatically Approve Loopback Connections\"\n" +
		"        lfrom=new label caption=\"\"\n" +
		"      }\n" +
		"    }\n" +
		"    tv=new treeview stretch=1 caption=\"Nickname\" showheader treelines" +
		" editable !fit_width fixed_begin=" + strconv.Itoa(pinnedColumns) +
		" key_width=" + strconv.Itoa(nicknameWidth) + " columns={\n" +
		"      sc=new column id=lastseen caption=\"Last Seen\" sortable sortproxy=" +
		strconv.Itoa(stampColumn) + " width=" + strconv.Itoa(lastSeenWidth) + "\n" +
		// A size reads from its last digit, so the sizes line up on the edge
		// the column ends at, whichever way the column reads.
		"      stc=new column id=storage caption=\"Storage\" align=layoutopposite" +
		" sortable sortproxy=" + strconv.Itoa(bytesColumn) +
		" width=" + strconv.Itoa(storageWidth) + "\n" +
		"      pc=new column id=permission caption=\"Permission\" width=" +
		strconv.Itoa(permWidth) + "\n" +
		"      idc=new column id=identity caption=\"Identity\" width=" +
		strconv.Itoa(identityWidth) + "\n" +
		// Declared last so the four shown ones keep the indices the pinning
		// counts, and never drawn: they exist to be sorted on.
		"      stampc=new column id=stamp hidden\n" +
		"      bytesc=new column id=bytes hidden numeric\n" +
		"    }\n" +
		"    bottom=new panel layout=grid columns={\n" +
		"      new band id=body stretch=1\n" +
		"    } children={\n" +
		"      subject=new label caption=\"Identity:\" row=0\n" +
		"      value=new label caption=\"\" row=1\n" +
		"      permhead=new label caption=\"Permission:\" row=2\n" +
		"      permrow=new panel layout=hbox spacing=8 row=3 children={\n" +
		"        c0=new radiobutton caption=" + protocol.Quote(hostChoices[0]) + " group=standing\n" +
		"        c1=new radiobutton caption=" + protocol.Quote(hostChoices[1]) + " group=standing checked\n" +
		"        c2=new radiobutton caption=" + protocol.Quote(hostChoices[2]) + " group=standing\n" +
		"      }\n" +
		"      acthead=new label caption=\"Actions:\" row=4\n" +
		"      actrow=new panel layout=hbox spacing=8 row=5 children={\n" +
		"        forget=new button caption=\"Forget\"\n" +
		"        clearcache=new button caption=\"Clear Cache\"\n" +
		"      }\n" +
		"    }\n" +
		"  }\n" +
		"}\n" +
		"tree=w.root.tv\n" +

		"trusted=w.root.top.trow.trusted\n" +
		"trustedfrom=w.root.top.trow.tfrom\n" +
		"loopback=w.root.top.lrow.loopback\n" +
		"loopbackfrom=w.root.top.lrow.lfrom\n" +
		"seencol=w.root.tv.sc\n" +
		"storagecol=w.root.tv.stc\n" +
		"stampcol=w.root.tv.stampc\n" +
		"bytescol=w.root.tv.bytesc\n" +
		"permcol=w.root.tv.pc\n" +
		"col=w.root.tv.idc\n" +
		"subject=w.root.bottom.subject\n" +
		"value=w.root.bottom.value\n" +
		"permhead=w.root.bottom.permhead\n" +
		"permrow=w.root.bottom.permrow\n" +
		"acthead=w.root.bottom.acthead\n" +
		"actrow=w.root.bottom.actrow\n" +
		"choice0=w.root.bottom.permrow.c0\n" +
		"choice1=w.root.bottom.permrow.c1\n" +
		"choice2=w.root.bottom.permrow.c2\n" +
		"forget=w.root.bottom.actrow.forget\n" +
		"clearcache=w.root.bottom.actrow.clearcache\n"
}

// editableFlag is what a row says about being written in.
func editableFlag(editable bool) string {
	if editable {
		return ""
	}
	return " !editable"
}

// connectionsItemsScript builds the rows, binding a surfaced name to each so
// the cell values can be addressed against them.
func connectionsItemsScript(rows []connectionsRow) string {
	var sb strings.Builder
	sb.WriteString("set tree items={\n")
	for i, r := range rows {
		// Only a client carries a nickname. This host is named by its
		// certificate and an app by the name it connected under, so those rows
		// are held out of the editor rather than taking a rename that is then
		// quietly dropped.
		fmt.Fprintf(&sb, "  r%d=new item%s caption=%s", i, editableFlag(r.identity != "" && !r.self), protocol.Quote(r.name))
		if len(r.children) > 0 {
			sb.WriteString(" expanded items={\n")
			for j, c := range r.children {
				fmt.Fprintf(&sb, "    r%dc%d=new item !editable caption=%s\n",
					i, j, protocol.Quote(c.name))
			}
			sb.WriteString("  }")
		}
		sb.WriteString("\n")
	}
	sb.WriteString("}\n")
	// Re-surface every row by its path: names bound inside a children block
	// are not returned, and the cells below are keyed by item id.
	for i, r := range rows {
		fmt.Fprintf(&sb, "i%d=tree.r%d\n", i, i)
		for j := range r.children {
			fmt.Fprintf(&sb, "i%dc%d=tree.r%d.r%dc%d\n", i, j, i, i, j)
		}
	}
	return sb.String()
}

// connectionsCellsScript fills every data column from the rows, addressing
// each cell by the item id the items batch surfaced.
func connectionsCellsScript(rows []connectionsRow, ids map[string]uint64) string {
	var sb strings.Builder
	for _, col := range []struct {
		name  string
		value func(connectionsRow) string
	}{
		{"seencol", seenWord},
		{"storagecol", storageWord},
		{"permcol", permWord},
		{"col", identityWord},
		{"stampcol", stampWord},
		{"bytescol", bytesWord},
	} {
		fmt.Fprintf(&sb, "set %s children={\n", col.name)
		for i, r := range rows {
			if id, ok := ids[fmt.Sprintf("i%d", i)]; ok {
				fmt.Fprintf(&sb, "  new cell item=%d value=%s\n",
					id, protocol.Quote(col.value(r)))
			}
			for j, c := range r.children {
				if id, ok := ids[fmt.Sprintf("i%dc%d", i, j)]; ok {
					fmt.Fprintf(&sb, "  new cell item=%d value=%s\n",
						id, protocol.Quote(col.value(c)))
				}
			}
		}
		sb.WriteString("}\n")
	}
	return sb.String()
}

// connectionsView is the window while it is open: the trinkets the pane writes
// into, and the stores a choice in it rewrites.
type connectionsView struct {
	d     *trinkets.Desktop
	host  connectionsHost
	store *authStore
	nicks *pairStore
	known *knownStore

	tree         *trinkets.TreeView
	trusted      *trinkets.Checkbox // admit only clients already decided about
	trustedFrom  *trinkets.Label    // where that value came from
	loopback     *trinkets.Checkbox // admit same-machine clients without asking
	loopbackFrom *trinkets.Label
	subject      *trinkets.Label // "Identity:" or "App Name:"
	value        *trinkets.Label // the fingerprint, or the app's name
	permhead     *trinkets.Label
	permrow      core.Trinket
	acthead      *trinkets.Label
	actrow       core.Trinket
	choices      [3]*trinkets.RadioButton
	forget       *trinkets.Button
	clear        *trinkets.Button

	// answering is set while the window is writing its own controls to match
	// what it found, so what it moves is not read back as the user choosing.
	answering bool
}

// showPolicies puts the two switches above the list where the server has them,
// and hands a window with no server behind it switches that cannot be thrown.
func (v *connectionsView) showPolicies() {
	v.answering = true
	defer func() { v.answering = false }()
	if v.host == nil {
		v.trusted.SetEnabled(false)
		v.loopback.SetEnabled(false)
		v.trustedFrom.SetText("")
		v.loopbackFrom.SetText("")
		return
	}
	v.trusted.SetChecked(v.host.PreTrustedOnly())
	// The switch is offered the way a user thinks of it -- loopback is waved
	// through -- and the server holds the question it answers, which is
	// whether to ask about a local client at all.
	v.loopback.SetChecked(!v.host.PromptLocal())
	v.trustedFrom.SetText(originPhrase(v.host.PolicyOrigin(PolicyPreTrustedOnly)))
	v.loopbackFrom.SetText(originPhrase(v.host.PolicyOrigin(PolicyPromptLocal)))
}

// policyChosen carries a thrown switch to the server, and puts back beside it
// wherever the server says the change was kept.
func (v *connectionsView) policyChosen(name string, on bool) {
	if v.answering || v.host == nil {
		return
	}
	v.host.SetPolicy(name, on)
	v.answering = true
	defer func() { v.answering = false }()
	switch name {
	case PolicyPreTrustedOnly:
		v.trustedFrom.SetText(originPhrase(v.host.PolicyOrigin(name)))
	case PolicyPromptLocal:
		v.loopbackFrom.SetText(originPhrase(v.host.PolicyOrigin(name)))
	}
	v.d.RequestUpdate()
}

// rowOf is what a tree row stands for, hung on the item itself.
func rowOf(item *trinkets.TreeItem) *connectionsRow {
	if item == nil {
		return nil
	}
	r, _ := item.Data.(*connectionsRow)
	return r
}

// show writes the pane to match a row: what it is, where it stands, and
// whether there is anything to be done to it. This host is us -- there is no
// standing to grant ourselves and nothing to forget -- so only its identity is
// shown.
func (v *connectionsView) show(item *trinkets.TreeItem) {
	row := rowOf(item)
	v.answering = true
	defer func() { v.answering = false }()

	answerable := row != nil && !row.self
	v.permhead.SetVisible(answerable)
	v.permrow.SetVisible(answerable)
	v.acthead.SetVisible(answerable)
	v.actrow.SetVisible(answerable)

	switch {
	case row == nil:
		v.subject.SetText("")
		v.value.SetText("")
	case row.app != "":
		v.subject.SetText("App Name:")
		v.value.SetText(row.app)
	default:
		v.subject.SetText("Identity:")
		v.value.SetText(row.identity)
	}
	if !answerable {
		return
	}

	captions := hostChoices
	if row.app != "" {
		captions = appChoices
	}
	for i, c := range v.choices {
		c.SetText(captions[i])
	}
	v.choices[standingOf(*row)].SetChecked(true)
}

// standingOf is which of the three the row sits at: refused, deferred, or
// admitted. A deny outranks an allow, which is how the gate reads the same
// two lines.
func standingOf(row connectionsRow) int {
	switch {
	case row.deny:
		return 0
	case row.allow:
		return 2
	}
	return 1
}

// choose puts the current row at a standing and writes it to the store. The
// row's Permission cell says what the store now holds, so the list and the pane
// cannot disagree about it.
func (v *connectionsView) choose(at int) {
	item := v.tree.CurrentItem()
	row := rowOf(item)
	if row == nil || row.self || at < 0 || at >= len(choiceRules) {
		return
	}
	rule := choiceRules[at]
	if row.app != "" {
		if err := v.store.setAppRule(row.identity, row.app, rule); err != nil {
			return
		}
	} else if err := v.store.setClientRule(row.identity, rule); err != nil {
		return
	}
	row.deny = rule == ruleDeny
	row.allow = rule == ruleAllow
	item.SetValue("permission", permWord(*row))
	v.tree.Update()
	v.d.RequestUpdate()
}

// clearCurrentCache throws away what the current row has cached and puts the
// new figure in its Storage cell. The client's own row is refreshed too when an
// app's cache goes, since a client's figure counts its apps.
func (v *connectionsView) clearCurrentCache() {
	item := v.tree.CurrentItem()
	row := rowOf(item)
	if row == nil || row.self || row.hostSafe == "" {
		return
	}
	if err := clearCache(row.hostSafe, row.appSafe); err != nil {
		return
	}
	showSize(item, row)
	if parent := item.Parent; parent != nil {
		if up := rowOf(parent); up != nil {
			showSize(parent, up)
		}
	}
	v.tree.Update()
	v.d.RequestUpdate()
}

// showSize measures a row again and puts the figure in both the cell that shows
// it and the hidden one it sorts by.
func showSize(item *trinkets.TreeItem, row *connectionsRow) {
	row.measure()
	item.SetValue("storage", storageWord(*row))
	item.SetValue("bytes", bytesWord(*row))
}

// forgetCurrent drops the current row from the store and from the list. A
// client is forgotten whole: its apps, the name the user gave it, and when it
// was last here all go with it, since none of them is worth keeping about a
// client this desktop no longer knows.
func (v *connectionsView) forgetCurrent() {
	item := v.tree.CurrentItem()
	row := rowOf(item)
	if row == nil || row.self {
		return
	}
	if row.app != "" {
		if err := v.store.forgetApp(row.identity, row.app); err != nil {
			return
		}
		_ = v.known.forgetApp(row.identity, row.app)
	} else {
		if err := v.store.forget(row.identity); err != nil {
			return
		}
		_ = v.known.forget(row.identity)
		_ = v.nicks.set(row.identity, "")
	}
	v.tree.RemoveItem(item)
	v.show(v.tree.CurrentItem())
	v.d.RequestUpdate()
}

// rename records the name a user typed over a row. Any row that is not a
// client -- this host, an app beneath a client -- has no name to give, so its
// caption is put back rather than quietly kept.
//
// The folder the client's material sits in is named after the nickname, so a
// new nickname moves it. The move happens first: renaming the record and
// leaving the folder would point every later read at a folder that is not
// there, and the material would read as lost.
func (v *connectionsView) rename(item *trinkets.TreeItem, value string) {
	row := rowOf(item)
	if row == nil || row.self || row.app != "" {
		return
	}
	nickname := strings.TrimSpace(value)
	_ = v.nicks.set(row.identity, nickname)
	row.name = nickname
	if row.name == "" {
		row.name = "(unnamed)"
		item.Text = row.name
		v.tree.Update()
	}
	v.refile(row, nickname)
}

// refile moves a renamed client's folder to match the name it now has, and
// carries the new folder name onto the rows that read from it. The name it is
// filed under comes from the NICKNAME rather than from the row's caption: a
// client whose name was cleared reads as "(unnamed)" on screen, and filing it
// under `unnamed` would be filing it under a word nobody typed.
func (v *connectionsView) refile(row *connectionsRow, nickname string) {
	from, to, err := v.known.rename(row.identity, nickname)
	if err != nil || from == to {
		return
	}
	if err := renameStorage(from, to); err != nil {
		return
	}
	row.hostSafe = to
	for i := range row.children {
		row.children[i].hostSafe = to
	}
}

// showConnections builds and shows the window, returning it so its caller can
// tell whether one is already up. Runs on the desktop's own thread.
func showConnections(d *trinkets.Desktop, host connectionsHost, store *authStore, nicks *pairStore, known *knownStore) *window.Window {
	wm := d.WindowManager()
	if wm == nil {
		return nil
	}
	_, win := buildConnections(d, host, store, nicks, known)
	if win == nil {
		return nil
	}
	sizeAndShow(d, wm, win)
	return win
}

// buildConnections builds the window and wires it to the stores, up to but not
// including putting it on the desktop.
func buildConnections(d *trinkets.Desktop, host connectionsHost, store *authStore, nicks *pairStore, known *knownStore) (*connectionsView, *window.Window) {
	factory := &promptFactory{
		inner: protocol.NewRegistryFactory(&protocol.BindContext{}),
		byID:  make(map[uint64]any),
	}
	session := protocol.NewSession()

	shell, err := protocol.Parse(connectionsShellScript())
	if err != nil {
		return nil, nil
	}
	reply, err := session.Execute(shell, factory)
	if err != nil {
		return nil, nil
	}
	// Only what this needs a Go handle for. The protocol registry hands back
	// its own wrappers for a column and an item -- unexported, so nothing
	// outside the trinkets package can name their type -- and asking for one
	// by type simply fails. The columns are addressed by the names the script
	// bound them to instead, which is all the cell batch needs.
	win, _ := factory.byID[reply.IDs["w"]].(*window.Window)
	v := &connectionsView{d: d, host: host, store: store, nicks: nicks, known: known}
	v.tree, _ = factory.byID[reply.IDs["tree"]].(*trinkets.TreeView)
	v.trusted, _ = factory.byID[reply.IDs["trusted"]].(*trinkets.Checkbox)
	v.trustedFrom, _ = factory.byID[reply.IDs["trustedfrom"]].(*trinkets.Label)
	v.loopback, _ = factory.byID[reply.IDs["loopback"]].(*trinkets.Checkbox)
	v.loopbackFrom, _ = factory.byID[reply.IDs["loopbackfrom"]].(*trinkets.Label)
	v.subject, _ = factory.byID[reply.IDs["subject"]].(*trinkets.Label)
	v.value, _ = factory.byID[reply.IDs["value"]].(*trinkets.Label)
	v.permhead, _ = factory.byID[reply.IDs["permhead"]].(*trinkets.Label)
	v.acthead, _ = factory.byID[reply.IDs["acthead"]].(*trinkets.Label)
	v.permrow, _ = factory.byID[reply.IDs["permrow"]].(core.Trinket)
	v.actrow, _ = factory.byID[reply.IDs["actrow"]].(core.Trinket)
	v.forget, _ = factory.byID[reply.IDs["forget"]].(*trinkets.Button)
	v.clear, _ = factory.byID[reply.IDs["clearcache"]].(*trinkets.Button)
	for i := range v.choices {
		v.choices[i], _ = factory.byID[reply.IDs[fmt.Sprintf("choice%d", i)]].(*trinkets.RadioButton)
	}
	if win == nil || !v.complete() {
		return nil, nil
	}

	rows := connectionsRows(store, nicks.all(), known.all())
	items, err := protocol.Parse(connectionsItemsScript(rows))
	if err != nil {
		return nil, nil
	}
	itemReply, err := session.Execute(items, factory)
	if err != nil {
		return nil, nil
	}
	cells, err := protocol.Parse(connectionsCellsScript(rows, itemReply.IDs))
	if err != nil {
		return nil, nil
	}
	if _, err := session.Execute(cells, factory); err != nil {
		return nil, nil
	}
	v.attach(rows)

	v.tree.SetOnCellEdited(func(item *trinkets.TreeItem, column *trinkets.TreeColumn, value string) {
		// The nickname is the key column: the row's own caption. A key edit
		// arrives under the tree's sentinel column rather than under nil, so
		// the tree is asked which one it was.
		if !v.tree.IsKeyColumn(column) {
			return
		}
		v.rename(item, value)
	})
	v.tree.SetOnCurrentChanged(func(item *trinkets.TreeItem) { v.show(item) })
	for i := range v.choices {
		at := i
		v.choices[i].SetOnToggled(func(checked bool) {
			if checked && !v.answering {
				v.choose(at)
			}
		})
	}
	v.forget.SetOnClick(v.forgetCurrent)
	v.clear.SetOnClick(v.clearCurrentCache)
	v.trusted.SetOnToggled(func(on bool) { v.policyChosen(PolicyPreTrustedOnly, on) })
	// The loopback switch says what the user wants; the server holds the
	// question that answers, which is the opposite one.
	v.loopback.SetOnToggled(func(on bool) { v.policyChosen(PolicyPromptLocal, !on) })
	v.showPolicies()
	v.show(v.tree.CurrentItem())
	return v, win
}

// complete reports whether every trinket the pane writes into was found. A
// window missing one of them would open with a pane that answers nothing, so
// it is not opened at all.
func (v *connectionsView) complete() bool {
	if v.tree == nil || v.trusted == nil || v.loopback == nil ||
		v.trustedFrom == nil || v.loopbackFrom == nil || v.subject == nil || v.value == nil || v.permhead == nil ||
		v.acthead == nil || v.permrow == nil || v.actrow == nil || v.forget == nil || v.clear == nil {
		return false
	}
	for _, c := range v.choices {
		if c == nil {
			return false
		}
	}
	return true
}

// attach hangs each row on the tree item that draws it, so a click on a row
// reaches the client it is about. The tree holds the items in the order the
// script built them, which is the order of the rows they came from.
func (v *connectionsView) attach(rows []connectionsRow) {
	items := v.tree.RootItems()
	for i := range rows {
		if i >= len(items) {
			return
		}
		row := &rows[i]
		items[i].Data = row
		for j := range row.children {
			if j < len(items[i].Children) {
				items[i].Children[j].Data = &row.children[j]
			}
		}
	}
}

// sizeAndShow gives the window a size to exist at and puts it on the desktop,
// centred. A window that starts at zero has nothing for its columns to measure
// against on the first frame, and one that covers the desktop is no use for
// looking at what is behind it.
func sizeAndShow(d *trinkets.Desktop, wm *window.WindowManager, win *window.Window) {
	metrics := d.EffectiveCellMetrics()
	area := wm.ClientArea()
	w := metrics.UnitsPerCellWidth * 84
	h := metrics.UnitsPerCellHeight * 24
	if area.Width > 0 {
		w = min(w, area.Width*3/4)
	}
	if area.Height > 0 {
		h = min(h, area.Height*3/4)
	}
	if !wm.SmoothPositioning() {
		aligned := metrics.AlignSize(core.UnitSize{Width: w, Height: h})
		w, h = aligned.Width, aligned.Height
	}
	win.SetBounds(core.UnitRect{Width: w, Height: h})
	wm.AddWindow(win)

	b := win.Bounds()
	x := area.X + (area.Width-b.Width)/2
	y := area.Y + (area.Height-b.Height)/2
	if !wm.SmoothPositioning() {
		x = metrics.RoundDownToCellX(x)
		y = metrics.RoundDownToCellY(y)
	}
	win.SetBounds(core.UnitRect{X: x, Y: y, Width: b.Width, Height: b.Height})
	wm.ActivateWindow(win)
	d.RequestUpdate()
}

// NewConnectionsOpener returns what the desktop's Connections menu item calls.
// Install it with Desktop.SetConnectionsOpener; without it the item is not
// offered, since a desktop with no display server has no connections to show.
func NewConnectionsOpener(d *trinkets.Desktop, host connectionsHost) func() {
	store := newAuthStore("")
	nicks := newNicknameStore("")
	known := newKnownStore("")
	// One window, not one per visit: a second copy would show the same store
	// twice and let a rename in one go stale in the other.
	var mu sync.Mutex
	var open *window.Window
	return func() {
		d.Post(func() {
			mu.Lock()
			showing := open
			mu.Unlock()
			if showing != nil {
				if wm := d.WindowManager(); wm != nil {
					wm.ActivateWindow(showing)
				}
				return
			}
			win := showConnections(d, host, store, nicks, known)
			if win == nil {
				return
			}
			mu.Lock()
			open = win
			mu.Unlock()
			win.AddOnClosed(func() {
				mu.Lock()
				if open == win {
					open = nil
				}
				mu.Unlock()
			})
		})
	}
}
