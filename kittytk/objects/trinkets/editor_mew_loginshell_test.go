//go:build mew

package trinkets

import (
	"os"
	"runtime"
	"strings"
	"testing"

	mew "github.com/phroun/mew"
)

// hostLoginShell is this HOST's answer to mew's `shell` command. mew never
// names a shell: which one a user logs in with belongs to the operating system
// and to their own account, so the question travels as a flag and the host
// resolves it. The environment holds the authoritative answer on both
// platforms — $SHELL on POSIX, %COMSPEC% on Windows.
func TestHostLoginShellPrefersTheEnvironment(t *testing.T) {
	key := "SHELL"
	if runtime.GOOS == "windows" {
		key = "COMSPEC"
	}
	t.Setenv(key, "/opt/pretend/fish")
	if got := hostLoginShell(); got != "/opt/pretend/fish" {
		t.Errorf("hostLoginShell() = %q, want the value of %s", got, key)
	}
}

// An environment that exports nothing still gets an answer: the one the
// platform guarantees is there. A blank would surface as "no command given",
// which is true of mew's request and false of the machine.
func TestHostLoginShellFallsBack(t *testing.T) {
	key, want := "SHELL", "/bin/sh"
	if runtime.GOOS == "windows" {
		key, want = "COMSPEC", "cmd.exe"
	}
	t.Setenv(key, "")
	if got := hostLoginShell(); got != want {
		t.Errorf("hostLoginShell() = %q, want %q", got, want)
	}
}

// A shell request that ALSO names a program is refused rather than resolved one
// way or the other: the two spellings mean different things, and quietly
// preferring either would make `shell` and `exec` indistinguishable from the
// outside. Nothing is started, so no child escapes the test.
func TestPTYProviderRefusesAShellRequestThatNamesAProgram(t *testing.T) {
	e := &Editor{}
	sess, err := e.ptyProvider(mew.PTYRequest{Shell: true, Command: "bash", Cols: 80, Rows: 24})
	if err == nil {
		if sess != nil {
			sess.Close()
		}
		t.Fatal("a shell request naming a program should have been refused")
	}
	if !strings.Contains(err.Error(), "bash") {
		t.Errorf("error %q should say which program was named", err)
	}
}

// The provider resolves the shell from the environment for a shell request —
// and reports it by NAME when it cannot be found, which is the observable proof
// that the environment's answer is the one being looked up. A real session is
// not started: a name that exists nowhere on PATH cannot spawn anything.
func TestPTYProviderResolvesTheLoginShell(t *testing.T) {
	key := "SHELL"
	if runtime.GOOS == "windows" {
		key = "COMSPEC"
	}
	t.Setenv(key, "mew-no-such-login-shell")
	// A home directory must exist for the blank-CWD path; it always does, and
	// the lookup fails before the directory is used in any case.
	if _, err := os.UserHomeDir(); err != nil {
		t.Skip("no home directory in this environment")
	}
	e := &Editor{}
	sess, err := e.ptyProvider(mew.PTYRequest{Shell: true, Cols: 80, Rows: 24})
	if err == nil {
		if sess != nil {
			sess.Close()
		}
		t.Fatal("a shell that does not exist should have been refused")
	}
	if !strings.Contains(err.Error(), "mew-no-such-login-shell") {
		t.Errorf("error %q should name the shell resolved from %s", err, key)
	}
}
