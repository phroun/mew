//go:build kittytk && windows

package main

// SIGUSR1 does not exist on Windows; the diagnostic dump is a no-op there.
func installDebugDump() {}
