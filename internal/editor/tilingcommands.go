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
// The tile a command acts on follows the #-handle idiom (see resolveTileArg /
// tileFront): pass it as a leading #-symbol (e.g. #tile, or any #-variable a
// script captured a handle into with `#mine: {viewport_split …}`), or omit it to
// act on the well-known #tile default (seeded to the main window's tile). The
// library itself requires an explicit tile on every call; mew supplies the
// default so bare commands and keybindings act on the obvious window.
//
// The names mirror ifitfits/COMMANDS.md (all viewport_*), except viewport_next
// and viewport_prior, which mew already uses for main-viewport focus cycling;
// the tiler's reading-order cycle is exposed here as tile_next / tile_prior.

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

// tileDefaultVar is the well-known hash-prefixed variable holding the default
// tile handle. A tiling command given no explicit #handle acts on this tile.
// Seeded once to the main window's tile by seedTileDefault (unless a script has
// already assigned it).
const tileDefaultVar = "#tile"

// ensureTiler builds the tiler on first use and returns it. It starts as a
// single empty tile (ifitfits gives one tile from NewViewport; it holds no ref,
// so it renders blank) with the tiler's focus on it, so there is always a sane
// "active tile" before mew focuses anything. mew's own initial viewport becomes
// this first tile when it is focused (see tilerFollowFocus); further viewports
// split off new tiles. The render loop keeps the workspace size current.
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
	vp, first := ifitfits.NewViewport(w, h)
	vp.SetMetrics(first, newTileMinW, newTileMinH, 0, 0)
	vp.SetFocus(first) // a sane active tile before anything is focused
	e.tiler = vp
	return e.tiler
}

// tilerFollowFocus is the noteMainFocus hook: when mew focuses main-area viewport
// `id`, make the tiler reflect it. If a tile already holds id, activate it
// (revealing it first when it is hidden behind a tab). Otherwise the viewport has
// no tile yet: fill an empty tile if one is free (the initial tile, on startup),
// else split a new tile off to the right of the active tile and put id there.
// Purely tiler-side — it runs under the manager lock and must not touch the
// Manager.
func (e *Editor) tilerFollowFocus(id string) {
	if id == "" {
		return
	}
	vp := e.ensureTiler()
	if tiles := vp.Get(id, false); len(tiles) > 0 {
		vp.SetFocus(tiles[0])
		return
	}
	if tiles := vp.Get(id, true); len(tiles) > 0 {
		vp.Reveal(tiles[0])
		vp.SetFocus(tiles[0])
		return
	}
	if empty := e.firstEmptyTile(); empty != 0 {
		vp.Set(empty, id)
		vp.SetFocus(empty)
		return
	}
	cur := vp.GetFocus()
	if cur == 0 {
		if ts := vp.Tiles(); len(ts) > 0 {
			cur = ts[0].Tile
		}
	}
	if cur == 0 {
		return
	}
	if nt := vp.New(cur, ifitfits.Right, id); nt != 0 {
		vp.SetMetrics(nt, newTileMinW, newTileMinH, 0, 0)
		vp.SetFocus(nt)
	}
}

// dismissTileFor closes any tiler tile whose ref is viewportID, so closing a mew
// viewport (viewport_close) also removes the tile that held it. If the closed tile
// was the active one, the follow-up focus change repopulates through
// tilerFollowFocus.
func (e *Editor) dismissTileFor(viewportID string) {
	if e.tiler == nil || viewportID == "" {
		return
	}
	for _, h := range e.tiler.Get(viewportID, true) {
		e.tiler.Close(h)
	}
}

// focusTilerTile moves the tiler's focus to the given tile handle (no-op for a
// zero handle, no tiler, or a handle already focused). Used when a press lands in
// a tile, so that specific pane becomes the viewport's canonical tile.
func (e *Editor) focusTilerTile(handle uint64) {
	if e.tiler == nil || handle == 0 {
		return
	}
	h := ifitfits.Handle(handle)
	if e.tiler.GetFocus() != h {
		e.tiler.SetFocus(h)
	}
}

// firstEmptyTile returns a visible tile that holds no content (ref ""), or 0.
func (e *Editor) firstEmptyTile() ifitfits.Handle {
	if e.tiler == nil {
		return 0
	}
	for _, b := range e.tiler.Tiles() {
		if b.Ref == "" {
			return b.Tile
		}
	}
	return 0
}

// seedTileDefault keeps the well-known #tile pointed at the tiler's active tile,
// so an explicit `#tile` argument resolves to whatever currently has focus. Uses
// Context.SetModuleObject — the module-object layer, which persists across
// top-level commands. Requires a command context; the render loop cannot seed it.
func (e *Editor) seedTileDefault(ctx *pawscript.Context) {
	if f := e.ensureTiler().GetFocus(); f != 0 {
		ctx.SetModuleObject(tileDefaultVar, uint64(f))
	}
}

// tileHashToHandle converts a value resolved from a #-variable into a tile
// handle. Handles arrive as numbers (or numeric strings) — a script assigns one
// with `#tile: {viewport_split …}` capturing a command's returned handle, or the
// host seeds #tile directly.
func tileHashToHandle(v interface{}) (ifitfits.Handle, bool) {
	switch t := v.(type) {
	case nil:
		return 0, false
	case ifitfits.Handle:
		return t, t != 0
	case uint64:
		return ifitfits.Handle(t), t != 0
	case int:
		return ifitfits.Handle(t), t > 0
	case int64:
		return ifitfits.Handle(t), t > 0
	case float64:
		return ifitfits.Handle(uint64(t)), t > 0
	default:
		f, err := strconv.ParseUint(strings.TrimSpace(fmt.Sprintf("%v", v)), 10, 64)
		if err != nil {
			return 0, false
		}
		return ifitfits.Handle(f), f != 0
	}
}

// resolveTileArg implements the #-handle idiom for the tile argument, following
// the os.argc pattern. If the first positional argument is a bare #-prefixed
// symbol (e.g. #tile, or any #-variable a script assigned a handle into), it
// NAMES the tile handle — resolved through the module-object chain — and the
// remaining positional arguments begin at index 1. Otherwise no handle was
// given: the well-known #tile default is used and the remaining args begin at
// index 0. Returns the handle, the index where the rest of the args start, and
// whether a handle was resolved.
func (e *Editor) resolveTileArg(ctx *pawscript.Context) (ifitfits.Handle, int, bool) {
	if len(ctx.Args) > 0 {
		if sym, ok := ctx.Args[0].(pawscript.Symbol); ok && strings.HasPrefix(string(sym), "#") {
			h, ok := tileHashToHandle(ctx.ResolveHashArg(string(sym)))
			return h, 1, ok
		}
	}
	// No explicit #handle: default to the tiler's active tile.
	f := e.ensureTiler().GetFocus()
	return f, 0, f != 0
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

// tileFront is the shared preamble for every tile-taking command: it ensures the
// tiler exists, seeds the #tile default, and resolves the tile argument via the
// #-handle idiom. It returns the tiler, the resolved handle, the index where the
// remaining positional arguments begin, and whether a handle was resolved (false
// only when an explicit #-symbol names an unset variable).
func (e *Editor) tileFront(ctx *pawscript.Context) (*ifitfits.Viewport, ifitfits.Handle, int, bool) {
	vp := e.ensureTiler()
	e.seedTileDefault(ctx)
	h, rest, ok := e.resolveTileArg(ctx)
	return vp, h, rest, ok
}

// ---- registration ----

func (e *Editor) registerTilingCommands(ps *pawscript.PawScript) {
	// Every command below takes its tile through the #-handle idiom (tileFront):
	// a leading #-symbol names the tile, or it is omitted to act on the
	// well-known #tile default. The remaining positional arguments begin at the
	// returned `rest` index. !ok means an explicit #-symbol named an unset
	// variable — the only tile-resolution failure.

	// tileOnly: no arguments beyond the (defaulting) tile.
	tileOnly := func(name, usage string, fn func(*ifitfits.Viewport, ifitfits.Handle)) {
		ps.RegisterCommand(name, func(ctx *pawscript.Context) pawscript.Result {
			vp, t, _, ok := e.tileFront(ctx)
			if !ok {
				e.ShowWarning("Usage: " + usage)
				return pawscript.BoolStatus(false)
			}
			fn(vp, t)
			return pawscript.BoolStatus(true)
		})
	}
	// tileRet: tile-only, but the method returns a destination/next handle.
	tileRet := func(name, usage string, fn func(*ifitfits.Viewport, ifitfits.Handle) ifitfits.Handle) {
		ps.RegisterCommand(name, func(ctx *pawscript.Context) pawscript.Result {
			vp, t, _, ok := e.tileFront(ctx)
			if !ok {
				e.ShowWarning("Usage: " + usage)
				return pawscript.BoolStatus(false)
			}
			ctx.SetResult(uint64(fn(vp, t)))
			return pawscript.BoolStatus(true)
		})
	}
	// tileDir: tile + a required direction.
	tileDir := func(name, usage string, fn func(*ifitfits.Viewport, ifitfits.Handle, ifitfits.Direction)) {
		ps.RegisterCommand(name, func(ctx *pawscript.Context) pawscript.Result {
			vp, t, rest, ok := e.tileFront(ctx)
			d, okD := tileArgDir(ctx, rest)
			if !ok || !okD {
				e.ShowWarning("Usage: " + usage)
				return pawscript.BoolStatus(false)
			}
			fn(vp, t, d)
			return pawscript.BoolStatus(true)
		})
	}
	// tileState: tile + an optional (true, false, toggle) state.
	tileState := func(name, usage string, fn func(*ifitfits.Viewport, ifitfits.Handle, ifitfits.State)) {
		ps.RegisterCommand(name, func(ctx *pawscript.Context) pawscript.Result {
			vp, t, rest, ok := e.tileFront(ctx)
			if !ok {
				e.ShowWarning("Usage: " + usage)
				return pawscript.BoolStatus(false)
			}
			fn(vp, t, tileArgState(ctx, rest))
			return pawscript.BoolStatus(true)
		})
	}

	// --- Structure ---
	// viewport_new / viewport_split: tile (idiom) + a required direction, with an
	// optional ref that otherwise clones the origin tile's ref. Both return the
	// new tile, stamped with our default minimums so it is not omitted on a
	// modest workspace.
	newSplit := func(name, usage string, fn func(*ifitfits.Viewport, ifitfits.Handle, ifitfits.Direction, ...string) ifitfits.Handle) {
		ps.RegisterCommand(name, func(ctx *pawscript.Context) pawscript.Result {
			vp, t, rest, ok := e.tileFront(ctx)
			d, okD := tileArgDir(ctx, rest)
			if !ok || !okD {
				e.ShowWarning("Usage: " + usage)
				return pawscript.BoolStatus(false)
			}
			var h ifitfits.Handle
			if ref, okR := tileArgStr(ctx, rest+1); okR {
				h = fn(vp, t, d, ref)
			} else {
				h = fn(vp, t, d)
			}
			if h != 0 {
				vp.SetMetrics(h, newTileMinW, newTileMinH, 0, 0)
			}
			ctx.SetResult(uint64(h))
			return pawscript.BoolStatus(true)
		})
	}
	newSplit("viewport_new", "viewport_new [#tile], <direction>, [ref]", (*ifitfits.Viewport).New)
	newSplit("viewport_split", "viewport_split [#tile], <direction>, [ref]", (*ifitfits.Viewport).Split)
	tileRet("viewport_close", "viewport_close [#tile]", (*ifitfits.Viewport).Close)

	// --- Navigation ---
	// viewport_seek is the raw ifitfits navigation: it moves the caret goal and
	// returns the resolved destination tile, with no mew-side side effects.
	ps.RegisterCommand("viewport_seek", func(ctx *pawscript.Context) pawscript.Result {
		vp, t, rest, ok := e.tileFront(ctx)
		d, okD := tileArgDir(ctx, rest)
		if !ok || !okD {
			e.ShowWarning("Usage: viewport_seek [#tile], <direction>")
			return pawscript.BoolStatus(false)
		}
		ctx.SetResult(uint64(vp.Go(t, d)))
		return pawscript.BoolStatus(true)
	})
	// viewport_go is mew's navigation wrapper around viewport_seek: it seeks,
	// makes the destination the new #tile default, and switches mew's view to the
	// destination tile's content — the tile's ref is a mew viewport id, so
	// focusing it drives lastMainViewport and the modebar exactly as an ordinary
	// focus change would. When the destination tile carries a ref that is not a
	// live viewport (e.g. an as-yet-unpopulated tile), the view is left as-is; the
	// #tile default still follows the move.
	ps.RegisterCommand("viewport_go", func(ctx *pawscript.Context) pawscript.Result {
		vp, t, rest, ok := e.tileFront(ctx)
		d, okD := tileArgDir(ctx, rest)
		if !ok || !okD {
			e.ShowWarning("Usage: viewport_go [#tile], <direction>")
			return pawscript.BoolStatus(false)
		}
		dest := vp.Go(t, d)
		vp.SetFocus(dest)                                 // the destination is now the active tile
		ctx.SetModuleObject(tileDefaultVar, uint64(dest)) // #tile follows the move
		if ref := vp.Content(dest); ref != "" {
			if e.ViewportManager.GetViewport(ref) != nil {
				e.ViewportManager.SetFocus(ref) // sets lastMainViewport, as a normal switch
				e.announceFocusedViewport()
			}
		}
		ctx.SetResult(uint64(dest))
		return pawscript.BoolStatus(true)
	})
	tileRet("viewport_up", "viewport_up [#tile]", (*ifitfits.Viewport).Up)
	tileRet("viewport_down", "viewport_down [#tile]", (*ifitfits.Viewport).Down)
	tileRet("viewport_left", "viewport_left [#tile]", (*ifitfits.Viewport).Left)
	tileRet("viewport_right", "viewport_right [#tile]", (*ifitfits.Viewport).Right)
	// viewport_prior / viewport_next are already mew commands (main-viewport
	// focus cycling); the tiler's reading-order cycle is tile_prior / tile_next.
	tileRet("tile_prior", "tile_prior [#tile]", (*ifitfits.Viewport).Prior)
	tileRet("tile_next", "tile_next [#tile]", (*ifitfits.Viewport).Next)

	// --- Move & reorder ---
	tileDir("viewport_swap", "viewport_swap [#tile], <direction>", (*ifitfits.Viewport).Swap)
	tileOnly("viewport_swap_up", "viewport_swap_up [#tile]", (*ifitfits.Viewport).SwapUp)
	tileOnly("viewport_swap_down", "viewport_swap_down [#tile]", (*ifitfits.Viewport).SwapDown)
	tileOnly("viewport_swap_left", "viewport_swap_left [#tile]", (*ifitfits.Viewport).SwapLeft)
	tileOnly("viewport_swap_right", "viewport_swap_right [#tile]", (*ifitfits.Viewport).SwapRight)
	tileDir("viewport_merge", "viewport_merge [#tile], <direction>", (*ifitfits.Viewport).Merge)
	tileOnly("viewport_merge_up", "viewport_merge_up [#tile]", (*ifitfits.Viewport).MergeUp)
	tileOnly("viewport_merge_down", "viewport_merge_down [#tile]", (*ifitfits.Viewport).MergeDown)
	tileOnly("viewport_merge_left", "viewport_merge_left [#tile]", (*ifitfits.Viewport).MergeLeft)
	tileOnly("viewport_merge_right", "viewport_merge_right [#tile]", (*ifitfits.Viewport).MergeRight)

	// --- Orientation ---
	tileOnly("viewport_flip", "viewport_flip [#tile]", (*ifitfits.Viewport).Flip)
	tileOnly("viewport_flip_parent", "viewport_flip_parent [#tile]", (*ifitfits.Viewport).FlipParent)
	tileOnly("viewport_reverse", "viewport_reverse [#tile]", (*ifitfits.Viewport).Reverse)
	tileOnly("viewport_reverse_parent", "viewport_reverse_parent [#tile]", (*ifitfits.Viewport).ReverseParent)

	// --- Stacks & tabs ---
	tileState("viewport_stack", "viewport_stack [#tile], [true|false|toggle]", (*ifitfits.Viewport).Stack)
	tileRet("viewport_tab_next", "viewport_tab_next [#tile]", (*ifitfits.Viewport).TabNext)
	tileRet("viewport_tab_prior", "viewport_tab_prior [#tile]", (*ifitfits.Viewport).TabPrior)
	tileOnly("viewport_move_tab_next", "viewport_move_tab_next [#tile]", (*ifitfits.Viewport).MoveTabNext)
	tileOnly("viewport_move_tab_prior", "viewport_move_tab_prior [#tile]", (*ifitfits.Viewport).MoveTabPrior)

	// --- Sizing ---
	tileState("viewport_zoom", "viewport_zoom [#tile], [true|false|toggle]", (*ifitfits.Viewport).Zoom)
	tileState("viewport_shrink", "viewport_shrink [#tile], [true|false|toggle]", (*ifitfits.Viewport).Shrink)
	tileOnly("viewport_normal", "viewport_normal [#tile]", (*ifitfits.Viewport).Normal)
	tileOnly("viewport_mode_next", "viewport_mode_next [#tile]", (*ifitfits.Viewport).ModeNext)
	tileOnly("viewport_mode_prior", "viewport_mode_prior [#tile]", (*ifitfits.Viewport).ModePrior)
	// viewport_expand / viewport_contract: tile + optional delta (default 1).
	delta := func(name, usage string, fn func(*ifitfits.Viewport, ifitfits.Handle, float64)) {
		ps.RegisterCommand(name, func(ctx *pawscript.Context) pawscript.Result {
			vp, t, rest, ok := e.tileFront(ctx)
			if !ok {
				e.ShowWarning("Usage: " + usage)
				return pawscript.BoolStatus(false)
			}
			d, hasD := tileArgFloat(ctx, rest)
			if !hasD {
				d = 1
			}
			fn(vp, t, d)
			return pawscript.BoolStatus(true)
		})
	}
	delta("viewport_expand", "viewport_expand [#tile], [delta]", (*ifitfits.Viewport).Expand)
	delta("viewport_contract", "viewport_contract [#tile], [delta]", (*ifitfits.Viewport).Contract)
	// viewport_resize: tile + optional size (<= 0 unpins).
	ps.RegisterCommand("viewport_resize", func(ctx *pawscript.Context) pawscript.Result {
		vp, t, rest, ok := e.tileFront(ctx)
		if !ok {
			e.ShowWarning("Usage: viewport_resize [#tile], [size]")
			return pawscript.BoolStatus(false)
		}
		size, _ := tileArgFloat(ctx, rest) // absent -> 0 -> unpin
		vp.Resize(t, size)
		return pawscript.BoolStatus(true)
	})
	// viewport_equalize / viewport_balance: tile + optional recursive flag.
	recursive := func(name, usage string, fn func(*ifitfits.Viewport, ifitfits.Handle, bool)) {
		ps.RegisterCommand(name, func(ctx *pawscript.Context) pawscript.Result {
			vp, t, rest, ok := e.tileFront(ctx)
			if !ok {
				e.ShowWarning("Usage: " + usage)
				return pawscript.BoolStatus(false)
			}
			fn(vp, t, tileArgBool(ctx, rest, false))
			return pawscript.BoolStatus(true)
		})
	}
	recursive("viewport_equalize", "viewport_equalize [#tile], [recursive]", (*ifitfits.Viewport).Equalize)
	recursive("viewport_balance", "viewport_balance [#tile], [recursive]", (*ifitfits.Viewport).Balance)

	// --- Lenses ---
	tileState("viewport_monocle", "viewport_monocle [#tile], [true|false|toggle]", (*ifitfits.Viewport).Monocle)
	tileState("viewport_local_monocle", "viewport_local_monocle [#tile], [true|false|toggle]", (*ifitfits.Viewport).LocalMonocle)
	tileState("viewport_spectacle", "viewport_spectacle [#tile], [true|false|toggle]", (*ifitfits.Viewport).Spectacle)
	tileState("viewport_local_spectacle", "viewport_local_spectacle [#tile], [true|false|toggle]", (*ifitfits.Viewport).LocalSpectacle)

	// --- Focus, caret & queries ---
	tileOnly("viewport_set_focus", "viewport_set_focus [#tile]", (*ifitfits.Viewport).SetFocus)
	ps.RegisterCommand("viewport_get_focus", func(ctx *pawscript.Context) pawscript.Result {
		ctx.SetResult(uint64(e.ensureTiler().GetFocus()))
		return pawscript.BoolStatus(true)
	})
	ps.RegisterCommand("viewport_set_caret", func(ctx *pawscript.Context) pawscript.Result {
		vp, t, rest, ok := e.tileFront(ctx)
		x, okX := tileArgFloat(ctx, rest)
		y, okY := tileArgFloat(ctx, rest+1)
		if !ok || !okX || !okY {
			e.ShowWarning("Usage: viewport_set_caret [#tile], <x>, <y>")
			return pawscript.BoolStatus(false)
		}
		vp.SetCaret(t, x, y)
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
		vp, t, _, ok := e.tileFront(ctx)
		if !ok {
			e.ShowWarning("Usage: viewport_content [#tile]")
			return pawscript.BoolStatus(false)
		}
		ctx.SetResult(vp.Content(t))
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
		vp, t, rest, ok := e.tileFront(ctx)
		ref, okR := tileArgStr(ctx, rest)
		if !ok || !okR {
			e.ShowWarning("Usage: viewport_set [#tile], <newRef>")
			return pawscript.BoolStatus(false)
		}
		vp.Set(t, ref)
		return pawscript.BoolStatus(true)
	})
}
