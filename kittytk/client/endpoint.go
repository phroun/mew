package client

// Transport addressing for the display protocol. The wire language is
// identical over every transport; only the endpoint string and the
// dial differ:
//
//	unix:/run/kittytk/display-0.sock   an explicit unix socket
//	/run/kittytk/display-0.sock        a bare path is unix (back-compat)
//	tcp://host:port                    plaintext TCP (loopback/trusted LAN)
//	tls://host:port                    TLS over TCP (real remote)
//
// TLS uses SSH-style trust-on-first-use: the server's certificate
// fingerprint is pinned in a known_hosts file on first connect and
// checked on every reconnect, so self-signed certificates work with no
// CA or PKI. A shared token (KITTYTK_TOKEN) optionally authorizes the
// client in the handshake; it is unsniffable under tls://.

import (
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// DefaultTCPPort is the port assumed when a tcp:// or tls:// endpoint
// omits one.
const DefaultTCPPort = "9797"

// TokenEnv names the environment variable carrying the shared secret a
// client presents in its handshake (optional; empty = no token).
const TokenEnv = "KITTYTK_TOKEN"

// KnownHostsEnv overrides the path of the TLS fingerprint store.
const KnownHostsEnv = "KITTYTK_KNOWN_HOSTS"

// InsecureEnv, when set to a truthy value, disables TLS fingerprint
// pinning (accept any server certificate). For diagnostics only.
const InsecureEnv = "KITTYTK_INSECURE"

// DialOptions carries the transport/auth knobs shared by every dialer.
// The zero value is the plain, back-compatible dial.
type DialOptions struct {
	Solo bool // request the whole display (see DialSolo)

	// MultiWindow declares the app manages more than one primary window, so
	// the display gives it a system-managed Window menu.
	MultiWindow bool

	// Token authorizes the client in the handshake. Empty defaults to
	// $KITTYTK_TOKEN; still empty means no token is sent.
	Token string

	// Insecure disables TLS fingerprint pinning for tls:// endpoints
	// (accept any certificate, do not record). Diagnostics only.
	Insecure bool

	// KnownHosts overrides the fingerprint store path (default:
	// $KITTYTK_KNOWN_HOSTS, else <config>/kittytk/known_hosts).
	KnownHosts string

	// TLSConfig, if set, fully replaces the default TOFU-pinning config
	// for tls:// endpoints (e.g. to verify against a real CA instead).
	TLSConfig *tls.Config

	// Dispatch receives action= command IDs (may be nil).
	Dispatch func(commandID string)
}

// token returns the effective token (explicit, else the environment).
func (o DialOptions) token() string {
	if o.Token != "" {
		return o.Token
	}
	return os.Getenv(TokenEnv)
}

func (o DialOptions) insecure() bool {
	return o.Insecure || isTruthy(os.Getenv(InsecureEnv))
}

func isTruthy(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

type endpoint struct {
	network string // "unix" or "tcp"
	address string // filesystem path, or host:port
	useTLS  bool
	host    string // hostname for SNI + pinning (tls only)
}

// parseEndpoint maps an endpoint string to a concrete transport. A bare
// value with no recognized scheme is a unix socket path (back-compat).
func parseEndpoint(s string) endpoint {
	switch {
	case strings.HasPrefix(s, "unix:"):
		return endpoint{network: "unix", address: strings.TrimPrefix(s, "unix:")}
	case strings.HasPrefix(s, "tcp://"):
		return endpoint{network: "tcp", address: withPort(strings.TrimPrefix(s, "tcp://"))}
	case strings.HasPrefix(s, "tls://"):
		addr := withPort(strings.TrimPrefix(s, "tls://"))
		host, _, err := net.SplitHostPort(addr)
		if err != nil {
			host = addr
		}
		return endpoint{network: "tcp", address: addr, useTLS: true, host: host}
	default:
		return endpoint{network: "unix", address: s}
	}
}

// withPort appends the default port when the address has none.
func withPort(addr string) string {
	if _, _, err := net.SplitHostPort(addr); err == nil {
		return addr
	}
	return net.JoinHostPort(addr, DefaultTCPPort)
}

// connect dials the endpoint, wrapping in a pinned TLS session for
// tls://. The returned net.Conn is transport-agnostic above this point.
func (e endpoint) connect(opts DialOptions) (net.Conn, error) {
	if e.network == "unix" {
		return net.Dial("unix", e.address)
	}
	raw, err := net.Dial("tcp", e.address)
	if err != nil {
		return nil, err
	}
	if !e.useTLS {
		return raw, nil
	}
	cfg := opts.TLSConfig
	if cfg == nil {
		cfg = tofuTLSConfig(e.address, e.host, opts)
	}
	tc := tls.Client(raw, cfg)
	if err := tc.Handshake(); err != nil {
		raw.Close()
		return nil, fmt.Errorf("tls: %w", err)
	}
	return tc, nil
}

// tofuTLSConfig verifies the server by pinned fingerprint rather than a
// CA chain (trust on first use), and presents this client's persistent
// identity certificate so the host can approve it by fingerprint
// (mutual TLS).
func tofuTLSConfig(hostport, host string, opts DialOptions) *tls.Config {
	cfg := &tls.Config{
		ServerName:         host,
		InsecureSkipVerify: true, // chain verification replaced by pinning
		VerifyConnection: func(cs tls.ConnectionState) error {
			if opts.insecure() {
				return nil
			}
			if len(cs.PeerCertificates) == 0 {
				return fmt.Errorf("server presented no certificate")
			}
			return verifyPin(opts.KnownHosts, hostport, FingerprintSHA256(cs.PeerCertificates[0].Raw))
		},
	}
	if id, err := clientIdentity(); err == nil {
		cfg.Certificates = []tls.Certificate{id}
	}
	return cfg
}

// FingerprintSHA256 renders a certificate's DER as sha256:<hex>, the
// form stored in known_hosts and printed by the host.
func FingerprintSHA256(der []byte) string {
	sum := sha256.Sum256(der)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// verifyPin applies trust-on-first-use against the known_hosts store.
func verifyPin(khPath, hostport, fp string) error {
	if khPath == "" {
		khPath = KnownHostsPath()
	}
	pinned, err := lookupPin(khPath, hostport)
	if err != nil {
		return err
	}
	if pinned == "" {
		if err := addPin(khPath, hostport, fp); err != nil {
			return err
		}
		fmt.Fprintf(os.Stderr, "kittytk: pinned new host %s %s\n", hostport, fp)
		return nil
	}
	if pinned != fp {
		return fmt.Errorf(
			"host identity for %s changed!\n  pinned %s\n  got    %s\n"+
				"if this is expected, remove that line from %s",
			hostport, pinned, fp, khPath)
	}
	return nil
}

// ConfigDir is the per-user KittyTK config directory: <config>/kittytk,
// where <config> is $XDG_CONFIG_HOME (else %APPDATA% on Windows, else
// ~/.config). The rule is duplicated in the Python and C clients so the
// three share one identity/known_hosts store per machine.
func ConfigDir() string {
	var base string
	if x := os.Getenv("XDG_CONFIG_HOME"); x != "" {
		base = x
	} else if runtime.GOOS == "windows" {
		if a := os.Getenv("APPDATA"); a != "" {
			base = a
		}
	}
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "kittytk")
}

// KnownHostsPath is the TLS fingerprint store: $KITTYTK_KNOWN_HOSTS,
// else <config>/kittytk/known_hosts.
func KnownHostsPath() string {
	if p := os.Getenv(KnownHostsEnv); p != "" {
		return p
	}
	return filepath.Join(ConfigDir(), "known_hosts")
}

// lookupPin returns the pinned fingerprint for hostport, or "" if none.
func lookupPin(path, hostport string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == hostport {
			return fields[1], nil
		}
	}
	return "", nil
}

// addPin appends a hostport/fingerprint line, creating the store.
func addPin(path, hostport, fp string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = fmt.Fprintf(f, "%s %s\n", hostport, fp)
	return err
}
