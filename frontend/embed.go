// Package frontend carries the built window into the binary.
//
// It exists as a package of its own for one mechanical reason: go:embed cannot
// reach outside the directory holding the file that declares it, so whatever
// embeds frontend/dist has to live here. It used to be main.go, which is why the
// program's entry point sat in the repository root; with this here, it does not
// have to.
package frontend

import "embed"

// Assets is the built window — the output of `npm run build`, not the sources
// beside it.
//
// "all:" so that files beginning with a dot or an underscore are included, which
// Vite emits and which the default pattern would silently drop.
//
//go:embed all:dist
var Assets embed.FS
