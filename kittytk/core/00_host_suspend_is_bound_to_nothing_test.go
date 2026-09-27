package core

import "testing"

// host_suspend comes bound to no key: the toolkit does not decide which key
// stops a person's whole session. It is offered in the ordinary state, so a
// binding a user or a host adds takes effect without anything else changing.
func TestHostSuspendIsBoundToNothingButCanBeBound(t *testing.T) {
	r := DefaultKeyRegistry()
	if keys := r.KeysFor(CmdHostSuspend); len(keys) != 0 {
		t.Errorf("the default keymap binds host_suspend to %v; it should be unbound", keys)
	}
	if got := r.BuildStateContext(StateNormal).Resolve("^Z"); got == CmdHostSuspend {
		t.Error("^Z suspends by default")
	}

	r.Bind("^Z", CmdHostSuspend)
	if got := r.BuildStateContext(StateNormal).Resolve("^Z"); got != CmdHostSuspend {
		t.Errorf("^Z bound to host_suspend resolves to %q in the ordinary state", got)
	}
}
