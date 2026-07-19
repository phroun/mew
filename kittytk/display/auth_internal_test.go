package display

import (
	"path/filepath"
	"testing"
)

func req(app, fp string) AuthRequest {
	return AuthRequest{AppName: app, Fingerprint: fp, Transport: "tls", RemoteAddr: "10.0.0.9:5000"}
}

func TestAuthStoreScoping(t *testing.T) {
	st := newAuthStore(filepath.Join(t.TempDir(), "auth"))
	const fp = "sha256:aaaa"

	// Undecided before any rule.
	if _, ok := st.decide(req("Editor", fp)); ok {
		t.Fatal("undecided request reported a decision")
	}

	// Always: this fingerprint + this app only.
	if err := st.record(req("Editor", fp), AuthAllowApp); err != nil {
		t.Fatal(err)
	}
	if allow, ok := st.decide(req("Editor", fp)); !ok || !allow {
		t.Errorf("Editor@fp = (%v,%v), want allowed", allow, ok)
	}
	if _, ok := st.decide(req("Mailer", fp)); ok {
		t.Error("a different app must not inherit the per-app allow")
	}

	// Block Client: denies the fingerprint for every app, and beats the
	// existing per-app allow (deny precedence).
	if err := st.record(req("Mailer", fp), AuthDenyClient); err != nil {
		t.Fatal(err)
	}
	if allow, ok := st.decide(req("Editor", fp)); !ok || allow {
		t.Errorf("Editor@fp after Block Client = (%v,%v), want denied", allow, ok)
	}
	if allow, ok := st.decide(req("Anything", fp)); !ok || allow {
		t.Errorf("Anything@fp after Block Client = (%v,%v), want denied", allow, ok)
	}
}

func TestAuthStoreAllowClient(t *testing.T) {
	st := newAuthStore(filepath.Join(t.TempDir(), "auth"))
	const fp = "sha256:bbbb"
	if err := st.record(req("X", fp), AuthAllowClient); err != nil {
		t.Fatal(err)
	}
	for _, app := range []string{"X", "Y", "Z"} {
		if allow, ok := st.decide(req(app, fp)); !ok || !allow {
			t.Errorf("%s@fp = (%v,%v), want allowed for any app", app, allow, ok)
		}
	}
}

func TestAdmitLocalAndLockdown(t *testing.T) {
	st := newAuthStore(filepath.Join(t.TempDir(), "auth"))
	s := &Server{store: st}

	// Local is trusted by default.
	if !s.admit(AuthRequest{AppName: "Local", Local: true}, "") {
		t.Error("local connection should be admitted")
	}

	// A remote, undecided connection with no authorizer is refused.
	r := req("Remote", "sha256:cccc")
	if s.admit(r, "") {
		t.Error("undecided remote connection should be refused (no authorizer)")
	}

	// An authorizer that says Always is admitted and its choice persists.
	s.authorize = func(AuthRequest) AuthDecision { return AuthAllowApp }
	if !s.admit(r, "") {
		t.Fatal("authorizer Always should admit")
	}
	s.authorize = nil // now rely on the persisted rule
	if !s.admit(r, "") {
		t.Error("persisted Always should re-admit without the authorizer")
	}

	// Lockdown: a *new* undecided client is refused without prompting.
	s.preTrustedOnly.Store(true)
	if s.admit(req("Remote", "sha256:dddd"), "") {
		t.Error("PreTrustedOnly must refuse an untrusted client")
	}
	// ...but the already-trusted one still gets in.
	if !s.admit(r, "") {
		t.Error("PreTrustedOnly must still admit a stored-allow client")
	}
}

func TestAdmitTokenBypass(t *testing.T) {
	s := &Server{
		store:       newAuthStore(filepath.Join(t.TempDir(), "auth")),
		token:       "open-sesame",
		authorize:   func(AuthRequest) AuthDecision { return AuthDenyOnce },
		promptLocal: true,
	}
	r := req("Auto", "sha256:eeee")
	if !s.admit(r, "open-sesame") {
		t.Error("matching token should bypass the deny authorizer")
	}
	if s.admit(r, "wrong") {
		t.Error("wrong token should fall through to the deny authorizer")
	}
}
