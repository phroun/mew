package config

import "testing"

// useLocks / useEmacsLocks parse from [general] and default to true;
// [storage] backups parses alongside scratch.
func TestLockAndBackupConfig(t *testing.T) {
	m := NewManager()
	c := m.LoadFromString("[general]\n")
	if !c.General.UseLocks || !c.General.UseEmacsLocks {
		t.Fatalf("locks should default on: %+v", c.General)
	}

	c = m.LoadFromString("[general]\nuseLocks=false\nuseEmacsLocks=false\n")
	if c.General.UseLocks || c.General.UseEmacsLocks {
		t.Fatalf("locks should parse off: %+v", c.General)
	}

	c = m.LoadFromString("[storage]\nbackups=/somewhere/backups\n")
	if c.Storage.Backups != "/somewhere/backups" {
		t.Fatalf("backups: %q", c.Storage.Backups)
	}

	c = m.LoadFromString("[storage]\ndeadcat=/var/crashdumps\n")
	if c.Storage.Deadcat != "/var/crashdumps" {
		t.Fatalf("deadcat: %q", c.Storage.Deadcat)
	}
}
