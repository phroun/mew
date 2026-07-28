//go:build !windows || !embedconsole

package selfinstall

// embeddedConsole returns nothing when no console binary was built into this
// one. Install then simply reports that mew.exe was not available, which is
// what it did before there was anything to carry.
func embeddedConsole() []byte { return nil }
