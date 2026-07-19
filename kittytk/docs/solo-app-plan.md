# Solo App Plan

## Standing rule: everything goes through the protocol

**We always develop client/server functionality.** No application built on
this toolkit should reach the UI except through the display protocol
(D10-D22): it builds and drives its interface as protocol text over a
connection, whether that connection is in-process (`client.NewInProcess`)
or a socket to a display service (`client.Dial`). New capabilities are
added as protocol vocabulary (properties, verbs, handshake options) plus
the host-side handling that services them - never as an in-process-only
trinket API an app calls directly. When a feature needs the app to reach
something only the host owns (the focused trinket, window arrangement, the
desktop), it becomes a protocol verb/property, as the display app-verbs
(`cut`, `tile`, `theme`, `rawkey`, ...) and window properties (`main`,
`tearable`, `font`) already do.

## Overview

Let an application be "the whole thing": its main window replaces the
desktop entirely and fills the display, rendered in the same self-contained
form a main window already takes when it is torn off - its own menu bar and
status bar, no desktop wallpaper, dock, or system menu behind it. This is
**solo mode**.

The torn-off state is already the visual target. A torn main window is a
`platform.SurfaceHandler` (`window.TearOffHost`) that paints a
window-with-chrome and handles its own input, popups, cursor and clipboard.
Solo mode is that same picture made the root, with no desktop hosting it.

## Settled decisions

- **No Psi / system menu.** A solo app owns 100% of the chrome; there is no
  desktop system menu. (The empty-desktop rule - only Psi when an app has
  neither menus nor windows - is a step toward "no system furniture unless
  earned"; solo mode drops Psi entirely.)
- **No redock handle.** The main window is not tearable in solo mode (there
  is nothing to dock back to), so it shows no `%`/`#` handle.
- **Additional windows.** A solo app may still open more windows. Each runs
  like another solo surface (its own OS surface) or lives in an MDI pane -
  the app's choice. They are genuine peers of the main window, not children
  on a desktop.
- **Lifecycle: genuine peers, quit on last close.** No app or window is
  privileged. The host owns the lifetime and quits when the *last* app
  window closes (or on explicit quit) - the ordinary multi-window
  convention, not "the first window is special." The "everything dies when
  the first app exits" behavior is **not** a design rule; it only appears in
  the *bundled* deployment, where the host lives inside that first app's
  process (a single-binary launch), so the process's death naturally takes
  the host - and everything it hosts - with it. A standing host gives true
  peers with no such coupling.
- **No host window or host chrome.** The host is the surface/event-loop
  owner; it is not itself a visible window and paints no chrome of its own.
  Every OS surface in solo mode is an app window - there is never an extra
  host window, wallpaper, or desktop furniture floating alongside the app.
  The OS/SDL event loop is process-level, so it survives any single window
  closing; the app windows are the only real surfaces. The only subtlety is
  the *primary* surface (where the loop began): to avoid a lingering blank
  surface when that window closes while peers remain, no surface renders host
  chrome, so "primary" is purely internal - the Shell extraction (Path B)
  makes this exact.
- **Protocol-driven.** Solo is declared over the wire (see below); it is not
  an in-process-only mode. Per the standing rule, the same declaration works
  in-process and remote.
- **Desktop may return later.** Nothing here forbids re-introducing a
  desktop *inside* a solo app someday (a desktop is a trinket); it is simply
  not needed now.

## Protocol surface

Solo is a property of the whole connection/app, known at connect time, so it
rides the handshake:

```
hello version=1 app="My App" solo
```

- `client.DialSolo(path, appName, dispatch)` (and an in-process equivalent)
  sends the `solo` flag.
- The display records it on the connection. When that connection adopts a
  top-level window marked `main` (the `main` window property), the display
  puts its desktop into solo mode bound to that window.
- Additional windows may come from the same connection or from new
  connections; all are peers. The host quits when the *last* app window
  closes - disconnecting one app never affects the others.

## Two implementation paths

- **Path A - solo mode on the existing Desktop (first).** Keep Desktop as
  the host but strip it: no wallpaper, no dock, no Psi menu; the `main`
  window is maximized to the full surface and non-tearable; its menu/status
  render as the (Psi-less) bar. Reuses the event loop, window manager,
  timers and cursor wiring already in place. A now-invisible Desktop still
  sits underneath - hidden, not replaced - which is acceptable for now. Its
  one rough edge is the peer lifecycle: if the primary window closes while
  peer windows remain, the stripped Desktop must not leave a blank primary
  surface behind (promote a peer, or keep the loop alive with nothing on the
  primary until the last peer closes). Path B removes this wrinkle.
- **Path B - extract a `Shell` host (later).** Factor the platform services
  both `Desktop` and `TearOffHost` consume (surface/backend, event loop,
  timer system, `CursorController`, clipboard, global pointer) into one host
  type that paints no chrome of its own. Then window+chrome surfaces run
  directly on a `Shell` as peers with no Desktop. Torn window = "a Shell
  hosting one window on a secondary surface"; solo app = "... on the primary
  surface"; Desktop = "a Shell that also has wallpaper/dock/WM/multiple
  apps." Because the Shell renders nothing itself, every surface is an app
  window, "primary" is purely internal, and the loop simply runs until the
  last surface closes - genuine peers with no host window. Build A first and
  let it reveal exactly which services B must extract.

## Milestones

1. **Solo handshake + reshape the primary window (done).** `solo` in the
   handshake; `client.DialSolo`. When a solo app's `main` window is adopted,
   the desktop **reshapes its own OS window into the app's window**: it strips
   the border (via the new `platform.BorderToggler` capability -
   `SDL_SetWindowBordered`) so the app's chrome is the only title bar, and
   hosts the main window on that same surface via `TearOffHost` (which fills
   the surface and maps keyboard/edge resize onto the OS window). No second
   window is created and none is closed - the SDL platform *refuses* to close
   its main window (it owns the event loop), so reshaping the one window is
   the way. The desktop lives on as a windowless coordinator and quits when
   the last window closes. Tested headlessly with the msPlatform harness (one
   surface, main window hosted and filling it, quit on last close).
2. **Additional windows as peer surfaces (done).** In solo there is no
   desktop surface, so every added window is torn onto its own surface (a
   peer) - which also makes secondary-app "New Window" appear. Peers keep no
   tear handle and are not zoomed; the host quits when the last surface
   closes.
3. **Primary-surface promotion (done).** The primary surface owns the event
   loop and cannot be destroyed, so closing the window that sits on it (via
   `[x]` or an internal quit action) does not end the app while peers remain.
   Instead the primary surface **takes on the personality of a remaining
   window**: a peer is promoted onto it - preferring a window that requested
   `main` - and the surface repositions and resizes to where that peer was
   (its screen origin and size), so the promoted window keeps its placement.
   The promoted peer's own surface is discarded. Only when the truly last
   window closes does the host quit. On a single-surface platform (a terminal
   or headless polling backend) windows stay docked instead of hosted, so the
   quit-on-last signal comes from the window manager's removed-hook rather
   than the tear-off path; a `soloHosting` guard keeps the internal lift
   (which removes a window from the manager to host it) from being mistaken
   for the last close. Tested with the msPlatform harness (primary refuses
   Close like SDL's main window; closing it promotes a peer with
   reposition/resize; closing the last quits).
4. **Solo <-> desktop toggle (done).** The root can flip between solo and a
   real desktop at runtime, requested by *any* client over the protocol (per
   the standing rule) - including a throwaway external tool, so the solo app
   need not cooperate:
   - `spawndesktop` verb -> `Desktop.ExitSoloMode()`: the primary surface is
     re-bordered and reclaimed by the desktop (wallpaper/dock/menu paint
     again), and the window that filled it becomes an ordinary tearable
     torn-off window on its own surface at the *same screen rectangle*, so it
     floats over the revealed desktop and can be dragged in to dock.
   - `gosolo` verb -> `Desktop.EnterSoloFromDesktop()`: the inverse - promote
     a detached app (preferring an app's `main` window) back to solo. Its
     surface is discarded and it fills the re-borderless primary, which
     *moves* to where that window was (`promoteToPrimary(..., reposition=true)`)
     so the app keeps its on-screen position instead of snapping to the
     desktop's spot.
   - `examples/spawndesktop` is the external tool: it dials, sends the verb
     (`-solo` sends `gosolo`), and exits.
   Tested with the msPlatform harness (primary re-borders and a torn window
   appears at the prior rect on exit; the torn surface is discarded and the
   primary keeps the display size on re-solo) and a display test asserting
   both verbs are accepted (not rejected as unknown session verbs).
5. **Shell extraction (later).** Formalize the windowless coordinator into a
   `Shell` so torn / solo / desktop share one host abstraction, and refine
   peer positioning (peers created after the primary closes still lose the
   desktop-relative origin between promotions).
