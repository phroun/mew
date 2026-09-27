package style

// An icon is a NAME, and the registry is what turns one into something to
// draw. These are the answers everything that paints an icon depends on.

import "testing"

func small(text string) TextIcon { return NewSmallTextIcon(text, DefaultStyle()) }
func large(a, b string) TextIcon { return NewLargeTextIcon(a, b, DefaultStyle()) }

// first is the first glyph of an icon, which is enough to tell two apart.
func first(i TextIcon) rune { return i.CellAt(0, 0).Char }

// A name nothing has registered is not an error and not a panic. It draws
// nothing, because an icon set is registered by whatever dresses an
// application and a trinket naming one before that has happened is early.
func TestAnUnregisteredNameDrawsNothing(t *testing.T) {
	if got := LookupIcon("nobody.registered.this"); got != nil {
		t.Errorf("an unregistered name found %v", got)
	}
	for _, size := range []IconSize{IconSmall, IconLarge} {
		if _, ok := IconText("nobody.registered.this", size); ok {
			t.Errorf("an unregistered name answered at size %v", size)
		}
	}
	// And the empty name, which is what a trinket carries when it has no icon
	// at all, is the same answer rather than a special case.
	if _, ok := IconText("", IconSmall); ok {
		t.Error("the empty name answered")
	}
}

// An icon registered with only a graphical form has a name and no text, which
// is a different answer from having no name.
func TestARegisteredNameWithNoTextAnswersFalse(t *testing.T) {
	RegisterIcon(NewIcon("t.graphics.only").WithGraphics("somewhere.svg"))
	if LookupIcon("t.graphics.only") == nil {
		t.Fatal("it did not register")
	}
	if _, ok := IconText("t.graphics.only", IconSmall); ok {
		t.Error("a graphics-only icon answered with text")
	}
}

// The size asked for is the size given, where the icon has it.
func TestTheSizeAskedForIsTheOneGiven(t *testing.T) {
	RegisterIcon(&Icon{ID: "t.both", TextSmall: small("<->"), TextLarge: large("ab", "cd")})

	got, ok := IconText("t.both", IconSmall)
	if !ok || first(got) != '<' {
		t.Errorf("small asked for, %q given (ok=%v)", first(got), ok)
	}
	got, ok = IconText("t.both", IconLarge)
	if !ok || first(got) != 'a' {
		t.Errorf("large asked for, %q given (ok=%v)", first(got), ok)
	}
}

// And where it has not, the other size is given rather than nothing: a picture
// at the wrong size is nearer to what was meant than a blank. BOTH ways round,
// because falling back only one way is the bug this is here to catch.
func TestASizeThatIsMissingFallsBackToTheOther(t *testing.T) {
	RegisterIcon(&Icon{ID: "t.small.only", TextSmall: small("<->")})
	RegisterIcon(&Icon{ID: "t.large.only", TextLarge: large("ab", "cd")})

	got, ok := IconText("t.small.only", IconLarge)
	if !ok || first(got) != '<' {
		t.Errorf("large asked of a small-only icon: %q (ok=%v)", first(got), ok)
	}
	got, ok = IconText("t.large.only", IconSmall)
	if !ok || first(got) != 'a' {
		t.Errorf("small asked of a large-only icon: %q (ok=%v)", first(got), ok)
	}
}

// A name means ONE thing, so registering it again is the new answer and not a
// second icon beside the first.
func TestRegisteringANameAgainReplacesIt(t *testing.T) {
	RegisterIcon(&Icon{ID: "t.replaced", TextSmall: small("<->")})
	RegisterIcon(&Icon{ID: "t.replaced", TextSmall: small("[x]")})

	got, ok := IconText("t.replaced", IconSmall)
	if !ok || first(got) != '[' {
		t.Errorf("the second registration did not take: %q (ok=%v)", first(got), ok)
	}
}

// Nothing without a name can be registered, there being no way to ask for it
// afterwards.
func TestAnIconWithNoNameIsNotRegistered(t *testing.T) {
	RegisterIcon(&Icon{TextSmall: small("<->")})
	if LookupIcon("") != nil {
		t.Error("an icon registered itself under no name")
	}
	RegisterIcon(nil) // and this is a no-op rather than a panic
}
