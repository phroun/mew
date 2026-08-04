# ifitfits (JavaScript)

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)

> **ifitfits** — *a viewport tiling engine ("if it fits, it shows")*

*If you use this, please support me on ko-fi:  [https://ko-fi.com/jeffday](https://ko-fi.com/F2F61JR2B4)*

[![ko-fi](https://ko-fi.com/img/githubbutton_sm.svg)](https://ko-fi.com/F2F61JR2B4)

A faithful JavaScript port of the [Go package](../). Same model, same method
names, same behavior — the two implementations are validated against a shared
set of parity tests so a host can be written against either.

`ifitfits.js` is a UMD module: it works with a plain `<script>` tag (exposing a
global `ifitfits`), with CommonJS `require`, and with an AMD loader.

## Use

```html
<script src="ifitfits.js"></script>
<script>
  const ff = window.ifitfits;
  const [vp, first] = ff.NewViewport(1920, 1080); // one tile, filling the workspace
  vp.Set(first, "editor");                         // give it a content ref

  const right = vp.Split(first, ff.Right);         // split; clones "editor"
  vp.Set(right, "terminal");
  vp.Stack(right, ff.On);                          // fold its group into tabs

  for (const b of vp.Tiles()) draw(b.Ref, b.Rect); // resolved boxes, host draws them
  for (const s of vp.Stacks()) drawTabStrip(s);    // tab strips (incl. hidden tabs)

  const dest = vp.Go(first, ff.Right);             // navigate; returns the destination tile
  vp.Monocle(dest, ff.On);                         // magnify it to fill the screen
</script>
```

The host owns focus — `ifitfits` never does. Every command takes an explicit
tile handle; report focus with `SetFocus` only so an active lens knows when to
dismiss. The full command surface (and the **mew** / PawScript `viewport_*`
names each method mirrors) is in [../COMMANDS.md](../COMMANDS.md).

## Example

[`example/index.html`](example/index.html) is a complete host application: it
owns focus, binds the keyboard to library commands, and renders `vp.Tiles()`,
`vp.Stacks()`, and `vp.Caret()` to the DOM. Open it in a browser — no build
step, no dependencies.

## Test

```
node ifitfits.test.js
```

These mirror the Go package's tests one-for-one; both print the same pass count.

## License

MIT — see [../LICENSE](../LICENSE).
