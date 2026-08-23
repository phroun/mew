package editor

import "github.com/phroun/mew/internal/config"

// The startup log is what the config files said back.
//
// mew reads config softly: an option it does not recognize is skipped and a
// value it cannot parse keeps its default, which is right for options, where a
// typo costs one setting. Key mappings are different. A mistyped binding is not
// missing — it is bound, to a key nobody presses — so nothing about using mew
// afterwards points at the line that did it, and the user is left believing the
// keymap they wrote is the keymap they have.
//
// So the parser collects what it could not honor (config.Warnings) and this
// puts it where a person will see it: an ordinary buffer, split beside whatever
// was opened, exactly as a launch --eval's output arrives. Ordinary is the
// point — it scrolls, it closes, and it prompts for nothing, because it is a
// note rather than a dialog. A clean start collects nothing and shows nothing.

// showStartupLog opens the config warnings in a buffer beside the launch files,
// or does nothing when the config was read without complaint.
//
// Called before the launch --eval batch, so an eval's own output buffer lands
// on top with the focus: the evals are what the user asked for, and the log is
// something mew wanted to mention.
func (e *Editor) showStartupLog() {
	text := config.StartupLog(e.LoadedConfig.Warnings)
	if text == "" {
		return
	}
	// Seeded as loaded content, so the buffer starts unmodified and closing it
	// never prompts to save. Unfocused: the log is beside the work, not in
	// front of it.
	buf := e.lib.NewFromString(text)
	e.createMainViewport(buf, nil, false)
}
