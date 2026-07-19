# assets

Icon source for the macOS app bundle (`make macapp`), which gives mew a proper
Dock / task-switcher icon that a bare binary can't have.

- **`mew.icns`** — the app icon. If present, `make macapp` copies it into the
  bundle. This is the file to add.
- **`mew.png`** — a 1024×1024 source image. If there is no `mew.icns`,
  `make macapp` builds one from this on macOS (via `sips` + `iconutil`).

Provide either one (`.icns` is used directly; `.png` is converted on macOS).
With neither, the bundle is still built, just with the generic system icon.

The mascot is miau-muah (see the root README).
