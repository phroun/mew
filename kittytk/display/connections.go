package display

// The Connections window: who has been allowed to draw on this desktop.
//
// It is the authorizations store made visible. Every row is a client the user
// has decided about, named by its certificate fingerprint -- exact, permanent,
// and unreadable -- with the apps it was approved for beneath it. The first
// column is a nickname the user writes, which is the only part of this the
// user chooses and the only part nothing depends on.
//
// Host-side only, and built from protocol text like the approval prompt: no
// application asked for it, none can see it, and none can be told it is open.

import (
	"crypto/tls"
	"fmt"
	"os"
	"sort"
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
	name     string // what the first column shows
	detail   string // what the second column shows
	identity string // non-empty only on a renameable peer row
	children []connectionsRow
}

// connectionsRows gathers what the window shows: this host first, then every
// client the store has decided about, each with the apps it names.
func connectionsRows(store *authStore, names map[string]string) []connectionsRow {
	self := hostFingerprint()
	if self == "" {
		self = "(no identity yet -- none has been needed)"
	}
	rows := []connectionsRow{{name: "This Host", detail: self}}

	for _, e := range store.entries() {
		r := connectionsRow{identity: e.identity, detail: e.identity}
		if n := names[e.identity]; n != "" {
			r.name = n
		} else {
			r.name = "(unnamed)"
		}
		switch {
		case e.deny:
			r.detail = e.identity + "  -- blocked"
		case e.allow:
			r.detail = e.identity + "  -- every app"
		}
		apps := append([]authEntryApp(nil), e.apps...)
		sort.Slice(apps, func(i, j int) bool { return apps[i].name < apps[j].name })
		for _, a := range apps {
			verdict := "denied"
			if a.allowed() {
				verdict = "allowed"
			}
			r.children = append(r.children, connectionsRow{name: a.name, detail: verdict})
		}
		rows = append(rows, r)
	}
	return rows
}

// connectionsShellScript is the window and the empty tree. The rows go in
// afterwards, so their ids can be surfaced and their second column filled by
// the same two-batch pattern a client would use.
func connectionsShellScript() string {
	return "" +
		"w=new window title=\"Connections\" width=640 height=340 children={\n" +
		"  root=new panel layout=vbox spacing=0 children={\n" +
		"    tv=new treeview caption=\"Nickname\" showheader treelines editable" +
		" key_width=200 columns={\n" +
		"      idc=new column id=identity caption=\"Identity\" width=560\n" +
		"    }\n" +
		"  }\n" +
		"}\n" +
		"tree=w.root.tv\n" +
		"col=w.root.tv.idc\n"
}

// connectionsItemsScript builds the rows, binding a surfaced name to each so
// the cell values can be addressed against them.
func connectionsItemsScript(rows []connectionsRow) string {
	var sb strings.Builder
	sb.WriteString("set tree items={\n")
	for i, r := range rows {
		fmt.Fprintf(&sb, "  r%d=new item caption=%s", i, protocol.Quote(r.name))
		if len(r.children) > 0 {
			sb.WriteString(" expanded items={\n")
			for j, c := range r.children {
				fmt.Fprintf(&sb, "    r%dc%d=new item caption=%s\n",
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

// showConnections builds and shows the window, returning it so its caller can
// tell whether one is already up. Runs on the desktop's own thread.
func showConnections(d *trinkets.Desktop, store *authStore, nicks *nicknameStore) *window.Window {
	wm := d.WindowManager()
	if wm == nil {
		return nil
	}

	factory := &promptFactory{
		inner: protocol.NewRegistryFactory(&protocol.BindContext{}),
		byID:  make(map[uint64]any),
	}
	session := protocol.NewSession()

	shell, err := protocol.Parse(connectionsShellScript())
	if err != nil {
		return nil
	}
	reply, err := session.Execute(shell, factory)
	if err != nil {
		return nil
	}
	// Only what this needs a Go handle for. The protocol registry hands back
	// its own wrappers for a column and an item -- unexported, so nothing
	// outside the trinkets package can name their type -- and asking for one
	// by type simply fails. The column is addressed by the name the script
	// bound it to instead, which is all the second batch needs.
	win, _ := factory.byID[reply.IDs["w"]].(*window.Window)
	tree, _ := factory.byID[reply.IDs["tree"]].(*trinkets.TreeView)
	if win == nil || tree == nil {
		return nil
	}

	rows := connectionsRows(store, nicks.all())
	items, err := protocol.Parse(connectionsItemsScript(rows))
	if err != nil {
		return nil
	}
	itemReply, err := session.Execute(items, factory)
	if err != nil {
		return nil
	}

	// The second column, and the map from a row back to the peer it names --
	// which is what says whether a rename means anything on that row. Keyed by
	// the wire id a protocol-built item keeps, since the row objects here are
	// the registry's wrappers rather than the tree's own items.
	renameable := map[core.ObjectID]string{}
	var cells strings.Builder
	cells.WriteString("set col children={\n")
	for i, r := range rows {
		if id, ok := itemReply.IDs[fmt.Sprintf("i%d", i)]; ok {
			fmt.Fprintf(&cells, "  new cell item=%d value=%s\n",
				id, protocol.Quote(r.detail))
			if r.identity != "" {
				renameable[core.ObjectID(id)] = r.identity
			}
		}
		for j, c := range r.children {
			if id, ok := itemReply.IDs[fmt.Sprintf("i%dc%d", i, j)]; ok {
				fmt.Fprintf(&cells, "  new cell item=%d value=%s\n",
					id, protocol.Quote(c.detail))
			}
		}
	}
	cells.WriteString("}\n")
	if parsed, err := protocol.Parse(cells.String()); err == nil {
		_, _ = session.Execute(parsed, factory)
	}

	// A rename writes the nickname store and nothing else. Any other row --
	// this host, an app beneath a peer -- has no identity to name, so its
	// caption is put back rather than quietly kept.
	tree.SetOnCellEdited(func(item *trinkets.TreeItem, column *trinkets.TreeColumn, value string) {
		if column != nil {
			return // only the key column carries the nickname
		}
		if item == nil {
			return
		}
		id, ok := renameable[item.ID]
		if !ok {
			return
		}
		_ = nicks.set(id, value)
		if strings.TrimSpace(value) == "" {
			item.Text = "(unnamed)"
			tree.Update()
		}
	})

	sizeAndShow(d, wm, win)
	return win
}

// sizeAndShow gives the window a size to exist at and puts it on the desktop,
// centred. A window that starts at zero has nothing for its columns to measure
// against on the first frame, and one that covers the desktop is no use for
// looking at what is behind it.
func sizeAndShow(d *trinkets.Desktop, wm *window.WindowManager, win *window.Window) {
	metrics := d.EffectiveCellMetrics()
	area := wm.ClientArea()
	w := metrics.UnitsPerCellWidth * 84
	h := metrics.UnitsPerCellHeight * 18
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
func NewConnectionsOpener(d *trinkets.Desktop) func() {
	store := newAuthStore("")
	nicks := newNicknameStore("")
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
			win := showConnections(d, store, nicks)
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
