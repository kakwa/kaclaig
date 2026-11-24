package main

import "testing"

func TestVersionStringOverride(t *testing.T) {
	old := version
	t.Cleanup(func() { version = old })

	version = "v9.9.9"
	if got := versionString(); got != "v9.9.9" {
		t.Errorf("versionString() = %q, want %q", got, "v9.9.9")
	}
}

func TestVersionStringFallback(t *testing.T) {
	old := version
	t.Cleanup(func() { version = old })

	version = ""
	got := versionString()
	if got == "" || got == "(devel)" {
		t.Errorf("versionString() = %q, want a usable version", got)
	}
}
