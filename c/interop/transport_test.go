package interop_c_test

// The same C interop exchange over tcp:// and tls:// instead of a unix
// socket. The tls:// case compiles the C client with -DKT_TLS and links
// OpenSSL, exercising mutual TLS + trust-on-first-use pinning end to end.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/display"
	"github.com/phroun/kittytk/objects/trinkets"
)

func walkButtons(root core.Trinket, out *[]*trinkets.Button) {
	if b, ok := root.(*trinkets.Button); ok {
		*out = append(*out, b)
	}
	if c, ok := root.(core.Container); ok {
		for _, k := range c.Children() {
			walkButtons(k, out)
		}
	}
}

func clickButtonByLabel(t *testing.T, d *trinkets.Desktop, label string) bool {
	t.Helper()
	clicked := false
	onUI(d, func() {
		for _, a := range d.Applications() {
			for _, w := range a.Windows() {
				var bs []*trinkets.Button
				walkButtons(w.Content(), &bs)
				for _, b := range bs {
					if b.Text() == label {
						b.Click()
						clicked = true
						return
					}
				}
			}
		}
	})
	return clicked
}

// buildCTLS compiles the smoke with TLS enabled (OpenSSL). It skips if
// cc or the OpenSSL toolchain isn't available.
func buildCTLS(t *testing.T, src string) string {
	t.Helper()
	if _, err := exec.LookPath("cc"); err != nil {
		t.Skipf("cc not available: %v", err)
	}
	bin := filepath.Join(t.TempDir(), "smoke_tls")
	cmd := exec.Command("cc", "-std=c11", "-O2", "-DKT_TLS", "-o", bin,
		filepath.Join("..", src), filepath.Join("..", "kittytk.c"),
		filepath.Join("..", "scripts.c"), "-lpthread", "-lssl", "-lcrypto")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("cc -DKT_TLS %s (OpenSSL unavailable?): %v\n%s", src, err, out)
	}
	return bin
}

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

func TestCClientInteropTCP(t *testing.T) {
	bin := buildC(t, "interop_smoke.c")
	desktop, addr, stop := startServiceCfg(t, display.Config{Endpoint: "tcp://127.0.0.1:0"})
	defer stop()
	driveCInterop(t, desktop, bin, "tcp://"+addr, nil)
}

func TestCClientInteropTLS(t *testing.T) {
	bin := buildCTLS(t, "interop_smoke.c")

	dir := t.TempDir()
	t.Setenv(display.HostIdentityEnv, filepath.Join(dir, "host_identity.pem"))
	t.Setenv(display.AuthStoreEnv, filepath.Join(dir, "authorizations"))

	desktop, addr, stop := startServiceCfg(t, display.Config{Endpoint: "tls://127.0.0.1:0"})
	defer stop()

	cdir := t.TempDir()
	env := []string{
		"KITTYTK_IDENTITY=" + filepath.Join(cdir, "identity.pem"),
		"KITTYTK_KNOWN_HOSTS=" + filepath.Join(cdir, "known_hosts"),
	}
	driveCInterop(t, desktop, bin, "tls://"+addr, env)

	if b, err := os.ReadFile(filepath.Join(cdir, "known_hosts")); err != nil || len(b) == 0 {
		t.Errorf("C client did not pin the host in known_hosts (err=%v)", err)
	}
}

// TestCCmdSecondTLS is the faithful demoapp "New Window" path: conn1's
// command handler (on conn1's event thread) dials a SECOND TLS connection
// and builds on it. The host fires the command by clicking conn1's
// button. If the second connection's batch or its terminator is lost, the
// handler blocks forever and the smoke times out.
func TestCCmdSecondTLS(t *testing.T) {
	bin := buildCTLS(t, "cmdsecond_smoke.c")

	dir := t.TempDir()
	t.Setenv(display.HostIdentityEnv, filepath.Join(dir, "host_identity.pem"))
	t.Setenv(display.AuthStoreEnv, filepath.Join(dir, "authorizations"))
	desktop, addr, stop := startServiceCfg(t, display.Config{
		Endpoint:    "tls://127.0.0.1:0",
		PromptLocal: true,
		Authorize:   func(display.AuthRequest) display.AuthDecision { return display.AuthAllowOnce },
	})
	defer stop()

	cdir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "tls://"+addr)
	cmd.Env = append(os.Environ(),
		"KITTYTK_IDENTITY="+filepath.Join(cdir, "identity.pem"),
		"KITTYTK_KNOWN_HOSTS="+filepath.Join(cdir, "known_hosts"))
	var buf strings.Builder
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}

	// Click conn1's "New" button to fire the command on its event thread.
	deadline := time.Now().Add(10 * time.Second)
	for !clickButtonByLabel(t, desktop, "New") {
		if time.Now().After(deadline) {
			_ = cmd.Process.Kill()
			t.Fatalf("conn1's New button never appeared\n%s", buf.String())
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err := cmd.Wait(); err != nil {
		t.Fatalf("cmd-second TLS smoke failed (stuck/second connection lost): %v\n%s", err, buf.String())
	}
	if !strings.Contains(buf.String(), "OK") {
		t.Fatalf("cmd-second TLS smoke did not report OK:\n%s", buf.String())
	}
}

// TestCTwoTLSConnections reproduces the demoapp "New Window" stall: a
// second TLS connection builds while its reader thread is mid-SSL_read.
// Before the SSL-serialization fix this corrupted the outbound record
// and build2 hung; the client must now complete and print OK.
func TestCTwoTLSConnections(t *testing.T) {
	bin := buildCTLS(t, "twoconn_smoke.c")

	dir := t.TempDir()
	t.Setenv(display.HostIdentityEnv, filepath.Join(dir, "host_identity.pem"))
	t.Setenv(display.AuthStoreEnv, filepath.Join(dir, "authorizations"))

	// Auto-admit both connections (no interactive prompt in the harness).
	_, addr, stop := startServiceCfg(t, display.Config{
		Endpoint:    "tls://127.0.0.1:0",
		PromptLocal: true,
		Authorize:   func(display.AuthRequest) display.AuthDecision { return display.AuthAllowOnce },
	})
	defer stop()

	cdir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "tls://"+addr)
	cmd.Env = append(os.Environ(),
		"KITTYTK_IDENTITY="+filepath.Join(cdir, "identity.pem"),
		"KITTYTK_KNOWN_HOSTS="+filepath.Join(cdir, "known_hosts"))
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("two-connection TLS smoke failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "OK") {
		t.Fatalf("two-connection TLS smoke did not report OK:\n%s", out)
	}
}
