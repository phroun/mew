package display

// The clean name a host or an app is filed under: what survives cleaning, and
// what happens when two of them clean down to the same word.

import "testing"

func TestANameIsCleanedDownToWhatAPathWillTake(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"KittyTK Demo", "kittytk-demo"},
		{"Jeff's Laptop", "jeff-s-laptop"},
		{"  Editor  ", "editor"},
		{"a//b", "a-b"},
		{"-- Scratch --", "scratch"},
		{"App 2", "app-2"},
		// Only ASCII survives, and what it leaves behind is separated the way
		// any other gap is -- a nickname written in another script keeps
		// nothing and falls back to what it is instead.
		{"Ünïcödé", "n-c-d"},
		{"デスクトップ", ""},
		{"...", ""},
		{"", ""},
	} {
		if got := cleanName(c.in); got != c.want {
			t.Errorf("%q cleans to %q, want %q", c.in, got, c.want)
		}
	}
}

// A run of punctuation is one dash, not one per character, and a name never
// begins or ends with one -- a directory called `-jeff-` is nobody's idea of a
// clean name.
func TestPunctuationDoesNotLeaveARowOfDashes(t *testing.T) {
	got := cleanName("!! Jeff's   ***   Laptop !!")
	if got != "jeff-s-laptop" {
		t.Errorf("cleaned to %q", got)
	}
}

// The first to claim a name gets it bare. Numbering something there is only one
// of reads as one of several, and the whole point of the number is to tell two
// apart.
func TestTheFirstToClaimANameGetsItBare(t *testing.T) {
	if got := safeName("KittyTK Demo", unnamedApp, map[string]bool{}); got != "kittytk-demo" {
		t.Errorf("the only demo is filed as %q", got)
	}
}

// The second gets a number, and so does the third -- each the lowest that
// nobody holds, so a name freed by a forgotten client is used again rather than
// counting up forever.
func TestTwoNamesThatCleanAlikeAreToldApart(t *testing.T) {
	taken := map[string]bool{}
	for _, want := range []string{"kittytk-demo", "kittytk-demo-2", "kittytk-demo-3"} {
		got := safeName("KittyTK Demo", unnamedApp, taken)
		if got != want {
			t.Fatalf("filed as %q, want %q", got, want)
		}
		taken[got] = true
	}

	delete(taken, "kittytk-demo-2")
	if got := safeName("KittyTK Demo!", unnamedApp, taken); got != "kittytk-demo-2" {
		t.Errorf("with -2 free the next one is filed as %q", got)
	}
}

// A name that cleans away to nothing is filed under what it is rather than
// under the empty string, which is not a directory name at all.
func TestANameThatCleansAwayFallsBackToWhatItIs(t *testing.T) {
	taken := map[string]bool{}
	first := safeName("", unnamedHost, taken)
	if first != "host" {
		t.Errorf("a client with no nickname is filed as %q", first)
	}
	taken[first] = true
	if second := safeName("!!!", unnamedHost, taken); second != "host-2" {
		t.Errorf("a second nameless client is filed as %q", second)
	}
	if got := safeName("...", unnamedApp, map[string]bool{}); got != "app" {
		t.Errorf("a nameless app is filed as %q", got)
	}
}

// A name that is already a number-suffixed one is not confused for a claim on
// the base: `demo-2` typed by hand takes `demo-2` and leaves `demo` alone.
func TestASuffixedNameIsJustAName(t *testing.T) {
	taken := map[string]bool{"demo": true}
	if got := safeName("demo-2", unnamedApp, taken); got != "demo-2" {
		t.Errorf("filed as %q, want demo-2", got)
	}
}
