package trinkets

import (
	"fmt"

	"github.com/phroun/kittytk/protocol"
	"github.com/phroun/kittytk/style"
)

// Wire registration for status bar content: a statusbar collects
// sections; a section collects styled text spans (our first inline
// styling on the wire - span fg=/bg= use the color vocabulary):
//
//	sb=new statusbar children={
//	    new section children={
//	        new span text="Ready - Press "
//	        new span text="F10" fg=red bg=white
//	        new span text=" for menu"
//	    }
//	}
//
// The application installs the result with SetStatusBarContent.

// wireStatusBar is the virtual statusbar target.
type wireStatusBar struct {
	sections []StatusSection
}

// Sections returns the collected sections (for SetStatusBarContent).
func (b *wireStatusBar) Sections() []StatusSection { return b.sections }

// wireSection accumulates one section.
type wireSection struct {
	section StatusSection
}

// wireSpan accumulates one styled span.
type wireSpan struct {
	span StatusTextSpan
}

func init() {
	protocol.RegisterType("statusbar", &protocol.TypeSpec{
		Virtual: true,
		New:     func() any { return &wireStatusBar{} },
		Append: func(parent, child any) error {
			b := parent.(*wireStatusBar)
			s, ok := child.(*wireSection)
			if !ok {
				return fmt.Errorf("statusbar: children must be sections, got %T", child)
			}
			b.sections = append(b.sections, s.section)
			return nil
		},
	})

	protocol.RegisterType("section", &protocol.TypeSpec{
		Virtual: true,
		New:     func() any { return &wireSection{} },
		Props: map[string]protocol.Property{
			"text": protocol.NewProperty("string", wprop("text", func(_ *protocol.BindContext, s *wireSection, v *protocol.Value, f protocol.FlagState) error {
				str, err := protocol.AsString("text", v, f)
				if err != nil {
					return err
				}
				s.section.Text = str
				return nil
			})).Tip("Section text (spans take precedence)"),
			"width": protocol.NewProperty("int", wprop("width", func(_ *protocol.BindContext, s *wireSection, v *protocol.Value, f protocol.FlagState) error {
				n, err := protocol.AsInt("width", v, f)
				if err != nil {
					return err
				}
				s.section.Width = n
				return nil
			})).Tip("Section width in units"),
			"stretch": protocol.NewProperty("flag", wprop("stretch", func(_ *protocol.BindContext, s *wireSection, v *protocol.Value, f protocol.FlagState) error {
				b, err := protocol.AsBool("stretch", v, f)
				if err != nil {
					return err
				}
				if b {
					s.section.Width = -1
				}
				return nil
			})).Tip("Section grows to fill space"),
			"align": protocol.NewProperty("enum", wprop("align", func(_ *protocol.BindContext, s *wireSection, v *protocol.Value, f protocol.FlagState) error {
				w, err := protocol.AsWord("align", v, f)
				if err != nil {
					return err
				}
				n, ok := map[string]int{"left": 0, "center": 1, "right": 2}[w]
				if !ok {
					return fmt.Errorf("align: unknown value %q", w)
				}
				s.section.Alignment = n
				return nil
			})).OneOf("left", "center", "right").Tip("Text alignment within section"),
		},
		Append: func(parent, child any) error {
			s := parent.(*wireSection)
			sp, ok := child.(*wireSpan)
			if !ok {
				return fmt.Errorf("section: children must be spans, got %T", child)
			}
			s.section.Spans = append(s.section.Spans, sp.span)
			return nil
		},
	})

	protocol.RegisterType("span", &protocol.TypeSpec{
		Virtual: true,
		New:     func() any { return &wireSpan{} },
		Props: map[string]protocol.Property{
			"text": protocol.NewProperty("string", wprop("text", func(_ *protocol.BindContext, s *wireSpan, v *protocol.Value, f protocol.FlagState) error {
				str, err := protocol.AsString("text", v, f)
				if err != nil {
					return err
				}
				s.span.Text = str
				return nil
			})).Tip("Span text"),
			"fg": protocol.NewProperty("color", spanColor("fg", true)).Tip("Span foreground color"),
			"bg": protocol.NewProperty("color", spanColor("bg", false)).Tip("Span background color"),
		},
	})
}

// spanColor applies a color to a span's style, creating it on first
// use from the default style.
func spanColor(name string, isFg bool) protocol.PropertyApplier {
	return wprop(name, func(_ *protocol.BindContext, s *wireSpan, v *protocol.Value, f protocol.FlagState) error {
		c, err := parseColor(name, v, f)
		if err != nil {
			return err
		}
		if s.span.Style == nil {
			st := style.DefaultStyle()
			s.span.Style = &st
		}
		if isFg {
			*s.span.Style = s.span.Style.WithFg(c)
		} else {
			*s.span.Style = s.span.Style.WithBg(c)
		}
		return nil
	})
}
