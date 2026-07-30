Build-time payload directory.

`make windows-sdl` builds the console binary first and drops it here as
`mew.exe.gz`, where a `//go:embed` under the `embedconsole` build tag picks it
up (see ../embedded_console_windows.go). The graphical binary can then write
the console one out beside itself at install time, so a single downloaded
mew-sdl.exe installs both.

Nothing here is checked in: it is a build artefact, and the embed only compiles
when the tag asks for it.
