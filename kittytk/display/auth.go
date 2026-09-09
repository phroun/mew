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

// The three standings a rule can express: an allow, a deny, or no line at all
// -- which leaves the client to whatever else decides, and in the end to being
// asked about again.
const (
	ruleNone  = ""
	ruleAllow = "allow"
	ruleDeny  = "deny"
)

// setClientRule makes the store say one thing about a client, whatever it said
// before: allow it every app, refuse it outright, or say nothing.
func (s *authStore) setClientRule(identity, rule string) error {
	return s.replaceRule(identity, "", rule)
}

// setAppRule is the same for one app of one client. Saying nothing here leaves
// the app to the client-wide rule.
func (s *authStore) setAppRule(identity, app, rule string) error {
	if app == "" {
		return nil
	}
	return s.replaceRule(identity, app, rule)
}

// forget drops everything the store says about a client, the apps it named
// included.
func (s *authStore) forget(identity string) error {
	if identity == "" {
		return nil
	}
	return s.edit(func(_, _, fp, _ string) bool { return fp != identity }, "")
}

// forgetApp drops what the store says about one app of a client.
func (s *authStore) forgetApp(identity, app string) error {
	if identity == "" || app == "" {
		return nil
	}
	return s.edit(func(_, scope, fp, a string) bool {
		return fp != identity || scope != "app" || a != app
	}, "")
}

func (s *authStore) replaceRule(identity, app, rule string) error {
	if identity == "" {
		return nil
	}
	scope := "client"
	if app != "" {
		scope = "app"
	}
	line := ""
	if rule == ruleAllow || rule == ruleDeny {
		line = fmt.Sprintf("%s %s %s", rule, scope, identity)
		if app != "" {
			line += " " + app
		}
	}
	return s.edit(func(_, sc, fp, a string) bool {
		return fp != identity || sc != scope || a != app
	}, line)
}

// edit rewrites the file with the lines the filter keeps, plus one more if
// there is one to add. A line it cannot parse -- a comment, or anything
// written by a hand other than this one -- is kept as it stands: the file
// belongs to the user, and an editor that dropped what it did not recognise
// would be a poor guest in it.
func (s *authStore) edit(keep func(verdict, scope, fp, app string) bool, add string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	var out []string
	if f, err := os.Open(s.path); err == nil {
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			line := sc.Text()
			if verdict, scope, fp, app, ok := parseAuthLine(line); ok && !keep(verdict, scope, fp, app) {
				continue
			}
			out = append(out, line)
		}
		f.Close()
	}
	if add != "" {
		out = append(out, add)
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	body := ""
	if len(out) > 0 {
		body = strings.Join(out, "\n") + "\n"
	}
	return os.WriteFile(s.path, []byte(body), 0o600)
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
	if req.Local && !s.promptLocal.Load() {
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

// authEntry is one client the store holds a standing decision about, with the
// apps it names individually. It is what the Connections window reads: the
// same lines `decide` consults, gathered per identity instead of answered for
// one request.
type authEntry struct {
	identity string
	allow    bool // a client-wide allow stands
	deny     bool // a client-wide deny stands
	apps     []authEntryApp
}

// authEntryApp is one app name a client was decided for by itself. Both
// verdicts are kept rather than one flag, so deny wins wherever the two lines
// happen to sit relative to each other -- which is what decide does.
type authEntryApp struct {
	name  string
	allow bool
	deny  bool
}

// allowed reports the standing verdict for this app: deny beats allow.
func (a authEntryApp) allowed() bool { return a.allow && !a.deny }

// app is what this client's rules say about one app of it. An app with no rule
// of its own comes back with neither verdict set, which is the standing that
// leaves it to the client's own.
func (e authEntry) app(name string) authEntryApp {
	for _, a := range e.apps {
		if a.name == name {
			return a
		}
	}
	return authEntryApp{name: name}
}

// entries reads every standing decision, in the order identities first appear
// in the file -- which is the order the user approved them. A repeated line
// updates the entry it belongs to rather than adding another, matching
// `decide`, which reads them all and lets deny win.
func (s *authStore) entries() []authEntry {
	s.mu.Lock()
	defer s.mu.Unlock()

	f, err := os.Open(s.path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var out []authEntry
	at := map[string]int{}    // identity -> index in out
	appAt := map[string]int{} // identity+"\x00"+app -> index in that entry's apps

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		verdict, scope, id, app, ok := parseAuthLine(sc.Text())
		if !ok {
			continue
		}
		i, seen := at[id]
		if !seen {
			i = len(out)
			at[id] = i
			out = append(out, authEntry{identity: id})
		}
		switch {
		case scope == "client" && verdict == "allow":
			out[i].allow = true
		case scope == "client" && verdict == "deny":
			out[i].deny = true
		case scope == "app" && app != "":
			k := id + "\x00" + app
			j, had := appAt[k]
			if !had {
				j = len(out[i].apps)
				appAt[k] = j
				out[i].apps = append(out[i].apps, authEntryApp{name: app})
			}
			if verdict == "deny" {
				out[i].apps[j].deny = true
			} else {
				out[i].apps[j].allow = true
			}
		}
	}
	return out
}
