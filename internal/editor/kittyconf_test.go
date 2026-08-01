package editor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScanKittyForceLTR(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	// A missing file reports found=false so the caller keeps its default.
	if v, ok := scanKittyForceLTR(filepath.Join(dir, "nope.conf"), dir, 0); ok || v {
		t.Errorf("missing file: got (%v,%v), want (false,false)", v, ok)
	}

	// Explicit force_ltr yes.
	p := write("y.conf", "font_size 12\nforce_ltr yes\n")
	if v, ok := scanKittyForceLTR(p, dir, 0); !ok || !v {
		t.Errorf("force_ltr yes: got (%v,%v), want (true,true)", v, ok)
	}

	// Present file, option absent -> found but default (false).
	p = write("d.conf", "font_size 12\n")
	if v, ok := scanKittyForceLTR(p, dir, 0); !ok || v {
		t.Errorf("absent option: got (%v,%v), want (false,true)", v, ok)
	}

	// A comment line does not count as an assignment.
	p = write("c.conf", "# force_ltr yes\n")
	if v, ok := scanKittyForceLTR(p, dir, 0); !ok || v {
		t.Errorf("commented option: got (%v,%v), want (false,true)", v, ok)
	}

	// Last assignment wins.
	p = write("l.conf", "force_ltr yes\nforce_ltr no\n")
	if v, ok := scanKittyForceLTR(p, dir, 0); !ok || v {
		t.Errorf("last-wins: got (%v,%v), want (false,true)", v, ok)
	}

	// An included file's value applies.
	write("inc.conf", "force_ltr yes\n")
	p = write("main.conf", "include inc.conf\n")
	if v, ok := scanKittyForceLTR(p, dir, 0); !ok || !v {
		t.Errorf("include: got (%v,%v), want (true,true)", v, ok)
	}

	// An assignment after the include still wins (file order preserved).
	p = write("main2.conf", "include inc.conf\nforce_ltr no\n")
	if v, ok := scanKittyForceLTR(p, dir, 0); !ok || v {
		t.Errorf("post-include override: got (%v,%v), want (false,true)", v, ok)
	}
}
