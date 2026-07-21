// Package trinkets: display-protocol registration infrastructure.
//
// Per the project architecture, each trinket's own codebase registers
// its wire type and property mappings in a sibling *_protocol.go file
// (see button_protocol.go etc.); this file provides the shared helpers
// and registers the COMMON properties every trinket supports.
package trinkets

import (
	"fmt"
	"strings"

	"github.com/phroun/kittytk/core"
	"github.com/phroun/kittytk/protocol"
	"github.com/phroun/kittytk/style"
)

// wprop adapts a trinket-typed applier to protocol.PropertyApplier.
func wprop[T any](name string, fn func(ctx *protocol.BindContext, w T, v *protocol.Value, f protocol.FlagState) error) protocol.PropertyApplier {
	return func(ctx *protocol.BindContext, target any, v *protocol.Value, f protocol.FlagState) error {
		w, ok := target.(T)
		if !ok {
			return fmt.Errorf("%s: wrong target type %T", name, target)
		}
		return fn(ctx, w, v, f)
	}
}

// stringProp is the common shape: a quoted string into a setter.
func stringProp[T any](name string, set func(w T, s string)) protocol.Property {
	return protocol.NewProperty("string", wprop(name, func(_ *protocol.BindContext, w T, v *protocol.Value, f protocol.FlagState) error {
		s, err := protocol.AsString(name, v, f)
		if err != nil {
			return err
		}
		set(w, s)
		return nil
	}))
}

// boolProp is the common shape: a flag into a setter.
func boolProp[T any](name string, set func(w T, b bool)) protocol.Property {
	return protocol.NewProperty("flag", wprop(name, func(_ *protocol.BindContext, w T, v *protocol.Value, f protocol.FlagState) error {
		b, err := protocol.AsBool(name, v, f)
		if err != nil {
			return err
		}
		set(w, b)
		return nil
	}))
}

// intProp is the common shape: an integer into a setter.
func intProp[T any](name string, set func(w T, n int)) protocol.Property {
	return protocol.NewProperty("int", wprop(name, func(_ *protocol.BindContext, w T, v *protocol.Value, f protocol.FlagState) error {
		n, err := protocol.AsInt(name, v, f)
		if err != nil {
			return err
		}
		set(w, n)
		return nil
	}))
}

// actionProp records the command bound to a control (action=). The
// control's activation wiring - set once in its Bind function -
// consults BindContext.Action/FireAction, so assigning or replacing
// the action never re-wires callbacks.
func actionProp(name string) protocol.Property {
	return protocol.NewProperty("action", wprop(name, func(ctx *protocol.BindContext, w core.Trinket, v *protocol.Value, f protocol.FlagState) error {
		id, err := protocol.AsWord(name, v, f)
		if err != nil {
			return err
		}
		if ctx.Dispatch == nil && ctx.Emit == nil {
			return fmt.Errorf("%s: no command dispatcher on this connection", name)
		}
		ctx.SetAction(trinketID(w), id)
		return nil
	}))
}

// destroyTrinket is the standard destroy: detach the trinket from its
// parent container.
func destroyTrinket(w core.Trinket) error {
	parent := w.Parent()
	if parent == nil {
		return nil // never attached; nothing to detach
	}
	if remover, ok := parent.(interface{ RemoveChild(core.Trinket) }); ok {
		remover.RemoveChild(w)
		return nil
	}
	return fmt.Errorf("destroy: parent %T does not support child removal", parent)
}

// trinketID returns a trinket's stable object identity as a wire ID.
func trinketID(w core.Trinket) uint64 {
	if iw, ok := w.(interface{ ObjectID() core.ObjectID }); ok {
		return uint64(iw.ObjectID())
	}
	return 0
}

// regTrinket registers a trinket type whose targets are core.Trinkets.
// bind, when non-nil, wires the trinket's event emission into the
// connection (called once at construction).
func regTrinket(name string, construct func() core.Trinket, props map[string]protocol.Property, appendFn func(parent, child core.Trinket) error, bind func(ctx *protocol.BindContext, w core.Trinket)) {
	spec := &protocol.TypeSpec{
		New:   func() any { return construct() },
		Props: props,
		ID: func(t any) uint64 {
			if w, ok := t.(interface{ ObjectID() core.ObjectID }); ok {
				return uint64(w.ObjectID())
			}
			return 0
		},
		// Default destroy: detach from the parent container. Types
		// with richer teardown override via their own TypeSpec.
		Destroy: func(t any) error {
			w, ok := t.(core.Trinket)
			if !ok {
				return fmt.Errorf("%s: not a trinket", name)
			}
			return destroyTrinket(w)
		},
	}
	if appendFn != nil {
		spec.Append = func(p, c any) error {
			pw, ok1 := p.(core.Trinket)
			cw, ok2 := c.(core.Trinket)
			if !ok1 || !ok2 {
				return fmt.Errorf("%s: children must be trinkets", name)
			}
			return appendFn(pw, cw)
		}
	}
	if bind != nil {
		spec.Bind = func(ctx *protocol.BindContext, t any) {
			if w, ok := t.(core.Trinket); ok {
				bind(ctx, w)
			}
		}
	}
	protocol.RegisterType(name, spec)
}

func init() {
	// Virtual objects (e.g. combobox items) draw their IDs from the
	// same allocator as real trinkets, so a virtual ID can never
	// collide with a core.ObjectID in a session's reply table.
	protocol.SetVirtualIDSource(func() uint64 { return uint64(core.NextObjectID()) })

	protocol.RegisterCommonProperty("enabled", boolProp("enabled", core.Trinket.SetEnabled).Def("true").Tip("Whether the trinket accepts input."))
	protocol.RegisterCommonProperty("visible", boolProp("visible", core.Trinket.SetVisible).Def("true").Tip("Whether the trinket is shown."))
	protocol.RegisterCommonProperty("name", stringProp("name", core.Trinket.SetName).Tip("Debug/tooling label; not identity."))

	protocol.RegisterCommonProperty("min_width", sizeProp("min_width", true, true).Tip("Minimum width, in units."))
	protocol.RegisterCommonProperty("min_height", sizeProp("min_height", true, false).Tip("Minimum height, in units."))
	protocol.RegisterCommonProperty("max_width", sizeProp("max_width", false, true).Tip("Maximum width, in units."))
	protocol.RegisterCommonProperty("max_height", sizeProp("max_height", false, false).Tip("Maximum height, in units."))

	protocol.RegisterCommonProperty("column_units", unitsProp("column_units", true).Def("inherited").Tip("Units one grid column spans (denomination override)."))
	protocol.RegisterCommonProperty("row_units", unitsProp("row_units", false).Def("inherited").Tip("Units one grid row spans (denomination override)."))

	protocol.RegisterCommonProperty("font", protocol.NewProperty("enum", wprop("font", func(_ *protocol.BindContext, w core.Trinket, v *protocol.Value, f protocol.FlagState) error {
		s, err := protocol.AsString("font", v, f)
		if err != nil {
			return err
		}
		// The built-in pseudo-fonts have dedicated constants; any other name is
		// a family the renderer resolves — the ui-* tree and real families on
		// the graphical side, or the text backend's cipher pseudo-fonts (Black
		// Serif, Fraktur, Double-Struck, …) which stay plain on the graphical
		// side. Empty is rejected (use the default via omission).
		fnt := map[string]*core.Font{
			"ui-text": core.FontUIText12,
			"ui-term": core.FontUITerm12,
			"Monday":  core.FontMonday12,
			"Tuesday": core.FontTuesday12,
		}[s]
		if fnt == nil {
			if strings.TrimSpace(s) == "" {
				return fmt.Errorf("font: empty family")
			}
			fnt = &core.Font{Name: s, Size: core.FontMonday12.Size}
		}
		fw, ok := w.(interface{ SetFont(*core.Font) })
		if !ok {
			return fmt.Errorf("font: not supported by this type")
		}
		fw.SetFont(fnt)
		return nil
	})).OneOf("ui-text", "ui-term", "Monday", "Tuesday",
		"Black Serif", "Double-Struck", "Bold Fraktur", "Bold Italic", "Fraktur",
		"Bold Script", "Black Sans", "Black Italic", "Italic").
		Def("inherited").Tip("Font family for this trinket's text (graphical: a family or the ui-* tree; text backend: Monday, Tuesday, or a cipher style)."))

	protocol.RegisterCommonProperty("acc_name", protocol.NewProperty("string", wprop("acc_name", func(_ *protocol.BindContext, w core.Trinket, v *protocol.Value, f protocol.FlagState) error {
		s, err := protocol.AsString("acc_name", v, f)
		if err != nil {
			return err
		}
		if aw, ok := w.(interface{ SetAccessibleName(string) }); ok {
			aw.SetAccessibleName(s)
			return nil
		}
		return fmt.Errorf("acc_name: not supported by this type")
	})).Tip("Accessibility name announced by screen readers."))

	// Layout hints live on the child (vocabulary decision 2026-07-05):
	// the parent's layout manager consults them at attach time, so in
	// scripts they must precede the trinket's placement in children={}
	// (property application order already guarantees that).
	protocol.RegisterCommonProperty("stretch", protocol.NewProperty("int", wprop("stretch", func(_ *protocol.BindContext, w core.Trinket, v *protocol.Value, f protocol.FlagState) error {
		n, err := protocol.AsInt("stretch", v, f)
		if err != nil {
			return err
		}
		if h, ok := w.(interface{ SetLayoutStretch(int) }); ok {
			h.SetLayoutStretch(n)
			return nil
		}
		return fmt.Errorf("stretch: not supported by this type")
	})).Def("0").Tip("Layout stretch factor relative to siblings."))

	protocol.RegisterCommonProperty("align", protocol.NewProperty("enum", wprop("align", func(_ *protocol.BindContext, w core.Trinket, v *protocol.Value, f protocol.FlagState) error {
		word, err := protocol.AsWord("align", v, f)
		if err != nil {
			return err
		}
		a, ok := map[string]core.Alignment{
			"fill":   core.AlignFill,
			"left":   core.AlignLeft,
			"center": core.AlignCenter,
			"right":  core.AlignRight,
			"top":    core.AlignTop,
			"middle": core.AlignMiddle,
			"bottom": core.AlignBottom,
		}[word]
		if !ok {
			return fmt.Errorf("align: unknown value %q", word)
		}
		if h, ok := w.(interface{ SetLayoutAlignment(core.Alignment) }); ok {
			h.SetLayoutAlignment(a)
			return nil
		}
		return fmt.Errorf("align: not supported by this type")
	})).OneOf("fill", "left", "center", "right", "top", "middle", "bottom").Tip("Layout alignment of this item in its cell."))

	// Colors (vocabulary decision 2026-07-05): named colors as bare
	// words, RGB as quoted "#rrggbb". fg/bg build on the trinket's
	// custom style override.
	protocol.RegisterCommonProperty("fg", colorProp("fg", true).Def("inherited").Tip("Text/foreground color (named or \"#rrggbb\")."))
	protocol.RegisterCommonProperty("bg", colorProp("bg", false).Def("inherited").Tip("Background color (named or \"#rrggbb\")."))
}

// parseColor interprets a wire color value: a bare named-color word
// (color=red, color=bright_blue) or a quoted "#rrggbb" string.
func parseColor(name string, v *protocol.Value, f protocol.FlagState) (style.Color, error) {
	if f != protocol.FlagNone || v == nil {
		return 0, fmt.Errorf("%s: expected a color (word or \"#rrggbb\")", name)
	}
	switch v.Kind {
	case protocol.WordValue:
		c, ok := map[string]style.Color{
			"default":        style.ColorDefault,
			"black":          style.ColorBlack,
			"red":            style.ColorRed,
			"green":          style.ColorGreen,
			"yellow":         style.ColorYellow,
			"blue":           style.ColorBlue,
			"magenta":        style.ColorMagenta,
			"cyan":           style.ColorCyan,
			"white":          style.ColorWhite,
			"bright_black":   style.ColorBrightBlack,
			"bright_red":     style.ColorBrightRed,
			"bright_green":   style.ColorBrightGreen,
			"bright_yellow":  style.ColorBrightYellow,
			"bright_blue":    style.ColorBrightBlue,
			"bright_magenta": style.ColorBrightMagenta,
			"bright_cyan":    style.ColorBrightCyan,
			"bright_white":   style.ColorBrightWhite,
		}[v.Word]
		if !ok {
			return 0, fmt.Errorf("%s: unknown color %q", name, v.Word)
		}
		return c, nil
	case protocol.StringValue:
		s := v.Str
		if len(s) != 7 || s[0] != '#' {
			return 0, fmt.Errorf("%s: RGB colors are \"#rrggbb\"", name)
		}
		var r, g, b int
		if _, err := fmt.Sscanf(s[1:], "%02x%02x%02x", &r, &g, &b); err != nil {
			return 0, fmt.Errorf("%s: malformed RGB color %q", name, s)
		}
		return style.RGB(r, g, b), nil
	default:
		return 0, fmt.Errorf("%s: expected a color (word or \"#rrggbb\")", name)
	}
}

// colorProp applies fg=/bg= through the trinket's custom style.
func colorProp(name string, isFg bool) protocol.Property {
	return protocol.NewProperty("color", wprop(name, func(_ *protocol.BindContext, w core.Trinket, v *protocol.Value, f protocol.FlagState) error {
		c, err := parseColor(name, v, f)
		if err != nil {
			return err
		}
		styler, ok := w.(interface {
			Style() *style.CellStyle
			SetStyle(*style.CellStyle)
		})
		if !ok {
			return fmt.Errorf("%s: not supported by this type", name)
		}
		s := style.DefaultStyle()
		if cur := styler.Style(); cur != nil {
			s = *cur
		}
		if isFg {
			s = s.WithFg(c)
		} else {
			s = s.WithBg(c)
		}
		styler.SetStyle(&s)
		return nil
	}))
}

func sizeProp(name string, min, isWidth bool) protocol.Property {
	return protocol.NewProperty("units", wprop(name, func(_ *protocol.BindContext, w core.Trinket, v *protocol.Value, f protocol.FlagState) error {
		n, err := protocol.AsInt(name, v, f)
		if err != nil {
			return err
		}
		if min {
			s := w.MinimumSize()
			if isWidth {
				s.Width = core.Unit(n)
			} else {
				s.Height = core.Unit(n)
			}
			w.SetMinimumSize(s)
		} else {
			s := w.MaximumSize()
			if isWidth {
				s.Width = core.Unit(n)
			} else {
				s.Height = core.Unit(n)
			}
			w.SetMaximumSize(s)
		}
		return nil
	}))
}

func unitsProp(name string, isColumn bool) protocol.Property {
	return protocol.NewProperty("units", wprop(name, func(_ *protocol.BindContext, w core.Trinket, v *protocol.Value, f protocol.FlagState) error {
		n, err := protocol.AsInt(name, v, f)
		if err != nil {
			return err
		}
		type metriced interface {
			CellMetricsOverride() *core.CellMetrics
			EffectiveCellMetrics() core.CellMetrics
			SetCellMetrics(*core.CellMetrics)
		}
		mw, ok := w.(metriced)
		if !ok {
			return fmt.Errorf("%s: not supported by this type", name)
		}
		m := mw.EffectiveCellMetrics()
		if ov := mw.CellMetricsOverride(); ov != nil {
			m = *ov
		}
		if isColumn {
			m.CellWidth = core.Unit(n)
		} else {
			m.CellHeight = core.Unit(n)
		}
		mw.SetCellMetrics(&m)
		return nil
	}))
}
