// Package mew lets host applications embed the mew text editor.
//
// The editor runs its full terminal UI on the host's stdin/stdout; the
// library surface controls what goes in, what comes out, and how the editor
// touches its environment:
//
//	// Edit a file, exactly like the mew CLI:
//	err := mew.EditFile("notes.txt")
//
//	// Edit an in-memory document and get the result back:
//	result, err := mew.EditContent("draft text\n",
//		mew.WithoutUserConfig(),
//		mew.WithoutProfileScript(),
//	)
//
//	// Virtualize the file system and capture exit state:
//	result, err := mew.EditContent(doc,
//		mew.WithFileSystem(myVirtualFS),
//		mew.WithStateCallback(func(state map[string]interface{}) {
//			psl, _ := mew.EncodeState(state, mew.FormatPSL)
//			saveSomewhere(psl)
//		}),
//	)
package mew

import (
	"github.com/phroun/argwild"

	"github.com/phroun/mew/internal/config"
	"github.com/phroun/mew/internal/editor"
	"github.com/phroun/mew/internal/input"
	"github.com/phroun/mew/internal/version"
)

// Version is the mew major.minor release number; Build is the per-commit
// build counter (bumped by `make increment`). Both live in
// internal/version — the single source of truth — and are re-exported here
// for hosts.
const (
	Version = version.Version
	Build   = version.Build
)

// FullVersion returns the complete major.minor.build version string.
func FullVersion() string {
	return version.FullVersion()
}

// FileSystem is the set of callbacks mew uses for document file I/O: opening
// files into buffers, saving buffers, inserting file contents, writing
// blocks, and globbing for filename autocompletion. Hosts can virtualize it,
// stub individual operations, or disable them by returning errors. The
// default is the real operating system.
type FileSystem = editor.FileSystem

// FileInfo is the metadata mew's file-system abstraction exposes (see Statter
// and DirGlobber).
type FileInfo = editor.FileInfo

// Statter is an optional FileSystem capability: metadata for one path. A host
// FileSystem may implement it; mew uses it when present (e.g. to mark
// directories in filename completion) and does without when absent.
type Statter = editor.Statter

// DirGlobber is an optional FileSystem capability: a Glob that returns each
// match's metadata in one call, so completion learns which matches are
// directories without a Stat per result. Implement it when the host's
// directory listing already knows entry types; mew otherwise falls back to
// Glob + Stat.
type DirGlobber = editor.DirGlobber

// OSFileSystem returns the default FileSystem backed by the real operating
// system, for hosts that want to wrap or partially delegate to it.
func OSFileSystem() FileSystem {
	return editor.OSFileSystem()
}

// StateFormat selects the serialization format for persisted editor state.
type StateFormat = config.StateFormat

const (
	// FormatPSL serializes state as a PawScript Serialized List (default).
	FormatPSL = config.FormatPSL
	// FormatJSON serializes state as JSON, for hosts that prefer it.
	FormatJSON = config.FormatJSON
)

// EncodeState serializes a state map in the requested format.
func EncodeState(state map[string]interface{}, format StateFormat) (string, error) {
	return config.EncodeState(state, format)
}

// DecodeState parses serialized state, auto-detecting PSL ('(') or JSON
// ('{') and normalizing nested containers to plain maps and slices.
func DecodeState(content string) (map[string]interface{}, error) {
	return config.DecodeState(content)
}

// Option customizes an editor session.
type Option func(*editor.Config)

// WithFileSystem substitutes the file system callbacks used for document I/O.
func WithFileSystem(fs FileSystem) Option {
	return func(cfg *editor.Config) { cfg.FS = fs }
}

// WithMewFileSystem virtualizes mew's own support tree — everything mew used
// to keep under ~/.mew: editor.conf, profile.mew, the syntax/ grammars, its
// crash dumps. Those live behind a "mew:/" URL scheme; by default "mew:/x"
// maps to <home>/.mew/x on the real OS (see WithHomeDir). Supply a FileSystem
// here and each "mew:/x" path is instead handed to it verbatim (scheme intact),
// so the host owns the tree — nothing under the user's home directory is read
// or written. Confinement is preserved either way: a ".." in a mew: path can
// never escape above the tree root. This governs only mew's support tree;
// document I/O still flows through WithFileSystem.
func WithMewFileSystem(fs FileSystem) Option {
	return func(cfg *editor.Config) { cfg.MewFS = fs }
}

// WithStateCallback registers a callback invoked once as the editor shuts
// down, with a snapshot of the runtime state (current option values). Persist
// it with EncodeState in PSL or JSON as the host prefers.
func WithStateCallback(cb func(state map[string]interface{})) Option {
	return func(cfg *editor.Config) { cfg.StateCallback = cb }
}

// WithShowDesktop / WithHideDesktop wire the show_desktop / hide_desktop
// commands to host functions that reveal or hide the host's desktop (e.g. a
// KittyTK window-manager host). Unset, both commands are no-ops.
func WithShowDesktop(fn func()) Option {
	return func(cfg *editor.Config) { cfg.ShowDesktop = fn }
}

// WithHideDesktop wires the hide_desktop command (see WithShowDesktop).
func WithHideDesktop(fn func()) Option {
	return func(cfg *editor.Config) { cfg.HideDesktop = fn }
}

// WithoutUserConfig prevents loading ~/.mew/editor.conf; built-in defaults
// apply. For hosts that must not read the user's home directory.
func WithoutUserConfig() Option {
	return func(cfg *editor.Config) { cfg.SkipUserConfig = true }
}

// WithoutProfileScript prevents running (and creating) ~/.mew/profile.mew.
func WithoutProfileScript() Option {
	return func(cfg *editor.Config) { cfg.SkipProfileScript = true }
}

// WithConfigText supplies the editor configuration (editor.conf content) as
// a string, replacing the local config file. The [storage] section is NOT
// honored from host-supplied config — user-editable preferences must not
// redirect scratch storage; use WithColdStoragePath instead.
func WithConfigText(text string) Option {
	return func(cfg *editor.Config) { cfg.ConfigText = &text }
}

// WithProfileScript supplies the startup pawscript as a string, replacing
// ~/.mew/profile.mew.
func WithProfileScript(script string) Option {
	return func(cfg *editor.Config) { cfg.ProfileScript = &script }
}

// WithState restores a state snapshot previously handed to the
// StateCallback (decoded via DecodeState), applied over the loaded
// configuration. Together with WithConfigText, WithProfileScript, and
// WithStateCallback, the host acts as the full persistence go-between: mew
// never needs to touch the host machine's user files.
func WithState(state map[string]interface{}) Option {
	return func(cfg *editor.Config) { cfg.InitialState = state }
}

// WithColdStoragePath sets the local directory handed to Garland for cold
// storage of large files. Garland always requires a real local path even
// when document I/O is virtualized; unset, it comes from the local config's
// [storage] scratch setting (never from WithConfigText) or the system temp
// directory.
func WithColdStoragePath(path string) Option {
	return func(cfg *editor.Config) { cfg.ColdStoragePath = path }
}

// WithDeadcat opts a host into crash dumps: on DumpDeadcat, the modified-buffer
// dump (mew's DEADJOE) is written to name through the host's FileSystem, so a
// host can flush unsaved work during its own sudden shutdown. Standalone
// sessions on the real terminal manage their own DEADCAT and do not need this.
func WithDeadcat(name string) Option {
	return func(cfg *editor.Config) { cfg.DeadcatName = name }
}

// WithHomeDir overrides the home directory mew resolves against: the local
// "mew:/" tree root (<home>/.mew, unless WithMewFileSystem virtualizes it) and
// the "~" prefix in filename completion. Unset, mew uses the OS user's home
// directory. A host that keeps mew's files elsewhere — or must not touch the
// real home — sets this (or virtualizes the tree entirely with
// WithMewFileSystem).
func WithHomeDir(dir string) Option {
	return func(cfg *editor.Config) { cfg.HomeDir = dir }
}

// WithIdentity overrides the identity mew stamps into lock files and compares
// against when deciding whether a lock is its own: user@host.pid. Any empty
// string / non-positive pid falls back to the OS value (the login name, the
// hostname, the process id). A host running many editor sessions, or one whose
// notion of "who" differs from the OS process, sets this so locks read and
// resolve correctly.
func WithIdentity(user, host string, pid int) Option {
	return func(cfg *editor.Config) {
		cfg.IdentityUser = user
		cfg.IdentityHost = host
		cfg.IdentityPID = pid
	}
}

// Terminal virtualizes the editor's terminal I/O: raw key input, rendered
// output, size queries, and resize signaling. Any nil field keeps the
// real-terminal behavior for that aspect, except native OS resize signals
// (SIGWINCH), which are only watched when no Terminal is supplied — a host
// signals size changes by sending on Resize after its terminal changes.
type Terminal = editor.TerminalIO

// WithTerminal supplies a virtualized terminal for the session.
func WithTerminal(t Terminal) Option {
	return func(cfg *editor.Config) { cfg.Terminal = &t }
}

// KeyFeed lets the host deliver parsed key input directly, replacing the
// byte-stream input half entirely: instead of mew running its own
// direct-key-handler over Terminal.Input, the host — which may already run
// direct-key-handler or an equivalent, a window manager for instance —
// forwards exactly the surfaces that pipeline normally provides, and only
// when it wants mew to have them (say, while a mew view is focused):
//
//   - SendKey(name): one parsed key per call, in direct-key-handler naming
//     ("a", "^K", "Escape", "M-Left", "F1"); mew's normalized aliases
//     ("esc", "M-left", "return") pass through unchanged.
//   - SendPaste(content, final): bracketed-paste content in order, chunked
//     on rune boundaries however the host likes, final chunk flagged. Each
//     paste collapses into a single undo revision and is never re-parsed
//     as keys.
//   - Close(): end of input; the session winds down after delivering any
//     events already sent (the state snapshot still reaches the host).
//
// Send calls block briefly if the editor is busy and the feed's buffer is
// full, and report false once the feed is closed. Rendering, size, and
// resize stay on the Terminal virtualization — combine WithKeyFeed with a
// WithTerminal whose Input is nil.
type KeyFeed = input.EventFeed

// NewKeyFeed creates a KeyFeed for WithKeyFeed.
func NewKeyFeed() *KeyFeed { return input.NewEventFeed() }

// WithKeyFeed makes the session take its key and paste input from a
// host-driven KeyFeed instead of a terminal byte stream. Terminal.Input is
// ignored while a feed is in use.
func WithKeyFeed(f *KeyFeed) Option {
	return func(cfg *editor.Config) { cfg.KeySource = f }
}

// EditFile runs a full editor session on the named file (empty for a new
// unnamed document), exactly like the mew command line.
func EditFile(filename string, opts ...Option) error {
	e, err := newEditor(opts)
	if err != nil {
		return err
	}
	defer e.Cleanup()
	return e.Run(filename)
}

// EditArgv runs an editor session from a pre-split argument vector (the CLI
// path — typically os.Args[1:]). Switches set options and operands are files
// to open, applied as a left-to-right walk: options accumulate and each file
// opens with the option set as it stands at that point (see internal/editor
// cli.go). Global options must precede the first file.
func EditArgv(args []string, opts ...Option) error {
	r, err := argwild.ParseArgs(args)
	if err != nil {
		return err
	}
	e, err := newEditor(opts)
	if err != nil {
		return err
	}
	defer e.Cleanup()
	return e.RunArgs(r)
}

// EditArgs runs an editor session from a raw argument STRING, so a host app
// embedding mew can hand it a command line to parse with full fidelity
// (quoting, numeric and PSL values). Same walk semantics as EditArgv.
func EditArgs(argLine string, opts ...Option) error {
	r, err := argwild.ParseString(argLine)
	if err != nil {
		return err
	}
	e, err := newEditor(opts)
	if err != nil {
		return err
	}
	defer e.Cleanup()
	return e.RunArgs(r)
}

// EditContent runs a full editor session on an in-memory document and
// returns the document's final content when the session ends.
func EditContent(content string, opts ...Option) (string, error) {
	e, err := newEditor(opts)
	if err != nil {
		return "", err
	}
	defer e.Cleanup()
	return e.RunContent(content)
}

// newEditor builds an editor from default config plus the given options.
func newEditor(opts []Option) (*editor.Editor, error) {
	cfg := editor.DefaultConfig()
	for _, opt := range opts {
		opt(&cfg)
	}
	return editor.New(cfg)
}
