//go:build !server

package main

// servesOverHTTP is false in the ordinary build, which opens a window.
//
// See mode_server.go for what this exists to tell apart and why the difference
// cannot be worked out at runtime.
const servesOverHTTP = false
