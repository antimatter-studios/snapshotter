//go:build server

package main

// servesOverHTTP reports that this binary was built to serve the interface over
// HTTP rather than to open a window.
//
// It is the build tag made visible to this package. `-tags server` used to reach
// only the Wails library, which is what decides whether application.Run opens a
// WKWebView or listens on a port — so every line before that decision was
// identical in both builds, including the two guards that assume a window.
//
// Both of them then refused a server. The bare-invocation check reads argv[0]
// for a bundle, and a server binary lives at bin/snapshotter-server, so it
// looked like somebody typing the name at a prompt and got the help text. The
// instance lock refuses a second window, and a headless server has none to be
// second — but it asked for the lock anyway and was turned away by whatever
// window happened to be open.
//
// The fix is to consult the thing that already knew. A build with this tag knows
// at compile time what it is for, and does not have to infer it from how it was
// addressed.
const servesOverHTTP = true
