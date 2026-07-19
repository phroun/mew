package display

// The interactive approval prompt: a small window the host shows when a
// non-local client connects. It is built from protocol text - the same
// wire language clients use - then its buttons are wired to deliver one
// of the six authorization outcomes. Because the host is the only user
// interface, this prompt IS the "who may connect" gate.

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/protocol"
)

// DefaultConfig builds a desktop host's Config: the given endpoint, an
// optional shared token from $KITTYTK_TOKEN (headless bypass), and an
// interactive on-desktop approval prompt for non-local connections. Set
// $KITTYTK_PROMPT_LOCAL=1 to also prompt for local (unix/loopback)
// connections - handy for trying the prompt on one machine.
func DefaultConfig(desktop *trinkets.Desktop, endpoint string) Config {
	return Config{
		Endpoint:    endpoint,
		Token:       os.Getenv("KITTYTK_TOKEN"),
		Prompt:      NewDesktopAuthorizer(desktop),
		PromptLocal: envTruthy("KITTYTK_PROMPT_LOCAL"),
	}
}

func envTruthy(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// promptTimeout bounds how long an unanswered prompt waits before it is
// treated as a refusal (so a wedged connection can't pin a dialog open
// forever).
const promptTimeout = 2 * time.Minute

// NewDesktopAuthorizer returns an Authorizer that asks the user on the
// desktop d. Prompts are serialized (one at a time) and answered by
// clicking one of the six outcome buttons; an unanswered prompt refuses
// after promptTimeout. Wire it into a host via Config.Prompt.
func NewDesktopAuthorizer(d *trinkets.Desktop) Authorizer {
	var mu sync.Mutex // one prompt on screen at a time
	return func(req AuthRequest) AuthDecision {
		mu.Lock()
		defer mu.Unlock()

		result := make(chan AuthDecision, 1)
		d.Post(func() {
			if !showAuthPrompt(d, req, func(dec AuthDecision) {
				select {
				case result <- dec:
				default:
				}
			}) {
				select {
				case result <- AuthDenyOnce:
				default:
				}
			}
		})
		select {
		case dec := <-result:
			return dec
		case <-time.After(promptTimeout):
			return AuthDenyOnce
		}
	}
}

// authPromptScript is the protocol text for the prompt window. The six
// buttons carry surfaced names the wiring looks up by id.
func authPromptScript(req AuthRequest) string {
	who := req.Fingerprint
	if who == "" {
		who = req.RemoteAddr
	}
	if who == "" {
		who = "(unknown)"
	}
	q := fmt.Sprintf("Accept connection for %s?", quotedApp(req.AppName))
	from := fmt.Sprintf("from %s  [%s]", who, req.Transport)
	// Names bound inside children={} blocks are not surfaced in the
	// reply; the trailing top-level bindings re-surface each button by
	// its dotted path so the wiring can find it.
	return "" +
		"w=new window title=\"Connection Request\" width=470 height=250 children={\n" +
		"  root=new panel layout=vbox spacing=6 children={\n" +
		"    q=new label caption=" + protocol.Quote(q) + " wrap\n" +
		"    src=new label caption=" + protocol.Quote(from) + " wrap\n" +
		"    al=new label caption=\"Allow:\"\n" +
		"    arow=new panel layout=hbox spacing=6 children={\n" +
		"      b_once=new button caption=\"Once Only\"\n" +
		"      b_always=new button caption=\"Always\"\n" +
		"      b_all=new button caption=\"Always for All Apps\"\n" +
		"    }\n" +
		"    dl=new label caption=\"Deny:\"\n" +
		"    drow=new panel layout=hbox spacing=6 children={\n" +
		"      b_not=new button caption=\"Not Now\"\n" +
		"      b_never=new button caption=\"Never for this App\"\n" +
		"      b_block=new button caption=\"Block Client\"\n" +
		"    }\n" +
		"  }\n" +
		"}\n" +
		"once=w.root.arow.b_once\n" +
		"always=w.root.arow.b_always\n" +
		"all=w.root.arow.b_all\n" +
		"notnow=w.root.drow.b_not\n" +
		"never=w.root.drow.b_never\n" +
		"block=w.root.drow.b_block\n"
}

// quotedApp wraps an app name in quotes for display within a caption.
func quotedApp(s string) string {
	if s == "" {
		return "\"(unnamed app)\""
	}
	return "\"" + strings.ReplaceAll(s, "\"", "'") + "\""
}

// promptFactory records built targets by id so the wiring can reach the
// real buttons behind their surfaced names.
type promptFactory struct {
	inner protocol.Factory
	byID  map[uint64]any
}

func (f *promptFactory) New(typeName string) (protocol.Object, error) {
	o, err := f.inner.New(typeName)
	if err != nil {
		return nil, err
	}
	type built interface {
		Target() any
		ID() uint64
	}
	if b, ok := o.(built); ok {
		f.byID[b.ID()] = b.Target()
	}
	return o, nil
}
func (f *promptFactory) Subscribe(id uint64, typ string) {
	if ec, ok := f.inner.(protocol.EventControl); ok {
		ec.Subscribe(id, typ)
	}
}
func (f *promptFactory) Unsubscribe(id uint64, typ string) {
	if ec, ok := f.inner.(protocol.EventControl); ok {
		ec.Unsubscribe(id, typ)
	}
}
func (f *promptFactory) Suppressed(fn func()) {
	if ec, ok := f.inner.(protocol.EventControl); ok {
		ec.Suppressed(fn)
		return
	}
	fn()
}

// showAuthPrompt builds and displays the prompt on the desktop's UI
// thread. deliver is called once with the chosen outcome. It returns
// false if the dialog could not be constructed (the caller then
// refuses).
func showAuthPrompt(d *trinkets.Desktop, req AuthRequest, deliver func(AuthDecision)) bool {
	factory := &promptFactory{
		inner: protocol.NewRegistryFactory(&protocol.BindContext{}),
		byID:  make(map[uint64]any),
	}
	parsed, err := protocol.Parse(authPromptScript(req))
	if err != nil {
		return false
	}
	reply, err := protocol.NewSession().Execute(parsed, factory)
	if err != nil {
		return false
	}
	win, _ := factory.byID[reply.IDs["w"]].(*window.Window)
	if win == nil {
		return false
	}

	wm := d.WindowManager()
	if wm == nil {
		return false
	}

	var once sync.Once
	choose := func(dec AuthDecision) func() {
		return func() {
			once.Do(func() {
				// Tear the modal down BEFORE releasing the decision, so
				// the admitted connection can't start building its window
				// while this modal is still on the stack (ActivateWindow
				// refuses to front a window under a live modal, which
				// would leave the new window hidden).
				wm.CloseModal()
				deliver(dec)
			})
		}
	}
	wired := 0
	wire := func(name string, dec AuthDecision) {
		if b, ok := factory.byID[reply.IDs[name]].(*trinkets.Button); ok && b != nil {
			b.SetOnClick(choose(dec))
			wired++
		}
	}
	wire("once", AuthAllowOnce)
	wire("always", AuthAllowApp)
	wire("all", AuthAllowClient)
	wire("notnow", AuthDenyOnce)
	wire("never", AuthDenyApp)
	wire("block", AuthDenyClient)
	if wired != 6 {
		return false
	}

	// Let the window manager place it: positionWindow snaps to the
	// cell grid (clientArea origin + a cell-multiple cascade), so the
	// frame renders correctly in the TUI. Hand-computing a centered
	// pixel offset here lands the window between cells, which drops the
	// left border and titlebar in the terminal renderer.
	wm.ShowModal(win)
	d.RequestUpdate()
	return true
}
