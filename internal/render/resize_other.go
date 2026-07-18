//go:build !unix

package render

// watchNativeResize is a no-op on platforms without SIGWINCH (e.g. Windows):
// terminal resizes are driven manually via TriggerResize or a host-provided
// resize channel.
func watchNativeResize(sr *ScreenRenderer) func() {
	return func() {}
}
