package protocol

import "testing"

func TestEventEncodeAndParseRoundTrip(t *testing.T) {
	ev := NewEvent("toggle").
		WithUint("trinket", 17).
		WithFlag("checked", FlagIndeterminate)

	encoded := ev.Encode()
	if encoded != `event toggle trinket=17 ?checked` {
		t.Fatalf("encoded = %q", encoded)
	}

	back, err := ParseEvent(encoded)
	if err != nil {
		t.Fatalf("ParseEvent: %v", err)
	}
	if back.Type != "toggle" {
		t.Errorf("type = %q", back.Type)
	}
	if id, ok := back.Trinket(); !ok || id != 17 {
		t.Errorf("trinket = %d/%v", id, ok)
	}
	if back.Flag("checked") != FlagIndeterminate {
		t.Errorf("checked = %v", back.Flag("checked"))
	}
}

func TestEventStringEscaping(t *testing.T) {
	ev := NewEvent("change").
		WithUint("trinket", 3).
		WithString("text", "a \"quoted\"\nline\\end")

	back, err := ParseEvent(ev.Encode())
	if err != nil {
		t.Fatalf("ParseEvent(%q): %v", ev.Encode(), err)
	}
	if s, ok := back.Text("text"); !ok || s != "a \"quoted\"\nline\\end" {
		t.Errorf("text = %q/%v", s, ok)
	}
}

func TestEventDispatcherRouting(t *testing.T) {
	d := NewEventDispatcher()

	var byTrinket, byType int
	d.On(17, "toggle", func(*Event) { byTrinket++ })
	d.OnType("command", func(*Event) { byType++ })

	if !d.Dispatch(NewEvent("toggle").WithUint("trinket", 17).WithFlag("checked", FlagTrue)) {
		t.Error("trinket-scoped event should be handled")
	}
	// Different trinket: not routed.
	if d.Dispatch(NewEvent("toggle").WithUint("trinket", 99).WithFlag("checked", FlagTrue)) {
		t.Error("unsubscribed trinket should not be handled")
	}
	if !d.Dispatch(NewEvent("command").WithWord("action", "file.open")) {
		t.Error("type-scoped event should be handled")
	}
	if byTrinket != 1 || byType != 1 {
		t.Errorf("byTrinket=%d byType=%d", byTrinket, byType)
	}
}
