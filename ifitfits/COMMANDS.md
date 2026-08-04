# ifitfits — command reference

`ifitfits` is the renderer-agnostic viewport tiling engine behind mew's window
management. This file records the **mew command names** (PawScript) and their
`ifitfits` Go equivalents, for reference when wiring the library into mew.

## Model

- A **tile** is a leaf of the layout tree. Every tile has a unique, library-assigned
  **handle** (an opaque id). The `tile` argument to every command is a handle.
- A tile also carries a **ref** — an application-supplied content reference. Unlike a
  handle, a ref is **not** unique: the same ref may appear on many tiles (e.g. the
  same document mirrored into several tiles).
- The library does **not** track a "focused tile" — that is the host's job. `tile` is
  therefore **mandatory** on every command. The host only tells the library about
  focus (`set_focus`) so an active monocle/spectacle knows when to dismiss.
- Units are the host's own: the library is agnostic to what a coordinate means
  (pixels, cells, …). It only does arithmetic on the numbers it is given.

Convention: `[arg]` is optional; a trailing `(true, false, toggle)` means the state
argument enables / disables / toggles, and defaults to **toggle** when omitted.

## Structure

| mew | Go | notes |
|---|---|---|
| `viewport_new (tile), (direction), [ref]` | `New(tile, dir, ref…) Handle` | insert a new tile; **clones the origin tile's ref** unless `ref` is given. Returns the new tile. `direction`: `up down left right before after` |
| `viewport_split (tile), (direction), [ref]` | `Split(tile, dir, ref…) Handle` | wrap the tile in a new nested group in place; clones ref unless given |
| `viewport_close (tile)` | `Close(tile)` | remove the tile |

## Navigation

`viewport_go` returns the **resolved destination tile** and, as a side effect,
updates the caret goal.

| mew | Go | notes |
|---|---|---|
| `viewport_go (tile), (direction)` | `Go(tile, dir) Handle` | `direction`: `up down left right` (spatial, with edge-slide) or `prior next` (reading-order cycle) |
| `viewport_up/down/left/right (tile)` | `Up/Down/Left/Right(tile) Handle` | aliases for the spatial directions |
| `viewport_prior/next (tile)` | `Prior/Next(tile) Handle` | aliases for the reading-order cycle |

At a workspace edge a spatial `go` doesn't move focus; it slides the caret goal
within the tile: **opposite-edge → center → pressed-edge** on repeated presses.

## Move & reorder

| mew | Go | notes |
|---|---|---|
| `viewport_swap (tile), (direction)` | `Swap(tile, dir)` | swap with the neighbor; at the edge, slides the caret (non-swap) |
| `viewport_swap_up/down/left/right (tile)` | `SwapUp/SwapDown/SwapLeft/SwapRight(tile)` | aliases |
| `viewport_merge (tile), (direction)` | `Merge(tile, dir)` | move the tile into the adjacent group |
| `viewport_merge_up/down/left/right (tile)` | `MergeUp/MergeDown/MergeLeft/MergeRight(tile)` | aliases |

## Orientation

| mew | Go |
|---|---|
| `viewport_flip (tile)` | `Flip(tile)` |
| `viewport_flip_parent (tile)` | `FlipParent(tile)` |
| `viewport_reverse (tile)` | `Reverse(tile)` |
| `viewport_reverse_parent (tile)` | `ReverseParent(tile)` |

## Stacks & tabs

| mew | Go | notes |
|---|---|---|
| `viewport_stack (tile), (true,false,toggle)` | `Stack(tile, state)` | fold the group into a tabbed stack / unfold; omit = toggle |
| `viewport_tab_next (tile)` | `TabNext(tile) Handle` | cycle the tile's enclosing stack forward (nested odometer; outermost wraps). Returns the newly shown tile |
| `viewport_tab_prior (tile)` | `TabPrior(tile) Handle` | …backward |
| `viewport_move_tab_next (tile)` | `MoveTabNext(tile)` | reorder the active tab within its stack |
| `viewport_move_tab_prior (tile)` | `MoveTabPrior(tile)` | |

## Sizing (all resolve through the "negotiation target": climb past only-child / stack-tab levels to the first ancestor that actually tiles)

| mew | Go | notes |
|---|---|---|
| `viewport_zoom (tile), (true,false,toggle)` | `Zoom(tile, state)` | grab surplus among siblings; omit = toggle |
| `viewport_shrink (tile), (true,false,toggle)` | `Shrink(tile, state)` | yield space; omit = toggle |
| `viewport_normal (tile)` | `Normal(tile)` | clear zoom/shrink |
| `viewport_mode_next (tile)` | `ModeNext(tile)` | cycle normal → zoom → shrink |
| `viewport_mode_prior (tile)` | `ModePrior(tile)` | cycle normal → shrink → zoom |
| `viewport_expand (tile), [delta]` | `Expand(tile, delta)` | grow the resolved size by `delta` (default 1) via a pin |
| `viewport_contract (tile), [delta]` | `Contract(tile, delta)` | shrink by `delta` (= negative expand) |
| `viewport_resize (tile), [size]` | `Resize(tile, size)` | pin at exactly `size`; omit / `<= 0` unpins |
| `viewport_equalize (tile), [recursive]` | `Equalize(tile, recursive)` | throw out pins+modes, give each child an equal share (ignoring naturals); `recursive` descends |
| `viewport_balance (tile), [recursive]` | `Balance(tile, recursive)` | clear pins only; let naturals + priority re-derive |

## Lenses (magnify; any `set_focus` outside the magnified subtree dismisses; omit state = toggle)

`monocle` magnifies the **tile**; `spectacle` magnifies its **enclosing group**.
`local_` fills the target's own **group box**; the plain form fills the whole
**screen**.

| mew | Go | fills | target |
|---|---|---|---|
| `viewport_monocle (tile), (bool)` | `Monocle(tile, state)` | screen | tile |
| `viewport_local_monocle (tile), (bool)` | `LocalMonocle(tile, state)` | group | tile |
| `viewport_spectacle (tile), (bool)` | `Spectacle(tile, state)` | screen | group |
| `viewport_local_spectacle (tile), (bool)` | `LocalSpectacle(tile, state)` | group | group |

## Focus, caret & queries

| mew | Go | notes |
|---|---|---|
| `viewport_set_focus (tile)` | `SetFocus(tile)` | host reports focus **only** so a lens can dismiss when focus leaves it |
| `viewport_get_focus` | `GetFocus() Handle` | the last tile the host reported (the library does not otherwise own focus) |
| `viewport_set_caret (tile), (x), (y)` | `SetCaret(tile, x, y)` | `x,y` are **local** to the tile (0,0 = its top-left), clamped to its size |
| `viewport_get_tile (x), (y)` | `GetTile(x, y) Handle` | the tile at absolute workspace coords, hit-testing the fully resolved (on-screen) layout |

## Refs (content identity — shareable across tiles)

| mew | Go | notes |
|---|---|---|
| `viewport_content (tile)` | `Content(tile) string` | the ref currently on a tile |
| `viewport_get (ref), (includeHidden)` | `Get(ref, includeHidden) []Handle` | every tile carrying `ref`; `includeHidden` also returns tiles not currently visible |
| `viewport_set (tile), (newRef)` | `Set(tile, ref)` | replace a tile's ref |

## Host-facing (not mew commands, but the library's render/query surface)

| Go | notes |
|---|---|
| `NewViewport(w, h) (*Viewport, Handle)` | make a viewport with one initial tile; returns it and its handle |
| `SetWorkspace(w, h)` | set the workspace size (drives layout) |
| `Tiles() []Box` | the resolved, on-screen tiles (handle, ref, rect, mode, pin, …) for the host to draw |
| `Caret() (x, y)` | the current caret goal, in workspace coords |
