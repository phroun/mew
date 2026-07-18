package version

import "testing"

func TestFullVersion(t *testing.T) {
	if got := FullVersion(); got != Version+".1" && Build == 1 {
		t.Fatalf("FullVersion() = %q", got)
	}
}

func TestBannerExact(t *testing.T) {
	want := "mew edits words 0.3 build 1 ** Type Ctrl-C to close or Ctrl-K then H for help."
	if Build == 1 && Version == "0.3" && Banner() != want {
		t.Fatalf("Banner() = %q\nwant       %q", Banner(), want)
	}
}
