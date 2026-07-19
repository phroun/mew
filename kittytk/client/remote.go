package client

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/phroun/kittytk/protocol"
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
		scanner: protocol.NewScanner(nc),
		replies: make(chan replyOrError, 1),
		events:  make(chan *protocol.Event, 256),
	}
	c.transport = rt

	// Handshake: hello out, welcome back (reattach-ready: the reply
	// carries the server-assigned session id). The optional `solo` flag
	// asks the display to run this app as the whole surface; the optional
	// token authorizes the client (checked by the host when configured).
	hello := fmt.Sprintf("hello version=1 app=%s", protocol.Quote(appName))
	if opts.Solo {
		hello += " solo"
	}
	if opts.MultiWindow {
		hello += " multiwindow"
	}
	if tok := opts.token(); tok != "" {
		hello += " token=" + protocol.Quote(tok)
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
	script, err := protocol.Parse(welcome)
	if err != nil || len(script.Statements) == 0 || script.Statements[0].Verb != "welcome" {
		nc.Close()
		return nil, fmt.Errorf("handshake: unexpected response %q", welcome)
	}
	// The handshake carries this connection's Application ObjectID, so the app
	// can address application-wide properties (see Conn.AppID / Conn.SetApp).
	for _, a := range script.Statements[0].Args {
		if a.Name == "app" && a.Value != nil && a.Value.Kind == protocol.NumberValue && a.Value.IsInt {
			c.appID = uint64(a.Value.Number)
		}
	}
	dbg("dial app=%q: welcome received (app id=%d), connection ready", appName, c.appID)

	go rt.readLoop()
	go rt.eventLoop()
	return c, nil
}

type replyOrError struct {
	reply *protocol.Reply
	err   error
}

// remoteTransport speaks D22 over a socket: batches out (terminated
// by end), reply/error/event statements in.
type remoteTransport struct {
	conn    *Conn
	nc      net.Conn
	scanner *protocol.Scanner

	writeMu sync.Mutex
	replies chan replyOrError

	// pendingDesc accumulates the describe verb's flat vocabulary
	// statements (proptype/prop/propcommon) that arrive ahead of the
	// reply terminating the batch; attached to that reply's Extra.
	pendingDesc []string

	// events are delivered on their own goroutine so a handler that
	// executes statements (SetCaption inside OnToggle) cannot
	// deadlock the reader that must route the reply.
	events chan *protocol.Event

	closeOnce sync.Once
}

func (t *remoteTransport) exec(src string) (*protocol.Reply, error) {
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

func (t *remoteTransport) close() error {
	var err error
	t.closeOnce.Do(func() { err = t.nc.Close() })
	return err
}

// readLoop routes inbound statements: replies/errors complete a
// pending exec; events queue for the event goroutine.
func (t *remoteTransport) readLoop() {
	defer func() {
		t.close()
		close(t.replies)
		close(t.events)
		t.conn.markClosed()
	}()
	for {
		text, err := t.scanner.Next()
		if err != nil {
			return
		}
		script, err := protocol.Parse(text)
		if err != nil {
			continue // malformed inbound statement; skip
		}
		for _, stmt := range script.Statements {
			switch stmt.Verb {
			case "reply":
				r, err := protocol.DecodeReply(stmt)
				if r != nil && len(t.pendingDesc) > 0 {
					r.Extra = t.pendingDesc
				}
				t.pendingDesc = nil
				t.replies <- replyOrError{reply: r, err: err}
			case "error":
				t.pendingDesc = nil
				msg := "display error"
				for _, a := range stmt.Args {
					if a.Name == "text" && a.Value != nil && a.Value.Kind == protocol.StringValue {
						msg = a.Value.Str
					}
				}
				t.replies <- replyOrError{err: fmt.Errorf("%s", msg)}
			case "proptype", "prop", "propcommon":
				// describe verb output: buffer until the reply arrives.
				t.pendingDesc = append(t.pendingDesc, strings.TrimSpace(text))
			case "event":
				if ev, err := protocol.ParseEvent(text); err == nil {
					t.events <- ev
				}
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
