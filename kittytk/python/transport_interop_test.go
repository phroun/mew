package interop_test

// The same Python interop exchange, but over tcp:// and tls:// instead of
// a unix socket - proving the Python client speaks every transport the Go
// client does. The tls:// case exercises mutual TLS + trust-on-first-use
// pinning end to end (the client generates an identity, pins the host).

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/phroun/kittytk/display"
	"github.com/phroun/kittytk/objects/trinkets"
)

// startServiceCfg boots a headless desktop serving cfg and returns the
// desktop, its actual address, and a cleanup.
func startServiceCfg(t *testing.T, cfg display.Config) (*trinkets.Desktop, string, func()) {
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
		return desktop, srv.Addr(), func() {
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
	return nil, "", func() {}
}

func TestPythonClientInteropTCP(t *testing.T) {
	desktop, addr, stop := startServiceCfg(t, display.Config{Endpoint: "tcp://127.0.0.1:0"})
	defer stop()
	exchange(t, "python-tcp", desktop, "tcp://"+addr, nil, "python3", "interop_smoke.py")
}

func TestPythonClientInteropTLS(t *testing.T) {
	// Isolate the host identity + authorizations for this test.
	dir := t.TempDir()
	t.Setenv(display.HostIdentityEnv, filepath.Join(dir, "host_identity.pem"))
	t.Setenv(display.AuthStoreEnv, filepath.Join(dir, "authorizations"))

	desktop, addr, stop := startServiceCfg(t, display.Config{Endpoint: "tls://127.0.0.1:0"})
	defer stop()

	// The Python client gets its own throwaway identity + known_hosts, so
	// TOFU pins the host on this first (and only) connect.
	cdir := t.TempDir()
	env := []string{
		"KITTYTK_IDENTITY=" + filepath.Join(cdir, "identity.pem"),
		"KITTYTK_KNOWN_HOSTS=" + filepath.Join(cdir, "known_hosts"),
	}
	exchange(t, "python-tls", desktop, "tls://"+addr, env, "python3", "interop_smoke.py")

	// The client must have pinned the host fingerprint.
	if b, err := os.ReadFile(filepath.Join(cdir, "known_hosts")); err != nil || len(b) == 0 {
		t.Errorf("client did not pin the host in known_hosts (err=%v)", err)
	}
}

// TestPythonTwoTLSConnections is the Python analogue of the C two-
// connection TLS stress: a second connection builds while its reader
// thread is mid-read, which corrupts a shared SSL object if reads and
// writes are not serialized.
func TestPythonTwoTLSConnections(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(display.HostIdentityEnv, filepath.Join(dir, "host_identity.pem"))
	t.Setenv(display.AuthStoreEnv, filepath.Join(dir, "authorizations"))

	_, addr, stop := startServiceCfg(t, display.Config{
		Endpoint:    "tls://127.0.0.1:0",
		PromptLocal: true,
		Authorize:   func(display.AuthRequest) display.AuthDecision { return display.AuthAllowOnce },
	})
	defer stop()

	cdir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", "twoconn_smoke.py", "tls://"+addr)
	cmd.Env = append(os.Environ(),
		"KITTYTK_IDENTITY="+filepath.Join(cdir, "identity.pem"),
		"KITTYTK_KNOWN_HOSTS="+filepath.Join(cdir, "known_hosts"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python two-connection TLS smoke failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "OK") {
		t.Fatalf("python two-connection TLS smoke did not report OK:\n%s", out)
	}
}
