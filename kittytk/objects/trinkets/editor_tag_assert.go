//go:build mew

package trinkets

// The mew build tag selects the mew-backed editor, which is not in this
// repository: it ships with mew's own distribution, which carries a
// modified copy of KittyTK (docs/fork-sync-policy.md).
//
// Without this assertion the tag builds here and registers no editor type
// at all, since both halves of the vanilla pair are //go:build !mew and
// nothing replaces them. Nothing reports that at build time; it surfaces
// later, as `new editor` answering "unknown trinket type" on the running
// host.
//
// So the tag asserts what it depends on. mew's editor_mew.go declares this
// constant and this repository does not, so building here with -tags mew
// fails and names what is missing.
//
// A compile-time constant referenced once, so nothing reaches the binary.
var _ = mewEditorSuppliedByTheMewDistribution
