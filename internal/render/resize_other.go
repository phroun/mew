//go:build !unix && !windows

package render

// watchNativeResize is a no-op on platforms with neither SIGWINCH nor a way
// to ask: resizes are driven manually via TriggerResize or a host-provided
// resize channel. Windows has no signal but can be polled, so it has its own
// (resize_windows.go).
func watchNativeResize(sr *ScreenRenderer) func() {
	return func() {}
}
