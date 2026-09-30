# Tray Icons and Desktop Notifications: Ideas

> **Status: parked.** Nothing here is built or decided. It is a record of how
> we would approach it, so the thinking need not be redone. The open
> questions at the end are the owner's to answer before any of it starts.

## The display owns them

A tray icon and a notification are chrome the display puts up on an
application's behalf, the way it puts up a `messagebox`. An application asks
over the wire and never calls the operating system itself. That is the rule
`app-hosted-objects.md` already states -- the display is the authority over
what is on screen -- and it is what lets one application protocol land on
very different hosts:

- **An SDL host on a real OS:** the native tray and native notifications.
- **The KittyTK desktop environment:** a tray area in its own menu bar, and
  toasts it draws itself.
- **The TUI:** a status-bar message through the existing `NotifyPassive`, with
  the tray's menu reachable from the desktop's menu bar.

## Wire shape

- `new trayicon icon= tooltip= children={ ...menu items... }`, reusing the
  menu item model the menu bar already has, so actions and roles work the
  same way there as anywhere.
- `new notification title= body= icon= urgency=`, with optional actions.
  Events: `activate`, `action id=`, and `dismiss reason=`. The application's
  `destroy` withdraws it.
- **The display says what it can do**, because the platforms differ a great
  deal: whether actions are supported, whether a tray exists at all, whether
  a primary click means anything. A refusal travels back as a Trouble, not as
  a notification that silently never appears.

## Platforms

### Tray

SDL 3.2 added a tray API -- `SDL_CreateTray`, tray menus, tooltips --
covering Windows, macOS and Linux. That is the cheap path. Not yet checked:
whether the SDL we ship, or the purego binding, exposes it. If the binding
leaves it out, `sdl/sdl3/gapfill.go` is the existing pattern for binding the
missing calls.

Differences the model has to accept rather than paper over:

- macOS always opens the menu on a click, and expects a monochrome template
  image.
- Windows tells a left click from the right-click menu.
- On Linux SDL goes through appindicator, and stock GNOME shows no tray at
  all.

So a primary-click action should not be promised. The menu is the one thing
that works everywhere.

### Notifications

SDL has nothing here, so each platform needs its own code.

- **Linux:** the freedesktop notification service over D-Bus, through a
  pure-Go D-Bus library. The easiest and the most complete of the three:
  actions, replacing a notification in place, and a signal when it closes.
- **Windows:** a proper toast needs a registered AppUserModelID with a Start
  Menu shortcut. The balloon attached to a tray icon (`Shell_NotifyIcon` with
  `NIF_INFO`) needs no registration and shows as a toast on Windows 10 and
  11. So a Windows notification could ride on the tray icon, with a hidden
  one made for an application that has none.
- **macOS:** `UNUserNotificationCenter` works only from a signed app bundle
  with a bundle id; a bare binary gets no permission. The fallback is
  `osascript -e 'display notification ...'`, which works but is attributed to
  Script Editor and cannot report a click. How good notifications can be on a
  Mac is decided by mew's app bundle.

## What it touches here

1. **Icons.** A tray icon is an image, and graphical icons are on hold. A
   tray means deciding at least how an image travels on the wire and how the
   display caches it, even if icons on trinkets wait.
2. **Staying alive.** An application with no windows left ends today (the
   demo's `watchWindow`), and a solo desktop ends with its last window
   (`lastWindowClosed`). An application with a tray icon has to keep running
   with no windows, or the commonest reason to have one does not work.
3. **Threads.** SDL's tray callbacks arrive on the main thread and D-Bus
   signals on a goroutine of their own. Both are posted to the desktop
   thread, as other platform events are.
4. **Platform interfaces.** Optional ones, as `CursorController` is: a
   `TrayHost` and a `Notifier` a platform may implement, with the desktop
   drawing its own version where the platform does not.

## Order, if it starts

1. The wire types, the platform interfaces, and the desktop's own drawn
   versions. Testable here, and they give the TUI and the desktop
   environment something at once.
2. Linux notifications over D-Bus.
3. The SDL tray.
4. Windows balloons.
5. macOS, once the bundle question is settled.

## Open questions

- Does a tray item get a primary-click event at all, or only a menu?
- Are a notification's actions in the first version?
- How much of the icon work comes off hold for this?
