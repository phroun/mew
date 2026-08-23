package display_test

// TCP and TLS transport, mutual-TLS identity, trust-on-first-use host
// pinning, token bypass, and connection authorization - all over real
// loopback sockets driving the same headless host as the unix tests.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/display"
	"github.com/phroun/kittytk/objects/trinkets"
)

// TestMain redirects every per-user store (client identity, host
// identity, known_hosts, authorizations) into a throwaway dir so the
// suite never reads or writes the developer's real ~/.config/kittytk.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "kittytk-test-config")
	if err != nil {
		panic(err)
	}
	set := func(k, v string) {
		if os.Getenv(k) == "" {
			os.Setenv(k, v)
		}
	}
	set(client.IdentityEnv, filepath.Join(dir, "identity.pem"))
	set(client.KnownHostsEnv, filepath.Join(dir, "known_hosts"))
	set(display.HostIdentityEnv, filepath.Join(dir, "host_identity.pem"))
	set(display.AuthStoreEnv, filepath.Join(dir, "authorizations"))
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

// startHost boots a headless desktop serving cfg and returns the server
// plus a cleanup. cfg.Endpoint's :0 port is filled by the OS; use
// srv.Addr() for the actual address.
func startHost(t *testing.T, cfg display.Config) (*trinkets.Desktop, *display.Server, func()) {
	t.Helper()
	desktop := trinkets.NewDesktop()
	desktop.SetBackend(&nullBackend{})

	ready := make(chan *display.Server, 1)
	errc := make(chan error, 1)
	desktop.SetOnStartup(func() {
		srv, err := display.ServeConfig(desktop, cfg)
		if err != nil {
			errc <- err
			desktop.Quit()
			return
		}
		ready <- srv
	})
	exited := make(chan int, 1)
	go func() { exited <- desktop.Run() }()

	select {
	case srv := <-ready:
		return desktop, srv, func() {
			srv.Close()
			desktop.Quit()
			select {
			case <-exited:
			case <-time.After(5 * time.Second):
				t.Error("desktop did not exit")
			}
		}
	case err := <-errc:
		t.Fatalf("serve: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("desktop did not start")
	}
	return nil, nil, func() {}
}

// TestTCPLoopback: a plaintext tcp:// connection on loopback is admitted
// as local and drives the host bidirectionally.
func TestTCPLoopback(t *testing.T) {
	_, srv, stop := startHost(t, display.Config{Endpoint: "tcp://127.0.0.1:0"})
	defer stop()

	dispatched := make(chan string, 4)
	conn, err := client.Dial("tcp://"+srv.Addr(), "TCP App", func(id string) { dispatched <- id })
	if err != nil {
		t.Fatalf("dial tcp: %v", err)
	}
	defer conn.Close()

	if _, err := conn.Build(`w=new window title="T" width=200 height=100 children={` +
		`p=new panel children={b=new button caption="Go" action=t.act}}` + "\nwb=w.p.b"); err != nil {
		t.Fatalf("build: %v", err)
	}
	// A button click server-side should dispatch across the wire; drive
	// it by exec so we don't need to find the trinket.
	// (The unix test already proves event plumbing; here we just prove
	// the transport carries a full session.)
	if _, err := conn.Exec("theme"); err != nil {
		t.Fatalf("exec over tcp: %v", err)
	}
}

// TestTLSMutualTOFU: a tls:// connection performs mutual TLS (the host
// sees a client fingerprint), the client pins the host on first use, and
// the session works end to end.
func TestTLSMutualTOFU(t *testing.T) {
	var seen display.AuthRequest
	cfg := display.Config{
		Endpoint:    "tls://127.0.0.1:0",
		PromptLocal: true, // force the authorizer to run on loopback
		Authorize: func(r display.AuthRequest) display.AuthDecision {
			seen = r
			return display.AuthAllowOnce
		},
	}
	_, srv, stop := startHost(t, cfg)
	defer stop()
	if srv.TLSFingerprint == "" {
		t.Fatal("host TLS fingerprint is empty")
	}

	endpoint := "tls://" + srv.Addr()
	conn, err := client.Dial(endpoint, "TLS App", nil)
	if err != nil {
		t.Fatalf("dial tls: %v", err)
	}
	defer conn.Close()

	// The host saw a mutual-TLS client identity for the right app.
	if seen.AppName != "TLS App" {
		t.Errorf("authorized app = %q, want %q", seen.AppName, "TLS App")
	}
	if seen.Transport != "tls" {
		t.Errorf("transport = %q, want tls", seen.Transport)
	}
	if !strings.HasPrefix(seen.Fingerprint, "sha256:") {
		t.Errorf("client fingerprint = %q, want sha256:...", seen.Fingerprint)
	}

	// The client pinned the host fingerprint in known_hosts on first use.
	kh, err := os.ReadFile(os.Getenv(client.KnownHostsEnv))
	if err != nil {
		t.Fatalf("read known_hosts: %v", err)
	}
	if !strings.Contains(string(kh), srv.Addr()) || !strings.Contains(string(kh), srv.TLSFingerprint) {
		t.Errorf("known_hosts %q missing pin for %s %s", kh, srv.Addr(), srv.TLSFingerprint)
	}

	if _, err := conn.Build(`w=new window title="S" width=200 height=100 children={new label caption="hi"}`); err != nil {
		t.Fatalf("build over tls: %v", err)
	}
}

// TestTLSPinMismatchRefused: a stale/wrong pin makes the client refuse to
// connect (the SSH "host identity changed" defense).
func TestTLSPinMismatchRefused(t *testing.T) {
	_, srv, stop := startHost(t, display.Config{Endpoint: "tls://127.0.0.1:0", PromptLocal: false})
	defer stop()

	// Pin a deliberately wrong fingerprint for this host:port.
	kh := filepath.Join(t.TempDir(), "known_hosts")
	bogus := "sha256:" + strings.Repeat("00", 32)
	if err := os.WriteFile(kh, []byte(srv.Addr()+" "+bogus+"\n"), 0o600); err != nil {
		t.Fatalf("seed known_hosts: %v", err)
	}

	_, err := client.DialWith("tls://"+srv.Addr(), "TLS App", client.DialOptions{KnownHosts: kh})
	if err == nil {
		t.Fatal("expected refusal on fingerprint mismatch, got nil")
	}
	if !strings.Contains(err.Error(), "changed") {
		t.Errorf("error = %v, want a host-identity-changed refusal", err)
	}
}

// TestTokenBypass: a matching token admits a connection the authorizer
// would otherwise deny (the headless automation path); a missing/wrong
// token falls through to the (deny) authorizer.
func TestTokenBypass(t *testing.T) {
	cfg := display.Config{
		Endpoint:    "tcp://127.0.0.1:0",
		Token:       "s3cret",
		PromptLocal: true,
		Authorize:   func(display.AuthRequest) display.AuthDecision { return display.AuthDenyOnce },
	}
	_, srv, stop := startHost(t, cfg)
	defer stop()

	// With the token: admitted despite the deny-all authorizer.
	ok, err := client.DialWith("tcp://"+srv.Addr(), "Auto", client.DialOptions{Token: "s3cret"})
	if err != nil {
		t.Fatalf("dial with token: %v", err)
	}
	ok.Close()

	// Without the token: the deny authorizer refuses.
	_, err = client.DialWith("tcp://"+srv.Addr(), "Auto", client.DialOptions{})
	if err == nil {
		t.Fatal("expected refusal without token")
	}
}
