package main

// The Make trouble button, and the three things that have to agree for it to do
// anything.
//
// It is a demonstration of a path with several hands on it -- a document on the
// shelf, a name in a statement, a command id -- and every one of them is a string
// that can be changed on its own. A button that quietly does nothing is the worst
// outcome available, this being the button for showing that nothing is quiet.

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/phroun/kittytk/client"
)

// The button is in the build, and the action it fires is the one the app listens
// for. Two files, one string.
func TestTheMakeTroubleButtonIsWiredToItsCommand(t *testing.T) {
	script := mainBuildScript()
	if !strings.Contains(script, "action=demo.trouble") {
		t.Error("the build names no demo.trouble action, so the button fires nothing")
	}
	if !strings.Contains(script, "litrouble=w.t.li.liv.litrouble") {
		t.Error("the button's key is not surfaced")
	}

	src, err := os.ReadFile("trouble.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), `OnCommand("demo.trouble"`) {
		t.Error("nothing listens for demo.trouble, so the button does nothing")
	}
}

// The bundle is on the shelf, under the key its own document declares.
//
// **A bundle is found by key AND version**, so the shelf key is the two spelled
// with a hyphen between them. Changing one without the other leaves a document on
// the shelf that nothing can name, and a button whose only sign of that is a
// status line saying nothing was said.
func TestTheTroubleBundleIsStockedUnderTheNameItDeclares(t *testing.T) {
	found := false
	for _, s := range samples() {
		if s.key == troubleBundleKey {
			found = true
			if s.typ != "psl" {
				t.Errorf("the bundle is stocked as %q, want psl", s.typ)
			}
			if string(s.body) != troubleBundle {
				t.Error("the stocked body is not the bundle")
			}
		}
	}
	if !found {
		t.Fatalf("nothing on the shelf is called %q", troubleBundleKey)
	}

	key := regexp.MustCompile(`key:\s*"([^"]+)"`).FindStringSubmatch(troubleBundle)
	version := regexp.MustCompile(`version:\s*"([^"]+)"`).FindStringSubmatch(troubleBundle)
	if key == nil || version == nil {
		t.Fatal("the document declares no key and version of its own")
	}
	if want := key[1] + "-" + version[1]; want != troubleBundleKey {
		t.Errorf("the document calls itself %q and it is shelved as %q",
			want, troubleBundleKey)
	}
	// And the statement asks for it by the key alone, which is how a bundle is
	// named: `bundle:<key>`, the version being the store's to choose.
	if want := "bundle:" + key[1]; want != troubleBundleName {
		t.Errorf("the statement names %q and the document is %q", troubleBundleName, want)
	}
}

// The trouble it makes is a REPORT and not a refusal: the hint cannot mean what
// it says, and the records are all still there. A bundle that refused would fail
// the statement instead, and the button would demonstrate the other thing.
func TestTheTroubleBundleLoadsRatherThanRefusing(t *testing.T) {
	if !strings.Contains(troubleBundle, "parent:") || !strings.Contains(troubleBundle, "location:") {
		t.Error("the hint is no longer two ways down at once, so nothing is wrong with it")
	}
	if !strings.Contains(troubleBundle, `("first record")`) {
		t.Error("the bundle holds no records, so there is nothing for it to load")
	}
}

// And the whole path, against a real display: the bundle goes on the shelf, the
// button's own function names it, and what comes back is the complaint.
//
// The three strings the tests above check separately are checked together here,
// by the code that actually runs when the button is clicked -- and by the loader
// that actually reads the document, which is the half no amount of string
// comparison can stand in for.
func TestTheButtonsTroubleComesBackFromARealDisplay(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // this test's own shelves
	sock, stop := startService(t)
	defer stop()

	conn, err := client.DialWith(sock, "KittyTK Demo", client.DialOptions{MultiWindow: true})
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// What stockTheStore does for this one document, without the rest of the shelf.
	if err := conn.Store().Write(troubleBundleKey, "psl", []byte(troubleBundle)); err != nil {
		t.Fatalf("stocking the bundle: %v", err)
	}

	// The statement the button sends, and the reply it reads.
	reply, err := conn.Exec(`muddle=new listview source="` + troubleBundleName + `"`)
	if err != nil {
		t.Fatalf("the statement was refused, so the bundle did not load: %v", err)
	}
	if reply.IDs["muddle"] == 0 {
		t.Error("no list was made, so the complaint refused something after all")
	}
	if len(reply.Trouble) == 0 {
		t.Fatal("the bundle loaded and said nothing, so the button demonstrates nothing")
	}
	if got := reply.Trouble[0].About; got != troubleBundleName {
		t.Errorf("the complaint is about %q, want the name the statement used", got)
	}
	if got := reply.Trouble[0].Text; !strings.Contains(got, "two ways down") {
		t.Errorf("it reads %q, want what the loader said about the hint", got)
	}
}
