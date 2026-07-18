# Deferred Work

Follow-ups flagged during development, waiting on upstream changes or a
future pass. Items here are intentional deferrals, not forgotten.

## Waiting on upstream

- **PawScript: pluggable FS interface** — planned upstream. Once available,
  route the `files::` module through mew's `FileSystem` callbacks so
  scripts cannot bypass a host-virtualized file system.
- **PawScript: ExecuteFileAsync** — ExecuteFile blocks waiting for async
  tokens, and the profile script runs before the editor's input loop
  starts, so a profile.mew that invokes a prompting command (go_line,
  find, ...) would deadlock startup: the prompt can only be answered once
  the loop runs. An async file-execution API (filename attribution +
  root-state export merging, without the token wait) would let profile
  scripts prompt; mew would resume them naturally once input flows.
- **PawScript: resume-false on token timeout** — when a suspension token
  times out, ForceCleanupToken silently drops the suspended command
  sequence: neither the `&` nor the `|` branch ever runs. Resuming the
  sequence with status=false before cleanup would let `| alternative`
  fire at the moment of expiry. Mew already fails safe on its side
  (a prompt answered after its token expired warns "Prompt timed out"
  and performs nothing, and the timeout is configurable via the
  promptTimeout/scriptTimeout options), but the at-expiry else-branch
  needs the upstream change. Also: the context-level Context.RequestToken
  hardcodes 5 minutes — mew now uses the host-level PawScript.RequestToken
  (which takes a timeout) instead, but script-internal suspensions (stdin
  reads, etc.) still hit the hardcoded value rather than scriptTimeout.

## Mew-side follow-ups

- **Script sandboxing options in the library API** — PawScript has
  ExecRoots to lock down `os::exec` paths and a LIBRARY restrict command to
  configure the exposed interface. Add mew library options so embedding
  hosts can set these (e.g. `WithExecRoots`, `WithRestrictedLibrary`).
- **Filename autocompletion** — the `FileSystem.Glob` callback is wired and
  waiting; build prompt autocompletion on top of it.
- **Richer script stdin semantics** — PawScript's stdin is now wired to the
  editor's (possibly virtual) terminal input, so scripts stay inside the
  session. A script reading stdin still shares that stream with the key
  handler; a prompt-driven stdin (script reads route through the prompt
  system) would remove the contention.
- **Headless stdin-exhaustion guard** — with a virtual terminal whose input
  reader hits EOF, the key handler blocks forever and the session never
  ends. Consider treating input EOF as an exit signal (or surfacing it to
  the host) for scripted sessions.
