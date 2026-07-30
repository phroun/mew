package config

import "os"

// The config package no longer embeds its own copy of the shipped default
// resource files — the editor injects its embedded resources/ tree at runtime
// (SetEmbeddedResources). For the config package's OWN standalone tests, where
// the editor is not imported, point the injection at that same tree on disk so
// the built-in mappings baseline resolves its "defaults/…" @includes. init runs
// before any test, hence before the first builtinMappings() call.
func init() {
	SetEmbeddedResources(os.DirFS("../editor/resources"))
}
