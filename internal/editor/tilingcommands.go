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
	// Cycling onto an existing untiled viewport (adoptFocusInPlace) reseats the
	// focused tile onto it, showing it in the current pane; otherwise — a command
	// that just CREATED a viewport — split a fresh tile beside it, as before.
	if e.adoptFocusInPlace {
		vp.Set(cur, id)
		vp.SetFocus(cur)
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

// tileDirArg parses the argument of an operator command (viewport_go /
// viewport_swap / viewport_merge / viewport_split), which is either a real
// direction OR one of the meta-tokens "pending" / "mode" that arm the operator
// instead of running it. Returns (dir, meta, ok): meta is "" for a real
// direction (dir valid), or "pending"/"mode" (dir unused). ok is false only when
// the argument is missing or an unrecognized word.
func tileDirArg(ctx *pawscript.Context, i int) (ifitfits.Direction, string, bool) {
	s, has := tileArgStr(ctx, i)
	if !has {
		return 0, "", false
	}
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "pending":
		return 0, "pending", true
	case "mode":
		return 0, "mode", true
	}
	d, ok := tileArgDir(ctx, i)
	return d, "", ok
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

// goToTile makes dest the active tile after a navigation: it becomes the #tile
// default, and when the tile carries a live mew viewport ref, focus follows there
// (driving lastMainViewport and the modebar) with the usual switch announcement.
// Publishes dest as the command result. Shared by viewport_go and the focusing
// tile_prior / tile_next cycle.
func (e *Editor) goToTile(ctx *pawscript.Context, vp *ifitfits.Viewport, dest ifitfits.Handle) {
	vp.SetFocus(dest)                                 // the destination is now the active tile
	ctx.SetModuleObject(tileDefaultVar, uint64(dest)) // #tile follows the move
	if ref := vp.Content(dest); ref != "" {
		if e.ViewportManager.GetViewport(ref) != nil {
			e.ViewportManager.SetFocus(ref) // sets lastMainViewport, as a normal switch
			e.announceFocusedViewport()
		}
	}
	ctx.SetResult(uint64(dest))
}

// ---- tiling operator mode (go / swap / merge / split) ----

// tileModeOrGo returns the persistent tiling mode, treating the empty value as
// the default "go".
func (e *Editor) tileModeOrGo() string {
	if e.tileMode == "" {
		return "go"
	}
	return e.tileMode
}

// takeTileOp returns the operator a directional dispatch (viewport_left, …)
// should carry out: the one-shot pending operator when armed (consuming it, so
// the next press falls back to the mode), else the persistent mode.
func (e *Editor) takeTileOp() string {
	if e.tilePending != "" {
		op := e.tilePending
		e.tilePending = ""
		return op
	}
	return e.tileModeOrGo()
}

// armTileOperator handles the `viewport_<op> pending` / `viewport_<op> mode`
// meta-directions: "pending" arms op as the one-shot operator; "mode" makes op
// the persistent mode, toggling back to "go" when op is already the mode (so the
// mode keys double as toggles). Choosing a mode also clears any pending arm.
func (e *Editor) armTileOperator(op, meta string) {
	switch meta {
	case "pending":
		e.tilePending = op
		e.announceTileArm(op, true)
	case "mode":
		if e.tileModeOrGo() == op {
			e.tileMode = "go"
		} else {
			e.tileMode = op
		}
		e.tilePending = ""
		e.announceTileArm(e.tileModeOrGo(), false)
	}
}

// announceTileArm surfaces the current arm as a transient notification so a
// persistent mode (or a pending one-shot) is not an invisible state.
func (e *Editor) announceTileArm(op string, pending bool) {
	if pending {
		e.ShowNotification("Tile: " + op + " (pending)")
		return
	}
	e.ShowNotification("Tile mode: " + op)
}

// applyTileOp carries out tiling operator op in direction d on tile t: "go"
// focus-moves there, "swap"/"merge" reorder against the neighbor, "split" opens
// a new tile that way. Reports whether the op ran.
func (e *Editor) applyTileOp(ctx *pawscript.Context, vp *ifitfits.Viewport, t ifitfits.Handle, op string, d ifitfits.Direction) bool {
	switch op {
	case "swap":
		vp.Swap(t, d)
	case "merge":
		vp.Merge(t, d)
	case "split":
		return e.splitDir(vp, t, d) != 0
	default: // "go"
		e.goToTile(ctx, vp, vp.Go(t, d))
	}
	return true
}

// doSplit runs viewport_split/new's create-or-clone placement for a fixed
// direction: an explicit ref shows that viewport; otherwise a fresh buffers
// surface is opened (and cleaned up if the split fails); the new tile gets mew's
// default minimums. Returns the new tile handle (0 on failure).
func (e *Editor) doSplit(vp *ifitfits.Viewport, fn func(*ifitfits.Viewport, ifitfits.Handle, ifitfits.Direction, ...string) ifitfits.Handle, t ifitfits.Handle, d ifitfits.Direction, ref string, hasRef bool) ifitfits.Handle {
	var h ifitfits.Handle
	switch {
	case hasRef:
		h = fn(vp, t, d, ref)
	default:
		if nw := e.newSurfaceViewport("buffers"); nw != nil {
			h = fn(vp, t, d, nw.ID)
			if h == 0 {
				e.ViewportManager.RemoveViewport(nw.ID) // split failed: don't orphan the pane
			}
		} else {
			h = fn(vp, t, d)
		}
	}
	if h != 0 {
		vp.SetMetrics(h, newTileMinW, newTileMinH, 0, 0)
	}
	return h
}

// splitDir is the "split" operator: a viewport_split in direction d with no ref
// (opening a buffers surface in the new tile).
func (e *Editor) splitDir(vp *ifitfits.Viewport, t ifitfits.Handle, d ifitfits.Direction) ifitfits.Handle {
	return e.doSplit(vp, (*ifitfits.Viewport).Split, t, d, "", false)
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
	// opCmd: an operator base command (viewport_go / viewport_swap /
	// viewport_merge). Its argument is a direction (run op that way now) OR the
	// meta-tokens pending/mode (arm op for the directional dispatch keys instead).
	opCmd := func(name, op string) {
		ps.RegisterCommand(name, func(ctx *pawscript.Context) pawscript.Result {
			vp, t, rest, ok := e.tileFront(ctx)
			if !ok {
				e.ShowWarning("Usage: " + name + " [#tile], <direction|pending|mode>")
				return pawscript.BoolStatus(false)
			}
			d, meta, okD := tileDirArg(ctx, rest)
			if !okD {
				e.ShowWarning("Usage: " + name + " [#tile], <direction|pending|mode>")
				return pawscript.BoolStatus(false)
			}
			if meta != "" {
				e.armTileOperator(op, meta)
				return pawscript.BoolStatus(true)
			}
			return pawscript.BoolStatus(e.applyTileOp(ctx, vp, t, op, d))
		})
	}
	// opDir: a fixed-direction convenience of an operator (viewport_go_left,
	// viewport_split_up, …) — no direction argument, op and direction both baked.
	opDir := func(name, op string, d ifitfits.Direction) {
		ps.RegisterCommand(name, func(ctx *pawscript.Context) pawscript.Result {
			vp, t, _, ok := e.tileFront(ctx)
			if !ok {
				e.ShowWarning("Usage: " + name + " [#tile]")
				return pawscript.BoolStatus(false)
			}
			return pawscript.BoolStatus(e.applyTileOp(ctx, vp, t, op, d))
		})
	}
	// dispatch: a directional command (viewport_left, …) that carries out the
	// currently armed operator — the one-shot pending one if set (consumed), else
	// the persistent mode (default "go").
	dispatch := func(name string, d ifitfits.Direction) {
		ps.RegisterCommand(name, func(ctx *pawscript.Context) pawscript.Result {
			vp, t, _, ok := e.tileFront(ctx)
			if !ok {
				e.ShowWarning("Usage: " + name + " [#tile]")
				return pawscript.BoolStatus(false)
			}
			return pawscript.BoolStatus(e.applyTileOp(ctx, vp, t, e.takeTileOp(), d))
		})
	}

	// --- Structure ---
	// viewport_new / viewport_split: tile (idiom) + a required direction, with an
	// optional ref. Given a ref, the new tile shows that existing viewport;
	// without one, a FRESH main viewport is created showing the mew:/buffers list,
	// so the new pane opens on a place to pick what to show rather than cloning the
	// origin tile's content. Both return the new tile, stamped with our default
	// minimums so it is not omitted on a modest workspace.
	newSplit := func(name, usage string, fn func(*ifitfits.Viewport, ifitfits.Handle, ifitfits.Direction, ...string) ifitfits.Handle) {
		ps.RegisterCommand(name, func(ctx *pawscript.Context) pawscript.Result {
			vp, t, rest, ok := e.tileFront(ctx)
			d, okD := tileArgDir(ctx, rest)
			if !ok || !okD {
				e.ShowWarning("Usage: " + usage)
				return pawscript.BoolStatus(false)
			}
			ref, hasRef := tileArgStr(ctx, rest+1)
			h := e.doSplit(vp, fn, t, d, ref, hasRef)
			ctx.SetResult(uint64(h))
			return pawscript.BoolStatus(true)
		})
	}
	newSplit("viewport_new", "viewport_new [#tile], <direction>, [ref]", (*ifitfits.Viewport).New)
	// viewport_split takes a direction (split that way, opening a buffers surface
	// when no ref is given) OR a meta-token pending/mode that arms the "split"
	// operator for the directional dispatch keys.
	ps.RegisterCommand("viewport_split", func(ctx *pawscript.Context) pawscript.Result {
		vp, t, rest, ok := e.tileFront(ctx)
		if !ok {
			e.ShowWarning("Usage: viewport_split [#tile], <direction|pending|mode>, [ref]")
			return pawscript.BoolStatus(false)
		}
		d, meta, okD := tileDirArg(ctx, rest)
		if !okD {
			e.ShowWarning("Usage: viewport_split [#tile], <direction|pending|mode>, [ref]")
			return pawscript.BoolStatus(false)
		}
		if meta != "" {
			e.armTileOperator("split", meta)
			return pawscript.BoolStatus(true)
		}
		ref, hasRef := tileArgStr(ctx, rest+1)
		ctx.SetResult(uint64(e.doSplit(vp, (*ifitfits.Viewport).Split, t, d, ref, hasRef)))
		return pawscript.BoolStatus(true)
	})
	// Fixed-direction split convenience family (mirrors viewport_swap_*).
	opDir("viewport_split_up", "split", ifitfits.Up)
	opDir("viewport_split_down", "split", ifitfits.Down)
	opDir("viewport_split_left", "split", ifitfits.Left)
	opDir("viewport_split_right", "split", ifitfits.Right)
	// tile_close closes a TILE (not the mew viewport it holds — that is
	// viewport_close).
	tileRet("tile_close", "tile_close [#tile]", (*ifitfits.Viewport).Close)

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
	// viewport_seek_{up,down,left,right}: the raw directional seek — resolve the
	// neighbor that way and return its handle, with NO focus move or #tile change
	// (the old viewport_up/down/left/right behavior, now under the seek_ name).
	tileRet("viewport_seek_up", "viewport_seek_up [#tile]", (*ifitfits.Viewport).Up)
	tileRet("viewport_seek_down", "viewport_seek_down [#tile]", (*ifitfits.Viewport).Down)
	tileRet("viewport_seek_left", "viewport_seek_left [#tile]", (*ifitfits.Viewport).Left)
	tileRet("viewport_seek_right", "viewport_seek_right [#tile]", (*ifitfits.Viewport).Right)
	// viewport_go is mew's navigation wrapper around viewport_seek: it seeks,
	// makes the destination the new #tile default, and switches mew's view to the
	// destination tile's content — the tile's ref is a mew viewport id, so
	// focusing it drives lastMainViewport and the modebar exactly as an ordinary
	// focus change would. When the destination tile carries a ref that is not a
	// live viewport (e.g. an as-yet-unpopulated tile), the view is left as-is; the
	// #tile default still follows the move. It also accepts pending/mode to arm
	// "go" as the directional operator (the default mode).
	opCmd("viewport_go", "go")
	opDir("viewport_go_up", "go", ifitfits.Up)
	opDir("viewport_go_down", "go", ifitfits.Down)
	opDir("viewport_go_left", "go", ifitfits.Left)
	opDir("viewport_go_right", "go", ifitfits.Right)
	// viewport_{up,down,left,right}: carry out the ARMED tiling operator in that
	// direction — a one-shot pending operator if set, else the persistent mode
	// (default "go", i.e. a focus move). Set the mode/pending with
	// viewport_<op> mode / viewport_<op> pending.
	dispatch("viewport_up", ifitfits.Up)
	dispatch("viewport_down", ifitfits.Down)
	dispatch("viewport_left", ifitfits.Left)
	dispatch("viewport_right", ifitfits.Right)
	// tile_prior / tile_next cycle the tiles in reading order and GO there —
	// updating the #tile default, focusing the destination tile's viewport, and
	// announcing the switch — the reading-order analog of viewport_go (not a raw
	// seek; viewport_seek prior/next is that). viewport_prior / viewport_next are
	// the separate main-viewport focus-cycling commands.
	ps.RegisterCommand("tile_prior", func(ctx *pawscript.Context) pawscript.Result {
		vp, t, _, ok := e.tileFront(ctx)
		if !ok {
			e.ShowWarning("Usage: tile_prior [#tile]")
			return pawscript.BoolStatus(false)
		}
		e.goToTile(ctx, vp, vp.Prior(t))
		return pawscript.BoolStatus(true)
	})
	ps.RegisterCommand("tile_next", func(ctx *pawscript.Context) pawscript.Result {
		vp, t, _, ok := e.tileFront(ctx)
		if !ok {
			e.ShowWarning("Usage: tile_next [#tile]")
			return pawscript.BoolStatus(false)
		}
		e.goToTile(ctx, vp, vp.Next(t))
		return pawscript.BoolStatus(true)
	})

	// --- Move & reorder ---
	// viewport_swap / viewport_merge take a direction OR pending/mode (arming the
	// operator for the directional dispatch keys); the _{up,down,left,right}
	// convenience commands bake the direction in.
	opCmd("viewport_swap", "swap")
	tileOnly("viewport_swap_up", "viewport_swap_up [#tile]", (*ifitfits.Viewport).SwapUp)
	tileOnly("viewport_swap_down", "viewport_swap_down [#tile]", (*ifitfits.Viewport).SwapDown)
	tileOnly("viewport_swap_left", "viewport_swap_left [#tile]", (*ifitfits.Viewport).SwapLeft)
	tileOnly("viewport_swap_right", "viewport_swap_right [#tile]", (*ifitfits.Viewport).SwapRight)
	opCmd("viewport_merge", "merge")
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
	// viewport_tab_next / viewport_tab_prior raise the next/prior tab in the stack
	// and GO to it: TabNext/TabPrior return the newly-shown tab's focus entry (the
	// leaf to land on), so — like tile_prior/next and viewport_go — focus follows
	// there, driving lastMainViewport and the modebar. Without this the tab was
	// raised but mew stayed focused on the old tab's viewport.
	tabGo := func(name, usage string, fn func(*ifitfits.Viewport, ifitfits.Handle) ifitfits.Handle) {
		ps.RegisterCommand(name, func(ctx *pawscript.Context) pawscript.Result {
			vp, t, _, ok := e.tileFront(ctx)
			if !ok {
				e.ShowWarning("Usage: " + usage)
				return pawscript.BoolStatus(false)
			}
			e.goToTile(ctx, vp, fn(vp, t))
			return pawscript.BoolStatus(true)
		})
	}
	tabGo("viewport_tab_next", "viewport_tab_next [#tile]", (*ifitfits.Viewport).TabNext)
	tabGo("viewport_tab_prior", "viewport_tab_prior [#tile]", (*ifitfits.Viewport).TabPrior)
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
