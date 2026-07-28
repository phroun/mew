package display

// Connection authorization. Because the host IS the user interface, the
// host is what asks: on a non-local connection it identifies the app by
// name and the client by certificate fingerprint, and the user approves.
//
// Decisions are keyed by (fingerprint, app name): a trusted client may
// not silently present an app it was not approved for. The six outcomes
// map onto the two-tier prompt:
//
//	Yes -> Once Only              (AuthAllowOnce)
//	    -> Always                 (AuthAllowApp: this fingerprint + this app)
//	    -> Always for All Apps    (AuthAllowClient: this fingerprint, any app)
//	No  -> Not Now                (AuthDenyOnce)
//	    -> Never for this App     (AuthDenyApp: this fingerprint + this app)
//	    -> Block Client           (AuthDenyClient: this fingerprint, any app)
//
// The persistent choices are recorded in an authorizations file so they
// apply on reconnect. Deny takes precedence over allow.

import (
	"bufio"
	"crypto/subtle"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// AuthDecision is the outcome of an authorization request.
type AuthDecision int

const (
	AuthDenyOnce    AuthDecision = iota // No / Not Now: reject this session
	AuthAllowOnce                       // Yes / Once Only: accept this session
	AuthAllowApp                        // Always: remember (fingerprint, app)
	AuthAllowClient                     // Always for All Apps: remember fingerprint
	AuthDenyApp                         // Never for this App: block (fingerprint, app)
	AuthDenyClient                      // Block Client: block fingerprint (any app)
)

func (d AuthDecision) allows() bool {
	return d == AuthAllowOnce || d == AuthAllowApp || d == AuthAllowClient
}

// AuthRequest describes a pending connection for an authorizer/prompt.
type AuthRequest struct {
	AppName     string // the app label from the handshake
	Fingerprint string // sha256:... of the client cert (tls://); "" otherwise
	Transport   string // "unix" | "tcp" | "tls"
	RemoteAddr  string // peer address (host:port), for tcp/tls
	Local       bool   // unix socket or loopback peer (same machine)
}

// identity is the persistence key: the certificate fingerprint over
// tls://, else ip:<host> for a plaintext peer.
func (r AuthRequest) identity() string {
	if r.Fingerprint != "" {
		return r.Fingerprint
	}
	if r.RemoteAddr != "" {
		if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
			return "ip:" + host
		}
		return "ip:" + r.RemoteAddr
	}
	return ""
}

// Authorizer decides a pending connection. Set Config.Authorize to
// supply one (tests inject a scripted authorizer; the desktop hosts
// install an interactive prompt). Persistent outcomes are recorded by
// the server regardless.
type Authorizer func(AuthRequest) AuthDecision

// AuthStoreEnv overrides the path of the persistent authorizations file.
const AuthStoreEnv = "KITTYTK_AUTHORIZATIONS"

func authStorePath() string {
	if p := os.Getenv(AuthStoreEnv); p != "" {
		return p
	}
	return filepath.Join(configDir(), "authorizations")
}

// authStore is the persistent allow/deny record.
type authStore struct {
	path string
	mu   sync.Mutex
}

func newAuthStore(path string) *authStore {
	if path == "" {
		path = authStorePath()
	}
	return &authStore{path: path}
}

// decide returns a terminal allow (true) / deny (false) if the store has
// a matching rule, with ok=false when the request is undecided. Deny
// wins over allow, and client-wide rules win over per-app rules.
func (s *authStore) decide(req AuthRequest) (allow bool, ok bool) {
	id := req.identity()
	if id == "" {
		return false, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.path)
	if err != nil {
		return false, false
	}
	defer f.Close()

	var denyClient, allowClient, denyApp, allowApp bool
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		verdict, scope, fp, app, ok := parseAuthLine(sc.Text())
		if !ok || fp != id {
			continue
		}
		switch {
		case scope == "client" && verdict == "deny":
			denyClient = true
		case scope == "client" && verdict == "allow":
			allowClient = true
		case scope == "app" && app == req.AppName && verdict == "deny":
			denyApp = true
		case scope == "app" && app == req.AppName && verdict == "allow":
			allowApp = true
		}
	}
	switch {
	case denyClient:
		return false, true // Block Client
	case denyApp:
		return false, true // Never for this App
	case allowClient:
		return true, true // Always for All Apps
	case allowApp:
		return true, true // Always
	}
	return false, false
}

// allowsAllApps reports whether the store grants this client an "Always for
// All Apps" standing (AuthAllowClient): a persistent `allow client <id>` rule
// with no overriding client-wide deny. It is the signal the host uses to let
// a remote app change its own name over the wire.
func (s *authStore) allowsAllApps(req AuthRequest) bool {
	id := req.identity()
	if id == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.path)
	if err != nil {
		return false
	}
	defer f.Close()

	var allowClient, denyClient bool
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		verdict, scope, fp, _, ok := parseAuthLine(sc.Text())
		if !ok || fp != id || scope != "client" {
			continue
		}
		switch verdict {
		case "allow":
			allowClient = true
		case "deny":
			denyClient = true
		}
	}
	return allowClient && !denyClient
}

// record persists the durable part of a decision (the once-only
// outcomes write nothing).
func (s *authStore) record(req AuthRequest, d AuthDecision) error {
	id := req.identity()
	if id == "" {
		return nil
	}
	var line string
	switch d {
	case AuthAllowApp:
		line = fmt.Sprintf("allow app %s %s", id, req.AppName)
	case AuthAllowClient:
		line = fmt.Sprintf("allow client %s", id)
	case AuthDenyApp:
		line = fmt.Sprintf("deny app %s %s", id, req.AppName)
	case AuthDenyClient:
		line = fmt.Sprintf("deny client %s", id)
	default:
		return nil // once-only: nothing persists
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintln(f, line)
	return err
}

// parseAuthLine parses "allow|deny app|client <fingerprint> [appname...]".
// The app name is the remainder of the line (may contain spaces).
func parseAuthLine(line string) (verdict, scope, fp, app string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", "", "", false
	}
	fields := strings.Fields(line)
	if len(fields) < 3 {
		return "", "", "", "", false
	}
	verdict, scope, fp = fields[0], fields[1], fields[2]
	if verdict != "allow" && verdict != "deny" {
		return "", "", "", "", false
	}
	if scope == "app" {
		// app name is everything after the fingerprint token
		idx := strings.Index(line, fp)
		app = strings.TrimSpace(line[idx+len(fp):])
	} else if scope != "client" {
		return "", "", "", "", false
	}
	return verdict, scope, fp, app, true
}

// admit is the authorization gate run before a connection is granted an
// Application. token is the value from the client's handshake.
func (s *Server) admit(req AuthRequest, token string) bool {
	// Automation bypass: a configured token that matches admits anyone.
	if s.token != "" &&
		subtle.ConstantTimeCompare([]byte(token), []byte(s.token)) == 1 {
		return true
	}
	// Local connections (unix socket / loopback) are same-machine and
	// trusted by the OS already; never prompt for them unless the host
	// opted into PromptLocal.
	if req.Local && !s.promptLocal {
		return true
	}
	// A persistent allow/deny is final.
	if allow, ok := s.store.decide(req); ok {
		return allow
	}
	// Lockdown: reject anything not already trusted, no prompt.
	if s.preTrustedOnly.Load() {
		return false
	}
	// Otherwise ask: a scripted authorizer, else the interactive prompt,
	// else refuse (a headless host with no decider is closed by default).
	var d AuthDecision = AuthDenyOnce
	switch {
	case s.authorize != nil:
		d = s.authorize(req)
	case s.prompt != nil:
		d = s.prompt(req)
	}
	_ = s.store.record(req, d)
	dbg("admit app=%q transport=%s id=%q -> decision=%d allowed=%v",
		req.AppName, req.Transport, req.identity(), d, d.allows())
	return d.allows()
}

// isLocalConn reports whether a connection's peer is on this machine (a
// unix socket, or a loopback TCP/TLS address).
func isLocalConn(nc net.Conn) bool {
	if _, ok := nc.(*net.UnixConn); ok {
		return true
	}
	ra := nc.RemoteAddr()
	if ra == nil {
		return false
	}
	if ra.Network() == "unix" {
		return true
	}
	host := ra.String()
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
