package main

import "runtime/debug"

// version can be set at build time with -ldflags "-X main.version=v1.2.3".
// When it is empty, the module version recorded by the Go toolchain is used.
var version = ""

func versionString() string {
	v := version
	if v == "" {
		if info, ok := debug.ReadBuildInfo(); ok {
			v = info.Main.Version
		}
	}
	if v == "" || v == "(devel)" {
		return "dev"
	}
	return v
}
