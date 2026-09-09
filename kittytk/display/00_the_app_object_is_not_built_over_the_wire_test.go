package display_test

// The connection arrives with its Application and the host registers it. A
// client asking for another one is refused -- and refused, not obeyed and not
// crashed into.

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/phroun/kittytk/client"
	"github.com/phroun/kittytk/display"
)

// An admitted client is not a trusted one. `new application` is a statement any
// of them can send, so what it does has to be a refusal the host walks away
// from -- a host that falls over on one statement is a desktop any app can take
// down, and every other app on it goes too.
func TestAskingForAnotherApplicationIsRefusedNotObeyed(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "d.sock")
	desktop, _, stop := startHost(t, display.Config{Endpoint: sock})
	defer stop()

	conn, err := client.Dial(sock, "Probe App", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	for _, src := range []string{
		"a=new application",
		`a=new application name="Impostor"`,
		"new application",
		"p=new panel children={\n  a=new application\n}\n",
	} {
		reply, err := conn.Exec(src)
		if err == nil {
			t.Errorf("%q was accepted, and answered %+v", src, reply)
			continue
		}
		if !strings.Contains(err.Error(), "not created over the wire") {
			t.Errorf("%q was refused with %q, which does not say why", src, err)
		}
	}

	// Still up, and still one app on the desktop rather than five.
	var apps int
	onUI(desktop, func() { apps = len(desktop.Applications()) })
	if apps != 1 {
		t.Errorf("the desktop holds %d applications after four attempts to make more", apps)
	}
	if _, err := conn.Exec(`w=new window title="alive" width=80 height=40`); err != nil {
		t.Errorf("the connection did not survive: %v", err)
	}
}

// The app object a client DOES hold still takes the properties it always did.
// Refusing to build one is not refusing to speak to the one there is.
func TestTheAppObjectItWasGivenStillAnswers(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "d.sock")
	desktop, _, stop := startHost(t, display.Config{Endpoint: sock})
	defer stop()

	conn, err := client.Dial(sock, "Named App", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	if conn.AppID() == 0 {
		t.Fatal("the handshake handed over no application id")
	}
	if err := conn.App().Set(`multiwindow name="Renamed"`); err != nil {
		t.Fatalf("setting the app's own properties: %v", err)
	}

	var names []string
	onUI(desktop, func() {
		for _, a := range desktop.Applications() {
			names = append(names, a.Name())
		}
	})
	if len(names) != 1 || names[0] != "Renamed" {
		t.Errorf("the desktop's applications read as %v", names)
	}
}

// The vocabulary says so too: the app object is described, and described as one
// the wire does not construct. A client reading the vocabulary to find out what
// it can build must not be told it can build this.
func TestTheVocabularySaysTheAppObjectIsNotBuilt(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "d.sock")
	_, _, stop := startHost(t, display.Config{Endpoint: sock})
	defer stop()

	conn, err := client.Dial(sock, "Reader App", nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	vocab, err := conn.Describe()
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, ty := range vocab.Types {
		if ty.Name != "application" {
			continue
		}
		found = true
		if !ty.Hosted {
			t.Error("the vocabulary offers `new application`")
		}
		var props []string
		for _, p := range ty.Props {
			props = append(props, p.Name)
		}
		for _, want := range []string{"name", "multiwindow", "contextonly"} {
			if !contains(props, want) {
				t.Errorf("the app object does not describe %s; it describes %v", want, props)
			}
		}
	}
	if !found {
		t.Error("the vocabulary does not mention the app object at all")
	}
	// Every trinket is still built the way it always was.
	for _, ty := range vocab.Types {
		if ty.Name == "button" && ty.Hosted {
			t.Error("a button reads as an object the wire cannot build")
		}
	}
}

func contains(all []string, want string) bool {
	for _, s := range all {
		if s == want {
			return true
		}
	}
	return false
}
