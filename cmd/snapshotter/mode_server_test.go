//go:build server

package main

import "testing"

// The tagged build serves, and says so before anything asks how it was
// addressed.
//
// This is the assertion whose absence let `task server` print the help text for
// two releases: `-tags server` reached only the Wails library, so nothing in
// this package could tell the two builds apart and nothing tested that it could.
func TestTheServerBuildSaysItServesOverHTTP(t *testing.T) {
	if !servesOverHTTP {
		t.Fatal("built with -tags server and still claiming to open a window, so the bare invocation will print help instead of serving")
	}
}
