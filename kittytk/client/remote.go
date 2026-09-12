package client

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/phroun/kittytk/wire"
)

// DisplayEnv is the environment variable naming the display endpoint.
// It may be a unix socket path or a tcp://host:port / tls://host:port
// URL; DefaultEndpoint is used when unset.
const DisplayEnv = "KITTYTK_DISPLAY"

// DefaultEndpoint returns the conventional endpoint: $KITTYTK_DISPLAY if
// set, else a per-OS default. On Windows the default is loopback TCP
// (tcp://127.0.0.1:9797): AF_UNIX is unsupported under Wine and unreliable
// on older Windows, whereas loopback TCP works everywhere and is still a
// same-machine ("local") connection. Elsewhere the default is a unix
// socket at $XDG_RUNTIME_DIR/kittytk/display-0.sock. The value may carry
// any scheme (see endpoint.go); client and host share this function, so
// they always agree.
func DefaultEndpoint() string {
	if p := os.Getenv(DisplayEnv); p != "" {
		return p
	}
	if runtime.GOOS == "windows" {
		return "tcp://127.0.0.1:" + DefaultTCPPort
	}
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	if runtimeDir == "" {
		runtimeDir = os.TempDir()
	}
	return filepath.Join(runtimeDir, "kittytk", "display-0.sock")
}

// DefaultSocketPath is the historical name for DefaultEndpoint (kept so
// existing callers keep compiling).
func DefaultSocketPath() string { return DefaultEndpoint() }

// Dial connects to a display service. endpoint is a unix socket path or
// a tcp://host:port / tls://host:port URL (see endpoint.go). appName
// identifies the application in the handshake; dispatch receives action=
// command IDs (may be nil).
//
// Remote-connection caveats: event handlers run on the connection's
// reader goroutine, and Handle.Target() is always nil (the trinkets
// live in the display service's process).
func Dial(endpoint, appName string, dispatch func(commandID string)) (*Conn, error) {
	return DialWith(endpoint, appName, DialOptions{Dispatch: dispatch})
}

// DialSolo is Dial for an app that wants to be the whole display: its
// `main` window replaces the desktop entirely (no system menu, dock or
// wallpaper), rendered like a torn-off window filling the surface. The
// host quits when the last window closes (see docs/solo-app-plan.md).
func DialSolo(endpoint, appName string, dispatch func(commandID string)) (*Conn, error) {
	return DialWith(endpoint, appName, DialOptions{Solo: true, Dispatch: dispatch})
}

// DialWith is the full-control dialer: transport (unix/tcp/tls) is
// chosen from the endpoint scheme, TLS uses trust-on-first-use pinning,
// and opts.Token (or $KITTYTK_TOKEN) authorizes the client.
func DialWith(endpointStr, appName string, opts DialOptions) (*Conn, error) {
	return dial(parseEndpoint(endpointStr), appName, opts)
}

func dial(ep endpoint, appName string, opts DialOptions) (*Conn, error) {
	dbg("dial app=%q net=%s addr=%s tls=%v: connecting", appName, ep.network, ep.address, ep.useTLS)
	nc, err := ep.connect(opts)
	if err != nil {
		dbg("dial app=%q: connect failed: %v", appName, err)
		return nil, err
	}
	dbg("dial app=%q: transport up, sending hello", appName)

	c := newConn(opts.Dispatch)
	rt := &remoteTransport{
		conn:    c,
		nc:      nc,
		scanner: wire.NewScanner(nc),
		replies: make(chan replyOrError, 1),
		events:  make(chan *wire.Event, 256),
		inbound: make(chan []*wire.Statement, 64),
	}
	c.transport = rt

	// Handshake: hello out, welcome back (reattach-ready: the reply
	// carries the server-assigned session id). The optional `solo` flag
	// asks the display to run this app as the whole surface; the optional
	// token authorizes the client (checked by the host when configured).
	hello := fmt.Sprintf("hello version=1 app=%s", wire.Quote(appName))
	if opts.Solo {
		hello += " solo"
	}
	if opts.MultiWindow {
		hello += " multiwindow"
	}
	if tok := opts.token(); tok != "" {
		hello += " token=" + wire.Quote(tok)
	}
	if _, err := nc.Write([]byte(hello + "\nend\n")); err != nil {
		nc.Close()
		return nil, err
	}
	dbg("dial app=%q: hello sent, awaiting welcome", appName)
	welcome, err := rt.scanner.Next()
	if err != nil {
		dbg("dial app=%q: reading welcome failed: %v", appName, err)
		nc.Close()
		return nil, fmt.Errorf("handshake: %w", err)
	}
	script, err := wire.Parse(welcome)
	if err != nil || len(script.Statements) == 0 || script.Statements[0].Verb != "welcome" {
		nc.Close()
		return nil, fmt.Errorf("handshake: unexpected response %q", welcome)
	}
	// Then an init statement: what this connection was handed, one field per
	// object. Every field of it is an object, so a client reads them all
	// without being taught the names -- a display that hands over a fourth
	// thing is reachable here with no client change (see Conn.Init).
	//
	// This first one is read here so the ids are in hand before Dial returns.
	// Later ones arrive on the read loop like anything else: the display can
	// hand over something new, or hand the same name a new object, whenever it
	// has reason to.
	init, err := rt.scanner.Next()
	if err != nil {
		dbg("dial app=%q: reading init failed: %v", appName, err)
		nc.Close()
		return nil, fmt.Errorf("handshake: reading init: %w", err)
	}
	initScript, err := wire.Parse(init)
	if err != nil || len(initScript.Statements) == 0 ||
		initScript.Statements[0].Verb != wire.InitVerb {
		nc.Close()
		return nil, fmt.Errorf("handshake: unexpected init %q", init)
	}
	c.handOver(initScript.Statements[0])
	dbg("dial app=%q: handed %v, connection ready", appName, c.InitNames())

	go rt.readLoop()
	go rt.eventLoop()
	go rt.inboundLoop()
	return c, nil
}

type replyOrError struct {
	reply *wire.Reply
	err   error
}

// remoteTransport speaks D22 over a socket: batches out (terminated
// by end), reply/error/event statements in.
type remoteTransport struct {
	conn    *Conn
	nc      net.Conn
	scanner *wire.Scanner

	writeMu sync.Mutex
	replies chan replyOrError

	// pendingDesc accumulates the describe verb's flat vocabulary statements
	// (proptype/prop/propcommon/ask/askarg/do/doarg/event/eventfield) that
	// arrive ahead of the reply terminating the batch; attached to that
	// reply's Extra.
	pendingDesc []string

	// events are delivered on their own goroutine so a handler that
	// executes statements (SetCaption inside OnToggle) cannot
	// deadlock the reader that must route the reply.
	events chan *wire.Event

	// inbound carries whole batches the display sent: the other direction of
	// the wire, where the display asks and this application answers. They get
	// a goroutine of their own rather than sharing the event one, because
	// serving a window can take as long as the records take and a list nobody
	// is looking at must not hold up a click.
	//
	// Whole batches rather than statements, because a batch is what gets one
	// reply -- and the reply is what carries the ids this application minted
	// for whatever the batch made.
	inbound   chan []*wire.Statement
	pendingIn []*wire.Statement

	closeOnce sync.Once
}

func (t *remoteTransport) Exec(src string) (*wire.Reply, error) {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()

	if _, err := t.nc.Write([]byte(src + "\nend\n")); err != nil {
		dbg("exec: write failed: %v", err)
		return nil, err
	}
	dbg("exec: batch sent (%d bytes), awaiting reply", len(src)+5)
	r, ok := <-t.replies
	if !ok {
		dbg("exec: connection closed before reply")
		return nil, fmt.Errorf("connection closed")
	}
	dbg("exec: reply received (err=%v)", r.err)
	return r.reply, r.err
}

func (t *remoteTransport) Close() error {
	var err error
	t.closeOnce.Do(func() { err = t.nc.Close() })
	return err
}

// readLoop routes inbound statements: replies/errors complete a
// pending exec; events queue for the event goroutine.
func (t *remoteTransport) readLoop() {
	defer func() {
		t.Close()
		close(t.replies)
		close(t.events)
		close(t.inbound)
		t.conn.markClosed()
	}()
	for {
		text, err := t.scanner.Next()
		if err != nil {
			return
		}
		script, err := wire.Parse(text)
		if err != nil {
			continue // malformed inbound statement; skip
		}
		for _, stmt := range script.Statements {
			switch stmt.Verb {
			case "reply":
				r, err := wire.DecodeReply(stmt)
				if r != nil && len(t.pendingDesc) > 0 {
					r.Extra = t.pendingDesc
				}
				t.pendingDesc = nil
				t.replies <- replyOrError{reply: r, err: err}
			case "error":
				t.pendingDesc = nil
				msg := "display error"
				for _, a := range stmt.Args {
					if a.Name == "text" && a.Value != nil && a.Value.Kind == wire.StringValue {
						msg = a.Value.Str
					}
				}
				t.replies <- replyOrError{err: fmt.Errorf("%s", msg)}
			case "proptype", "prop", "propcommon", "ask", "askarg", "do", "doarg", "eventfield":
				// Two different lines start with `ask` and with `do`: the
				// display putting a question to something this application
				// holds, and the describe stream's record of a question a type
				// CAN answer. The first is addressed to an object and so opens
				// with a bare id; the second opens with of=.
				if _, _, ok := hostedTarget(stmt, nil); ok {
					t.pendingIn = append(t.pendingIn, stmt)
					continue
				}
				// describe verb output: buffer until the reply arrives.
				t.pendingDesc = append(t.pendingDesc, strings.TrimSpace(text))
			case "new", wire.QueryVerb, "set", "destroy", "sub", "unsub":
				// The other direction: the display making, asking after, or
				// letting go of something this application holds. Gathered
				// until the batch ends, because a batch is what gets a reply.
				t.pendingIn = append(t.pendingIn, stmt)
			case "end":
				// The batch is closed. Handled off the reader, because
				// answering writes and the reader has to stay free to route
				// what comes back.
				if len(t.pendingIn) > 0 {
					t.inbound <- t.pendingIn
					t.pendingIn = nil
				}
			case wire.InitVerb:
				// The display handing over something: a new object, or a new
				// object under a name already in hand. It is not only a
				// handshake step -- the display says it whenever it has
				// something to give.
				t.conn.handOver(stmt)
			case "event":
				// One verb, two things: an event RECORD opens with a bare type
				// word, and the describe stream's description of one names the
				// type it belongs to with of=. ParseEvent is what tells them
				// apart -- it wants that leading word -- so a line it refuses is
				// a description, and describing events was reaching no client
				// at all until it was buffered here.
				if ev, err := wire.ParseEvent(text); err == nil {
					t.events <- ev
					continue
				}
				t.pendingDesc = append(t.pendingDesc, strings.TrimSpace(text))
			}
		}
	}
}

// eventLoop delivers events in order on a dedicated goroutine (the
// remote handler-thread: replica folding then app handlers).
func (t *remoteTransport) eventLoop() {
	for ev := range t.events {
		t.conn.deliver(ev)
	}
}

// inboundLoop runs the batches the display sent, in the order they arrived.
func (t *remoteTransport) inboundLoop() {
	for batch := range t.inbound {
		t.conn.InboundBatch(batch)
	}
}

// Send writes without waiting for anything back, which is what the reverse
// direction needs: this is the end answering, not asking.
func (t *remoteTransport) Send(src string) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	_, err := t.nc.Write([]byte(src + "\n"))
	return err
}
