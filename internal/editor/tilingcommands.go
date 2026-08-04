package editor

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/phroun/ifitfits"
	"github.com/phroun/pawscript"
)

// This file wires the ifitfits viewport-tiling engine's command surface into
// PawScript. Each command parses its arguments, calls the matching ifitfits
// method on the editor's tiler, and reports a Usage warning (like the other
// registered commands) when a REQUIRED argument is missing. Commands that
// return a value — a tile handle, a ref, a list — publish it via ctx.SetResult.
//
// The names mirror ifitfits/COMMANDS.md (all viewport_*), except viewport_next
// and viewport_prior, which mew already uses for main-viewport focus cycling;
// the tiler's reading-order cycle is exposed here as tile_next / tile_prior.
//
// This is deliberately mechanical: the commands fire the library methods and
// surface their results. Nothing here decides which tile a bare command should
// act on — the tile handle is always an explicit argument, as the library
// requires.

// Intrinsic minimums stamped on every tile we create — in the initialization
// code below and on any tile returned by viewport_new / viewport_split — until
// the host derives real per-content metrics. Without this a freshly-created
// tile inherits the library's larger default minimum and can be omitted on a
// modest workspace. Natural sizes are left at the library default (0 = unchanged
// in SetMetrics), so equal tiles still split evenly.
const (
	newTileMinW = 10
	newTileMinH = 2
)

// ensureTiler builds the tiler on first use (root tile "main" split right into
// an empty "blank") and returns it. The render loop keeps the workspace size
// current; a command that runs before the first render still gets a valid tiler
// sized from the current terminal (or a sane default), corrected on the next
// frame.
func (e *Editor) ensureTiler() *ifitfits.Viewport {
	if e.tiler != nil {
		return e.tiler
	}
	w, h := float64(e.Renderer.Width), float64(e.Renderer.Height)
	if w <= 0 {
		w = 80
	}
	if h <= 0 {
		h = 24
	}
	vp, main := ifitfits.NewViewport(w, h)
	vp.Set(main, "main")
	vp.SetMetrics(main, newTileMinW, newTileMinH, 0, 0)
	blank := vp.Split(main, ifitfits.Right)
	vp.Set(blank, "blank")
	vp.SetMetrics(blank, newTileMinW, newTileMinH, 0, 0)
	e.tiler = vp
	e.tilerMain = main
	return e.tiler
}

// ---- argument parsing ----

func tileArg(ctx *pawscript.Context, i int) (interface{}, bool) {
	if i < 0 || i >= len(ctx.Args) || ctx.Args[i] == nil {
		return nil, false
	}
	return ctx.Args[i], true
}

func tileArgStr(ctx *pawscript.Context, i int) (string, bool) {
	v, ok := tileArg(ctx, i)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("%v", v), true
}

func tileArgFloat(ctx *pawscript.Context, i int) (float64, bool) {
	v, ok := tileArg(ctx, i)
	if !ok {
		return 0, false
	}
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	case uint64:
		return float64(t), true
	default:
		f, err := strconv.ParseFloat(strings.TrimSpace(fmt.Sprintf("%v", v)), 64)
		return f, err == nil
	}
}

// tileArgHandle reads a tile handle. Handles are opaque ids the library assigns;
// they arrive as numbers (or numeric strings) from a script.
func tileArgHandle(ctx *pawscript.Context, i int) (ifitfits.Handle, bool) {
	f, ok := tileArgFloat(ctx, i)
	if !ok || f <= 0 {
		return 0, false
	}
	return ifitfits.Handle(uint64(f)), true
}

func tileArgDir(ctx *pawscript.Context, i int) (ifitfits.Direction, bool) {
	s, ok := tileArgStr(ctx, i)
	if !ok {
		return 0, false
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "up":
		return ifitfits.Up, true
	case "down":
		return ifitfits.Down, true
	case "left":
		return ifitfits.Left, true
	case "right":
		return ifitfits.Right, true
	case "before":
		return ifitfits.Before, true
	case "after":
		return ifitfits.After, true
	case "prior":
		return ifitfits.Prior, true
	case "next":
		return ifitfits.Next, true
	default:
		return 0, false
	}
}

// tileArgState parses the tri-state (true, false, toggle) shared by the
// toggling commands. A missing or unrecognized value defaults to Toggle, per
// the library's convention.
func tileArgState(ctx *pawscript.Context, i int) ifitfits.State {
	v, ok := tileArg(ctx, i)
	if !ok {
		return ifitfits.Toggle
	}
	if b, isBool := v.(bool); isBool {
		if b {
			return ifitfits.On
		}
		return ifitfits.Off
	}
	switch strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", v))) {
	case "true", "on", "1", "yes":
		return ifitfits.On
	case "false", "off", "0", "no":
		return ifitfits.Off
	default:
		return ifitfits.Toggle
	}
}

// tileArgBool parses an optional boolean flag (recursive, includeHidden),
// defaulting to def when absent.
func tileArgBool(ctx *pawscript.Context, i int, def bool) bool {
	v, ok := tileArg(ctx, i)
	if !ok {
		return def
	}
	if b, isBool := v.(bool); isBool {
		return b
	}
	switch strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", v))) {
	case "true", "on", "1", "yes":
		return true
	case "false", "off", "0", "no":
		return false
	default:
		return def
	}
}

// ---- registration ----

func (e *Editor) registerTilingCommands(ps *pawscript.PawScript) {
	// tileOnly: a command whose only required argument is the tile handle.
	tileOnly := func(name, usage string, fn func(*ifitfits.Viewport, ifitfits.Handle)) {
		ps.RegisterCommand(name, func(ctx *pawscript.Context) pawscript.Result {
			t, ok := tileArgHandle(ctx, 0)
			if !ok {
				e.ShowWarning("Usage: " + usage)
				return pawscript.BoolStatus(false)
			}
			fn(e.ensureTiler(), t)
			return pawscript.BoolStatus(true)
		})
	}
	// tileRet: tile-only, but the method returns a destination/next handle.
	tileRet := func(name, usage string, fn func(*ifitfits.Viewport, ifitfits.Handle) ifitfits.Handle) {
		ps.RegisterCommand(name, func(ctx *pawscript.Context) pawscript.Result {
			t, ok := tileArgHandle(ctx, 0)
			if !ok {
				e.ShowWarning("Usage: " + usage)
				return pawscript.BoolStatus(false)
			}
			ctx.SetResult(uint64(fn(e.ensureTiler(), t)))
			return pawscript.BoolStatus(true)
		})
	}
	// tileDir: tile + a required direction.
	tileDir := func(name, usage string, fn func(*ifitfits.Viewport, ifitfits.Handle, ifitfits.Direction)) {
		ps.RegisterCommand(name, func(ctx *pawscript.Context) pawscript.Result {
			t, okT := tileArgHandle(ctx, 0)
			d, okD := tileArgDir(ctx, 1)
			if !okT || !okD {
				e.ShowWarning("Usage: " + usage)
				return pawscript.BoolStatus(false)
			}
			fn(e.ensureTiler(), t, d)
			return pawscript.BoolStatus(true)
		})
	}
	// tileState: tile + an optional (true, false, toggle) state.
	tileState := func(name, usage string, fn func(*ifitfits.Viewport, ifitfits.Handle, ifitfits.State)) {
		ps.RegisterCommand(name, func(ctx *pawscript.Context) pawscript.Result {
			t, ok := tileArgHandle(ctx, 0)
			if !ok {
				e.ShowWarning("Usage: " + usage)
				return pawscript.BoolStatus(false)
			}
			fn(e.ensureTiler(), t, tileArgState(ctx, 1))
			return pawscript.BoolStatus(true)
		})
	}

	// --- Structure ---
	// viewport_new / viewport_split: tile + direction, with an optional ref that
	// otherwise clones the origin tile's ref. Both return the new tile.
	newSplit := func(name, usage string, fn func(*ifitfits.Viewport, ifitfits.Handle, ifitfits.Direction, ...string) ifitfits.Handle) {
		ps.RegisterCommand(name, func(ctx *pawscript.Context) pawscript.Result {
			t, okT := tileArgHandle(ctx, 0)
			d, okD := tileArgDir(ctx, 1)
			if !okT || !okD {
				e.ShowWarning("Usage: " + usage)
				return pawscript.BoolStatus(false)
			}
			vp := e.ensureTiler()
			var h ifitfits.Handle
			if ref, ok := tileArgStr(ctx, 2); ok {
				h = fn(vp, t, d, ref)
			} else {
				h = fn(vp, t, d)
			}
			// Stamp our default minimums on the new tile so it does not inherit
			// the library's larger default and get omitted on a modest workspace.
			if h != 0 {
				vp.SetMetrics(h, newTileMinW, newTileMinH, 0, 0)
			}
			ctx.SetResult(uint64(h))
			return pawscript.BoolStatus(true)
		})
	}
	newSplit("viewport_new", "viewport_new <tile>, <direction>, [ref]", (*ifitfits.Viewport).New)
	newSplit("viewport_split", "viewport_split <tile>, <direction>, [ref]", (*ifitfits.Viewport).Split)
	tileRet("viewport_close", "viewport_close <tile>", (*ifitfits.Viewport).Close)

	// --- Navigation ---
	// viewport_go returns the resolved destination tile.
	ps.RegisterCommand("viewport_go", func(ctx *pawscript.Context) pawscript.Result {
		t, okT := tileArgHandle(ctx, 0)
		d, okD := tileArgDir(ctx, 1)
		if !okT || !okD {
			e.ShowWarning("Usage: viewport_go <tile>, <direction>")
			return pawscript.BoolStatus(false)
		}
		ctx.SetResult(uint64(e.ensureTiler().Go(t, d)))
		return pawscript.BoolStatus(true)
	})
	tileRet("viewport_up", "viewport_up <tile>", (*ifitfits.Viewport).Up)
	tileRet("viewport_down", "viewport_down <tile>", (*ifitfits.Viewport).Down)
	tileRet("viewport_left", "viewport_left <tile>", (*ifitfits.Viewport).Left)
	tileRet("viewport_right", "viewport_right <tile>", (*ifitfits.Viewport).Right)
	// viewport_prior / viewport_next are already mew commands (main-viewport
	// focus cycling); the tiler's reading-order cycle is tile_prior / tile_next.
	tileRet("tile_prior", "tile_prior <tile>", (*ifitfits.Viewport).Prior)
	tileRet("tile_next", "tile_next <tile>", (*ifitfits.Viewport).Next)

	// --- Move & reorder ---
	tileDir("viewport_swap", "viewport_swap <tile>, <direction>", (*ifitfits.Viewport).Swap)
	tileOnly("viewport_swap_up", "viewport_swap_up <tile>", (*ifitfits.Viewport).SwapUp)
	tileOnly("viewport_swap_down", "viewport_swap_down <tile>", (*ifitfits.Viewport).SwapDown)
	tileOnly("viewport_swap_left", "viewport_swap_left <tile>", (*ifitfits.Viewport).SwapLeft)
	tileOnly("viewport_swap_right", "viewport_swap_right <tile>", (*ifitfits.Viewport).SwapRight)
	tileDir("viewport_merge", "viewport_merge <tile>, <direction>", (*ifitfits.Viewport).Merge)
	tileOnly("viewport_merge_up", "viewport_merge_up <tile>", (*ifitfits.Viewport).MergeUp)
	tileOnly("viewport_merge_down", "viewport_merge_down <tile>", (*ifitfits.Viewport).MergeDown)
	tileOnly("viewport_merge_left", "viewport_merge_left <tile>", (*ifitfits.Viewport).MergeLeft)
	tileOnly("viewport_merge_right", "viewport_merge_right <tile>", (*ifitfits.Viewport).MergeRight)

	// --- Orientation ---
	tileOnly("viewport_flip", "viewport_flip <tile>", (*ifitfits.Viewport).Flip)
	tileOnly("viewport_flip_parent", "viewport_flip_parent <tile>", (*ifitfits.Viewport).FlipParent)
	tileOnly("viewport_reverse", "viewport_reverse <tile>", (*ifitfits.Viewport).Reverse)
	tileOnly("viewport_reverse_parent", "viewport_reverse_parent <tile>", (*ifitfits.Viewport).ReverseParent)

	// --- Stacks & tabs ---
	tileState("viewport_stack", "viewport_stack <tile>, [true|false|toggle]", (*ifitfits.Viewport).Stack)
	tileRet("viewport_tab_next", "viewport_tab_next <tile>", (*ifitfits.Viewport).TabNext)
	tileRet("viewport_tab_prior", "viewport_tab_prior <tile>", (*ifitfits.Viewport).TabPrior)
	tileOnly("viewport_move_tab_next", "viewport_move_tab_next <tile>", (*ifitfits.Viewport).MoveTabNext)
	tileOnly("viewport_move_tab_prior", "viewport_move_tab_prior <tile>", (*ifitfits.Viewport).MoveTabPrior)

	// --- Sizing ---
	tileState("viewport_zoom", "viewport_zoom <tile>, [true|false|toggle]", (*ifitfits.Viewport).Zoom)
	tileState("viewport_shrink", "viewport_shrink <tile>, [true|false|toggle]", (*ifitfits.Viewport).Shrink)
	tileOnly("viewport_normal", "viewport_normal <tile>", (*ifitfits.Viewport).Normal)
	tileOnly("viewport_mode_next", "viewport_mode_next <tile>", (*ifitfits.Viewport).ModeNext)
	tileOnly("viewport_mode_prior", "viewport_mode_prior <tile>", (*ifitfits.Viewport).ModePrior)
	// viewport_expand / viewport_contract: tile + optional delta (default 1).
	delta := func(name, usage string, fn func(*ifitfits.Viewport, ifitfits.Handle, float64)) {
		ps.RegisterCommand(name, func(ctx *pawscript.Context) pawscript.Result {
			t, ok := tileArgHandle(ctx, 0)
			if !ok {
				e.ShowWarning("Usage: " + usage)
				return pawscript.BoolStatus(false)
			}
			d, hasD := tileArgFloat(ctx, 1)
			if !hasD {
				d = 1
			}
			fn(e.ensureTiler(), t, d)
			return pawscript.BoolStatus(true)
		})
	}
	delta("viewport_expand", "viewport_expand <tile>, [delta]", (*ifitfits.Viewport).Expand)
	delta("viewport_contract", "viewport_contract <tile>, [delta]", (*ifitfits.Viewport).Contract)
	// viewport_resize: tile + optional size (<= 0 unpins).
	ps.RegisterCommand("viewport_resize", func(ctx *pawscript.Context) pawscript.Result {
		t, ok := tileArgHandle(ctx, 0)
		if !ok {
			e.ShowWarning("Usage: viewport_resize <tile>, [size]")
			return pawscript.BoolStatus(false)
		}
		size, _ := tileArgFloat(ctx, 1) // absent -> 0 -> unpin
		e.ensureTiler().Resize(t, size)
		return pawscript.BoolStatus(true)
	})
	// viewport_equalize / viewport_balance: tile + optional recursive flag.
	recursive := func(name, usage string, fn func(*ifitfits.Viewport, ifitfits.Handle, bool)) {
		ps.RegisterCommand(name, func(ctx *pawscript.Context) pawscript.Result {
			t, ok := tileArgHandle(ctx, 0)
			if !ok {
				e.ShowWarning("Usage: " + usage)
				return pawscript.BoolStatus(false)
			}
			fn(e.ensureTiler(), t, tileArgBool(ctx, 1, false))
			return pawscript.BoolStatus(true)
		})
	}
	recursive("viewport_equalize", "viewport_equalize <tile>, [recursive]", (*ifitfits.Viewport).Equalize)
	recursive("viewport_balance", "viewport_balance <tile>, [recursive]", (*ifitfits.Viewport).Balance)

	// --- Lenses ---
	tileState("viewport_monocle", "viewport_monocle <tile>, [true|false|toggle]", (*ifitfits.Viewport).Monocle)
	tileState("viewport_local_monocle", "viewport_local_monocle <tile>, [true|false|toggle]", (*ifitfits.Viewport).LocalMonocle)
	tileState("viewport_spectacle", "viewport_spectacle <tile>, [true|false|toggle]", (*ifitfits.Viewport).Spectacle)
	tileState("viewport_local_spectacle", "viewport_local_spectacle <tile>, [true|false|toggle]", (*ifitfits.Viewport).LocalSpectacle)

	// --- Focus, caret & queries ---
	tileOnly("viewport_set_focus", "viewport_set_focus <tile>", (*ifitfits.Viewport).SetFocus)
	ps.RegisterCommand("viewport_get_focus", func(ctx *pawscript.Context) pawscript.Result {
		ctx.SetResult(uint64(e.ensureTiler().GetFocus()))
		return pawscript.BoolStatus(true)
	})
	ps.RegisterCommand("viewport_set_caret", func(ctx *pawscript.Context) pawscript.Result {
		t, okT := tileArgHandle(ctx, 0)
		x, okX := tileArgFloat(ctx, 1)
		y, okY := tileArgFloat(ctx, 2)
		if !okT || !okX || !okY {
			e.ShowWarning("Usage: viewport_set_caret <tile>, <x>, <y>")
			return pawscript.BoolStatus(false)
		}
		e.ensureTiler().SetCaret(t, x, y)
		return pawscript.BoolStatus(true)
	})
	ps.RegisterCommand("viewport_get_tile", func(ctx *pawscript.Context) pawscript.Result {
		x, okX := tileArgFloat(ctx, 0)
		y, okY := tileArgFloat(ctx, 1)
		if !okX || !okY {
			e.ShowWarning("Usage: viewport_get_tile <x>, <y>")
			return pawscript.BoolStatus(false)
		}
		ctx.SetResult(uint64(e.ensureTiler().GetTile(x, y)))
		return pawscript.BoolStatus(true)
	})

	// --- Refs ---
	ps.RegisterCommand("viewport_content", func(ctx *pawscript.Context) pawscript.Result {
		t, ok := tileArgHandle(ctx, 0)
		if !ok {
			e.ShowWarning("Usage: viewport_content <tile>")
			return pawscript.BoolStatus(false)
		}
		ctx.SetResult(e.ensureTiler().Content(t))
		return pawscript.BoolStatus(true)
	})
	ps.RegisterCommand("viewport_get", func(ctx *pawscript.Context) pawscript.Result {
		ref, ok := tileArgStr(ctx, 0)
		if !ok {
			e.ShowWarning("Usage: viewport_get <ref>, [includeHidden]")
			return pawscript.BoolStatus(false)
		}
		handles := e.ensureTiler().Get(ref, tileArgBool(ctx, 1, false))
		list := make([]interface{}, len(handles))
		for i, h := range handles {
			list[i] = uint64(h)
		}
		ctx.SetResult(list)
		return pawscript.BoolStatus(true)
	})
	ps.RegisterCommand("viewport_set", func(ctx *pawscript.Context) pawscript.Result {
		t, okT := tileArgHandle(ctx, 0)
		ref, okR := tileArgStr(ctx, 1)
		if !okT || !okR {
			e.ShowWarning("Usage: viewport_set <tile>, <newRef>")
			return pawscript.BoolStatus(false)
		}
		e.ensureTiler().Set(t, ref)
		return pawscript.BoolStatus(true)
	})
}
