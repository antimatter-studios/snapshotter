//go:build !server

package main

import "testing"

// The ordinary build opens a window, and the two guards that assume one must
// therefore still run.
//
// Asserted rather than assumed, because the constant decides both: a build that
// wrongly claimed to serve over HTTP would skip the instance lock and stop
// printing help at a bare invocation, and neither of those failures announces
// itself.
func TestTheOrdinaryBuildDoesNotServeOverHTTP(t *testing.T) {
	if servesOverHTTP {
		t.Fatal("the untagged build claims to serve over HTTP, so the window's guards are being skipped")
	}
}
