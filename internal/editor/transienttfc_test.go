package editor

import (
	"strings"
	"testing"

	"github.com/phroun/mew/internal/config"
	"github.com/phroun/mew/internal/keys"
)

// A TFC-enabled transient expands %keys#…% codes to the live binding, wrapping
// the badge in the "key" color and closing with the class's "messages" color so
// the surrounding text returns to the bar's color.
func TestExpandTransientTFC(t *testing.T) {
	e := &Editor{}
	e.KeyProcessor = keys.NewSequenceProcessor(nil)
	e.KeyProcessor.SetMappings(map[string]string{"^K H": "help_toggle"})
	e.LoadedConfig = config.DefaultConfig()

	got := e.expandTransientTFC("Press %keys_verbose#help_toggle|^Q H% for help.", "notification")

	keyColor := e.LoadedConfig.Colors.Resolve("notification", "tool", "key")
	barColor := e.LoadedConfig.Colors.Resolve("notification", "tool", "messages")
	wantBadge := keyColor + "Ctrl+K then H" + barColor
	if !strings.Contains(got, wantBadge) {
		t.Fatalf("badge not wrapped in key/messages colors\n got %q\nwant contains %q", got, wantBadge)
	}
	if strings.Contains(got, "%keys_verbose#") {
		t.Fatalf("TFC code left unexpanded: %q", got)
	}
	if !strings.HasPrefix(got, "Press ") || !strings.HasSuffix(got, " for help.") {
		t.Fatalf("surrounding text mangled: %q", got)
	}
}

// TFC expansion is opt-in per notification: ShowNotificationTFC expands, plain
// ShowNotification leaves the code verbatim.
func TestTransientTFCIsOptIn(t *testing.T) {
	e, _ := newTestEditor(t, "hi\n")
	e.KeyProcessor.SetMappings(map[string]string{"^K H": "help_toggle"})
	msg := "Press %keys_verbose#help_toggle% for help."

	notifText := func() string {
		for _, w := range e.WindowManager.AllWindows() {
			if w.Class == "notification" {
				return w.MessageTopInner
			}
		}
		return ""
	}

	e.ShowNotification(msg)
	if got := notifText(); !strings.Contains(got, "%keys_verbose#") {
		t.Fatalf("plain ShowNotification should keep the raw code, got %q", got)
	}
	clearNotifications(e)

	e.ShowNotificationTFC(msg)
	if got := notifText(); strings.Contains(got, "%keys_verbose#") || !strings.Contains(got, "Ctrl+K then H") {
		t.Fatalf("ShowNotificationTFC should expand the code, got %q", got)
	}
}
