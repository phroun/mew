// Package display is the display-service side of the D22 transport:
// a listener that accepts app connections speaking protocol text
// over a unix socket, giving each one a full Application in the
// desktop (menus, status bar, windows - peers of in-process apps).
//
// Threading (D21): socket readers parse batches off the wire and
// Post them onto the desktop's platform thread; everything that
// touches trinkets runs there. Event emission enqueues onto a
// per-connection writer goroutine, so the UI thread never blocks on
// a slow client.
package display

import (
	"crypto/tls"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/objects/app"
	"github.com/phroun/kittytk/objects/trinkets"
	"github.com/phroun/kittytk/objects/window"
	"github.com/phroun/kittytk/protocol"
	"github.com/phroun/kittytk/style"
)

// Config configures a display server's transport and authorization.
// The zero value plus an Endpoint is the plain unix-socket server.
type Config struct {
	// Endpoint selects the transport: a bare path or unix:/path (unix
	// socket), tcp://host:port (plaintext), or tls://host:port (TLS).
	Endpoint string

	// Token, if non-empty, admits any client presenting it in the
	// handshake (an automation bypass for headless hosts). It never
	// gates local (unix / loopback) connections.
	Token string

	// TLSConfig overrides the host certificate for tls:// endpoints. If
	// nil, a persistent self-signed identity is loaded or generated.
	TLSConfig *tls.Config

	// Authorize decides non-local connections (tests and custom hosts
	// supply this). If nil, Prompt is used; if both are nil, non-local
	// connections without a stored allow are refused.
	Authorize Authorizer

	// Prompt is the interactive approval used when Authorize is nil and
	// the persistent store has no rule (the desktop dialog).
	Prompt Authorizer

	// PromptLocal, when true, subjects local (unix / loopback)
	// connections to the same authorization as remote ones instead of
	// trusting them automatically (for shared machines).
	PromptLocal bool
}

// Server accepts display-protocol connections for one desktop.
type Server struct {
	desktop  *trinkets.Desktop
	listener net.Listener
	sessions atomic.Uint64
	closed   atomic.Bool

	endpoint    endpoint
	token       string
	store       *authStore
	authorize   Authorizer
	prompt      Authorizer
	promptLocal bool

	// preTrustedOnly, when set, auto-rejects any connection without an
	// existing stored allow instead of prompting (a lockdown mode the
	// Psi menu's "Pre-Trusted Clients Only" toggles at runtime).
	preTrustedOnly atomic.Bool

	// TLSFingerprint is the host certificate's sha256:<hex> for tls://
	// endpoints (what clients pin); empty otherwise.
	TLSFingerprint string
}

// SetPreTrustedOnly toggles lockdown: while true, connections that are
// not already in the trusted store are rejected without a prompt.
func (s *Server) SetPreTrustedOnly(v bool) { s.preTrustedOnly.Store(v) }

// PreTrustedOnly reports the lockdown state.
func (s *Server) PreTrustedOnly() bool { return s.preTrustedOnly.Load() }

// Serve listens on the unix socket at path (creating its directory,
// 0700) and serves connections until Close. Call from desktop wiring
// (e.g. SetOnStartup). This is ServeConfig with a unix Endpoint.
func Serve(desktop *trinkets.Desktop, path string) (*Server, error) {
	return ServeConfig(desktop, Config{Endpoint: path})
}

// ServeConfig is the general server: it selects the transport from
// cfg.Endpoint, wires TLS + authorization, and serves until Close.
func ServeConfig(desktop *trinkets.Desktop, cfg Config) (*Server, error) {
	ep := parseEndpoint(cfg.Endpoint)
	s := &Server{
		desktop:     desktop,
		endpoint:    ep,
		token:       cfg.Token,
		store:       newAuthStore(""),
		authorize:   cfg.Authorize,
		prompt:      cfg.Prompt,
		promptLocal: cfg.PromptLocal,
	}

	var ln net.Listener
	var err error
	switch {
	case ep.network == "unix":
		if err = os.MkdirAll(filepath.Dir(ep.address), 0o700); err != nil {
			return nil, err
		}
		_ = os.Remove(ep.address) // stale socket from a previous run
		ln, err = net.Listen("unix", ep.address)
	case ep.useTLS:
		tlsCfg := cfg.TLSConfig
		fp := ""
		if tlsCfg == nil {
			tlsCfg, fp, err = loadOrCreateHostTLS()
			if err != nil {
				return nil, err
			}
		} else {
			if len(tlsCfg.Certificates) > 0 && len(tlsCfg.Certificates[0].Certificate) > 0 {
				fp = fingerprintSHA256(tlsCfg.Certificates[0].Certificate[0])
			}
			if tlsCfg.ClientAuth == tls.NoClientCert {
				tlsCfg.ClientAuth = tls.RequireAnyClientCert
			}
		}
		s.TLSFingerprint = fp
		var raw net.Listener
		raw, err = net.Listen("tcp", ep.address)
		if err == nil {
			ln = tls.NewListener(raw, tlsCfg)
		}
	default: // plaintext tcp
		ln, err = net.Listen("tcp", ep.address)
	}
	if err != nil {
		return nil, err
	}
	s.listener = ln
	go s.acceptLoop()
	return s, nil
}

// Addr returns the listener address (the socket path or host:port).
func (s *Server) Addr() string { return s.listener.Addr().String() }

// Close stops accepting and closes the listener. Existing
// connections end when their sockets close.
func (s *Server) Close() error {
	s.closed.Store(true)
	return s.listener.Close()
}

func (s *Server) acceptLoop() {
	for {
		nc, err := s.listener.Accept()
		if err != nil {
			return // listener closed
		}
		go s.serveConn(nc)
	}
}

// conn is one app connection's display-side state.
type conn struct {
	server *Server
	nc     net.Conn

	session *protocol.Session
	factory *hostFactory
	app     *app.Application

	// solo marks a connection that asked (via the handshake) to be the
	// whole display: its main window replaces the desktop entirely.
	solo bool

	// Accessibility routing state (the "Show/Speak Announcements"
	// toggles): the connection owns the intent, and installs a desktop
	// OnAnnounce handler reflecting it. Speech serializes through
	// speechMu so a new utterance cancels the previous one.
	announceVisual bool
	announceSpeak  bool
	speechMu       sync.Mutex
	speechCmd      *exec.Cmd

	// outbound statements; the writer goroutine owns the socket's
	// write side.
	out chan string
}

func (s *Server) serveConn(nc net.Conn) {
	defer nc.Close()

	// A tls:// peer authenticates by certificate: complete the handshake
	// up front so its fingerprint is known before we admit it.
	req := AuthRequest{Transport: s.endpoint.transport(), Local: isLocalConn(nc)}
	if ra := nc.RemoteAddr(); ra != nil {
		req.RemoteAddr = ra.String()
	}
	if tc, ok := nc.(*tls.Conn); ok {
		if err := tc.Handshake(); err != nil {
			return
		}
		if certs := tc.ConnectionState().PeerCertificates; len(certs) > 0 {
			req.Fingerprint = fingerprintSHA256(certs[0].Raw)
		}
	}

	scanner := protocol.NewScanner(nc)

	// Handshake: first batch must open with hello (D22
	// reattach-ready: we assign a session id and return it).
	first, err := readBatch(scanner)
	if err != nil || len(first) == 0 || first[0].Verb != "hello" {
		fmt.Fprintf(nc, "%s\n", protocol.EncodeError("handshake: expected hello"))
		return
	}
	appName := "Remote App"
	solo := false
	multiWindow := false
	token := ""
	for _, a := range first[0].Args {
		if a.Name == "app" && a.Value != nil && a.Value.Kind == protocol.StringValue {
			appName = a.Value.Str
		}
		if a.Name == "solo" && a.Flag == protocol.FlagTrue {
			solo = true
		}
		if a.Name == "multiwindow" && a.Flag == protocol.FlagTrue {
			multiWindow = true
		}
		if a.Name == "token" && a.Value != nil && a.Value.Kind == protocol.StringValue {
			token = a.Value.Str
		}
	}
	req.AppName = appName

	// Authorize before granting the connection an Application.
	if !s.admit(req, token) {
		fmt.Fprintf(nc, "%s\n", protocol.EncodeError("connection refused"))
		return
	}
	sessionID := s.sessions.Add(1)

	c := &conn{
		server:  s,
		nc:      nc,
		session: protocol.NewSession(),
		solo:    solo,
		out:     make(chan string, 1024),
	}

	// Per-connection BindContext: events encode onto the wire.
	// Dispatch stays nil - command events ARE the dispatch path
	// across the seam (the app-side registry lives in the app).
	ctx := &protocol.BindContext{
		Emit: func(ev *protocol.Event) { c.send(ev.Encode()) },
	}
	c.factory = &hostFactory{inner: protocol.NewRegistryFactory(ctx)}

	// The connection is a full Application (D22). It is a protocol object in
	// its own right: register it in the session so the client can address it
	// by ID (set app-wide properties), and hand that ID over in the handshake.
	application := app.New(nil)
	application.SetName(appName)
	application.SetMultiWindow(multiWindow)
	// The name the app connected with is its authorized name (the trust
	// decision is keyed on it). A later wire change to a DIFFERENT name doesn't
	// match that approval, so we allow free renames only when the connection's
	// trust is independent of the name: a local app, or an "Always for All
	// Apps" client. Otherwise a wire rename must keep the authorized name.
	// (A future step could re-prompt instead of rejecting.)
	application.SetWireNameChangeAllowed(req.Local || s.store.allowsAllApps(req))
	c.app = application
	c.session.Register(application)
	s.desktop.Post(func() { s.desktop.AddApplication(application) })
	defer s.desktop.Post(func() { c.teardown() })

	go c.writeLoop()
	defer close(c.out)

	c.send(fmt.Sprintf("welcome version=1 session=%d app=%d", sessionID, application.ObjectID()))
	dbg("welcome sent session=%d app=%q id=%d", sessionID, appName, application.ObjectID())

	// Batch loop: read until end, execute on the UI thread, reply.
	for {
		batch, err := readBatch(scanner)
		if err != nil {
			dbg("batch read ended for app=%q: %v", appName, err)
			return // disconnect -> deferred teardown
		}
		dbg("executing batch (%d statements) for app=%q", len(batch), appName)
		done := make(chan struct{})
		s.desktop.Post(func() {
			defer close(done)
			c.execute(batch)
		})
		<-done
	}
}

// readBatch collects statements until the D22 end terminator.
func readBatch(scanner *protocol.Scanner) ([]*protocol.Statement, error) {
	var batch []*protocol.Statement
	for {
		text, err := scanner.Next()
		if err != nil {
			return nil, err
		}
		script, err := protocol.Parse(text)
		if err != nil {
			// A malformed statement poisons the batch; report at
			// execution time by injecting a marker error.
			return nil, fmt.Errorf("parse: %w", err)
		}
		for _, stmt := range script.Statements {
			if stmt.Verb == "end" && stmt.Key == "" {
				return batch, nil
			}
			batch = append(batch, stmt)
		}
	}
}

// execute runs one batch on the UI thread and replies.
func (c *conn) execute(batch []*protocol.Statement) {
	// Session-level app verbs the protocol session doesn't model are
	// handled here before the rest of the batch runs against the session.
	batch = c.handleAppVerbs(batch)

	script := &protocol.Script{Statements: batch}
	reply, err := c.session.Execute(script, c.factory)
	if err != nil {
		c.send(protocol.EncodeError(err.Error()))
		return
	}

	// Adopt what the batch created: windows join the connection's
	// application; a menubar/statusbar becomes the app's bar content.
	var soloMain *window.Window
	for _, target := range c.factory.take() {
		switch t := target.(type) {
		case *window.Window:
			// A window built as a child of an existing trinket (e.g. an
			// MDI document appended into an mdipane) already has a home;
			// only genuinely top-level windows join the application.
			if t.Parent() == nil {
				// Enforce the per-type creation rules. A rejected window is
				// closed (which emits its window_closed event so the client
				// learns it did not open) and skipped - no out-of-band error
				// line, which would desync the batch's request/reply stream.
				if err := c.gateWindow(t); err != nil {
					dbg("window rejected for app=%q: %v", c.app.Name(), err)
					t.Close()
					continue
				}
				c.app.AddWindow(t)
				// A window that asked to be the app's main window carries
				// the menu/status chrome when torn off.
				if t.MainRequested() {
					c.app.SetMainWindow(t)
					// A solo connection's main window replaces the desktop
					// entirely. Deferred to after the batch so its menu/
					// status content is already adopted when solo mode
					// rebuilds the (Psi-less) bar.
					if c.solo {
						soloMain = t
					}
				}
			}
		case *trinkets.MessageBox:
			if t.Window.Parent() == nil {
				c.app.AddWindow(&t.Window)
			}
		case interface{ Menus() []*trinkets.Menu }:
			c.app.SetMenuBarContent(t.Menus())
		case interface {
			Sections() []trinkets.StatusSection
		}:
			c.app.SetStatusBarContent(t.Sections())
		}
	}
	if soloMain != nil {
		c.server.desktop.EnterSoloMode(soloMain)
	}
	c.server.desktop.RequestUpdate()
	dbg("batch adopted for app=%q: app now has %d window(s)", c.app.Name(), len(c.app.Windows()))

	// Deliver any verb-produced statements (the describe verb's flat
	// vocabulary stream) ahead of the reply that terminates the batch.
	for _, line := range reply.Extra {
		c.send(line)
	}
	c.send(protocol.EncodeReply(reply))
}

// resolveOwnerWindow returns the window an owner object id refers to in this
// session, or nil (unknown id, or the object is not a window).
func (c *conn) resolveOwnerWindow(id uint64) *window.Window {
	if id == 0 {
		return nil
	}
	obj, ok := c.session.Object(id)
	if !ok {
		return nil
	}
	if tp, ok := obj.(interface{ Target() any }); ok {
		if w, ok := tp.Target().(*window.Window); ok {
			return w
		}
	}
	return nil
}

// gateWindow resolves a newly created top-level window's owner and enforces
// the per-type creation rules before it is adopted into the application:
//
//   - main: at most one per application.
//   - normal: only when the application declares multiwindow.
//   - dialog: must have an owner window.
//   - mdichild, modal, toolpalette: always allowed (all apps may create these).
//
// It returns an error describing why the window was rejected, or nil to adopt.
func (c *conn) gateWindow(t *window.Window) error {
	if owner := c.resolveOwnerWindow(t.OwnerRequestID()); owner != nil {
		t.SetOwner(owner)
	}
	// The application's first top-level window defaults to its main window, so
	// a single-window app can create a plain window without declaring
	// multiwindow.
	if t.Type() == window.WindowTypeNormal && !c.appHasMainWindow(t) {
		t.SetType(window.WindowTypeMain)
	}
	switch t.Type() {
	case window.WindowTypeMain:
		if c.appHasMainWindow(t) {
			return fmt.Errorf("application already has a main window")
		}
	case window.WindowTypeNormal:
		if !c.app.MultiWindow() {
			return fmt.Errorf("normal windows require the application to declare multiwindow")
		}
	case window.WindowTypeDialog:
		if t.Owner() == nil {
			return fmt.Errorf("a dialog window requires an owner")
		}
	case window.WindowTypeMDIChild, window.WindowTypeModal, window.WindowTypeToolPalette:
		// Always allowed.
	}
	return nil
}

// appHasMainWindow reports whether the application already has a main window
// (a set main window, or an adopted window of type main), ignoring except.
func (c *conn) appHasMainWindow(except *window.Window) bool {
	if c.app.MainWindow() != nil {
		return true
	}
	for _, w := range c.app.Windows() {
		if w != except && w.Type() == window.WindowTypeMain {
			return true
		}
	}
	return false
}

// handleAppVerbs consumes the session-level application verbs the display
// implements directly (the protocol session has a closed verb set), and
// returns the remaining statements for the session to run. These are the
// desktop-reaching actions a remote app can't perform through its own
// trinket handles - the display does them on the app's behalf:
//
//	rawkey            - pass the next key straight to the focused trinket
//	cut/copy/paste/   - the standard edit actions on the focused trinket
//	  selectall
//	tile/cascade      - arrange the desktop's windows
//	spawndesktop      - leave solo mode: reveal a desktop, solo window
//	                    becomes a torn-off dockable window
//	gosolo            - the inverse: promote a detached app back to solo
//	theme             - toggle the dark/light terminal theme (+ retheme)
//	desktopfont NAME  - set the desktop font (tuesday | default)
//	announce_visual   - toggle showing announcements in the status bar
//	announce_speak    - toggle speaking announcements (macOS `say`)
func (c *conn) handleAppVerbs(batch []*protocol.Statement) []*protocol.Statement {
	d := c.server.desktop
	rest := batch[:0:0]
	for _, stmt := range batch {
		if stmt.Key != "" {
			rest = append(rest, stmt)
			continue
		}
		switch stmt.Verb {
		case "rawkey":
			d.ActivatePassNextKeyToTrinket()
		case "status":
			if sb := d.StatusBar(); sb != nil {
				sb.SetText(argString(stmt, "text"))
			}
		case "cut", "copy", "paste", "selectall":
			editAction(d.FocusedTrinket(), stmt.Verb)
		case "tile":
			if wm := d.WindowManager(); wm != nil {
				wm.TileWindows()
			}
		case "cascade":
			if wm := d.WindowManager(); wm != nil {
				wm.CascadeWindows()
			}
		case "spawndesktop":
			// Leave solo mode: reveal a desktop and turn the solo window
			// into a torn-off, dockable window. Any client may request it.
			d.ExitSoloMode()
		case "gosolo":
			// The inverse: promote a detached app back to solo (fill the
			// display, dismiss the desktop).
			d.EnterSoloFromDesktop()
		case "theme":
			toggleTerminalTheme(d)
		case "desktopfont":
			d.SetFont(namedDesktopFont(firstWord(stmt)))
		case "announce_visual":
			c.announceVisual = !c.announceVisual
			c.updateAnnounce()
		case "announce_speak":
			c.announceSpeak = !c.announceSpeak
			c.updateAnnounce()
		default:
			rest = append(rest, stmt)
		}
	}
	return rest
}

// firstWord returns the first bare-word argument of a statement (the
// "tuesday" in `desktopfont tuesday`), or "" if there is none.
func firstWord(stmt *protocol.Statement) string {
	for _, a := range stmt.Args {
		if a.Flag == protocol.FlagTrue && a.Value == nil {
			return a.Name
		}
	}
	return ""
}

// argString returns the named string argument of a statement (the
// text= in `status text="..."`), or "" if absent.
func argString(stmt *protocol.Statement, name string) string {
	for _, a := range stmt.Args {
		if a.Name == name && a.Value != nil && a.Value.Kind == protocol.StringValue {
			return a.Value.Str
		}
	}
	return ""
}

// namedDesktopFont maps a desktopfont argument to a font (nil = the
// desktop default, Monday).
func namedDesktopFont(name string) *core.Font {
	if name == "tuesday" || name == "tuesday12" {
		return core.FontTuesday12
	}
	return nil
}

// editAction invokes one of the standard edit operations on a trinket
// that supports them (text inputs, edit boxes); a nil or non-editing
// trinket is a no-op.
func editAction(w core.Trinket, verb string) {
	ea, ok := w.(interface {
		Cut()
		Copy()
		Paste()
		SelectAll()
	})
	if !ok {
		return
	}
	switch verb {
	case "cut":
		ea.Cut()
	case "copy":
		ea.Copy()
	case "paste":
		ea.Paste()
	case "selectall":
		ea.SelectAll()
	}
}

// toggleTerminalTheme flips the active dark/light terminal theme and
// repaints; embedded terminals follow via their own palette.
func toggleTerminalTheme(d *trinkets.Desktop) {
	dark := style.ActiveTermTheme() == style.TermThemeLight
	if dark {
		style.SetActiveTermTheme(style.TermThemeDark)
	} else {
		style.SetActiveTermTheme(style.TermThemeLight)
	}
	if wm := d.WindowManager(); wm != nil {
		for _, w := range wm.Windows() {
			applyTerminalTheme(w, dark)
		}
	}
	d.RequestUpdate()
}

// applyTerminalTheme walks a trinket subtree and puts every PurfecTerm
// into the given dark/light mode.
func applyTerminalTheme(w core.Trinket, dark bool) {
	if w == nil {
		return
	}
	if term, ok := w.(*trinkets.PurfecTerm); ok {
		term.SetDarkTheme(dark)
	}
	if cont, ok := w.(core.Container); ok {
		for _, child := range cont.Children() {
			applyTerminalTheme(child, dark)
		}
	}
}

// updateAnnounce installs or clears the desktop's OnAnnounce handler to
// reflect this connection's visual/speech toggles.
func (c *conn) updateAnnounce() {
	am := c.server.desktop.AccessibilityManager()
	if am == nil {
		return
	}
	if !c.announceVisual && !c.announceSpeak {
		am.OnAnnounce = nil
		return
	}
	am.OnAnnounce = func(a core.AccessibilityAnnouncement) {
		if c.announceVisual {
			if sb := c.server.desktop.StatusBar(); sb != nil {
				prefix := "\U0001F4E2"
				if a.Priority == "assertive" {
					prefix = "⚠️"
				}
				sb.SetText(fmt.Sprintf("%s [%s] %s", prefix, a.Priority, a.Message))
			}
		}
		if c.announceSpeak && a.Vocal && runtime.GOOS == "darwin" {
			c.speak(a.Message)
		}
	}
	// Announce the toggle itself so the change is perceptible.
	am.AnnouncePolite("Announcements updated")
}

// speak voices a message via macOS `say`, cancelling any in-flight
// utterance first (navigation throttling already thinned the stream).
func (c *conn) speak(msg string) {
	go func() {
		c.speechMu.Lock()
		if c.speechCmd != nil && c.speechCmd.Process != nil {
			_ = c.speechCmd.Process.Kill()
			_ = c.speechCmd.Wait()
		}
		c.speechCmd = exec.Command("say", "-r", "250", msg)
		c.speechMu.Unlock()
		_ = c.speechCmd.Run()
		c.speechMu.Lock()
		c.speechCmd = nil
		c.speechMu.Unlock()
	}()
}

// teardown runs on the UI thread at disconnect: the app and its
// windows leave the desktop (D22 v1; reattach arrives with D4).
func (c *conn) teardown() {
	for _, w := range c.app.Windows() {
		w.Close()
	}
	c.server.desktop.RemoveApplication(c.app)
	c.server.desktop.RequestUpdate()
}

// send enqueues one outbound statement line (non-blocking for the UI
// thread; a slow client eventually loses its connection rather than
// stalling the display).
func (c *conn) send(statement string) {
	select {
	case c.out <- statement:
	default:
		// Queue full: drop the connection's socket; the reader will
		// notice and tear down.
		c.nc.Close()
	}
}

func (c *conn) writeLoop() {
	for line := range c.out {
		if _, err := c.nc.Write([]byte(line + "\n")); err != nil {
			return
		}
	}
}

// hostFactory records batch-created top-level targets for adoption
// and forwards EventControl.
type hostFactory struct {
	inner   protocol.Factory
	created []any
}

func (f *hostFactory) New(typeName string) (protocol.Object, error) {
	o, err := f.inner.New(typeName)
	if err != nil {
		return nil, err
	}
	if tg, ok := o.(interface{ Target() any }); ok {
		f.created = append(f.created, tg.Target())
	}
	return o, nil
}

// take returns and clears the created list (called per batch, on the
// UI thread).
func (f *hostFactory) take() []any {
	out := f.created
	f.created = nil
	return out
}

func (f *hostFactory) Subscribe(id uint64, typ string) {
	if ec, ok := f.inner.(protocol.EventControl); ok {
		ec.Subscribe(id, typ)
	}
}
func (f *hostFactory) Unsubscribe(id uint64, typ string) {
	if ec, ok := f.inner.(protocol.EventControl); ok {
		ec.Unsubscribe(id, typ)
	}
}
func (f *hostFactory) Suppressed(fn func()) {
	if ec, ok := f.inner.(protocol.EventControl); ok {
		ec.Suppressed(fn)
		return
	}
	fn()
}
