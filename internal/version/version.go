// Package version reports which build of Snapshotter is running.
//
// One value, read by everything that has to answer the question: the `version`
// command, the interface's home screen, and the Info.plist stamped into the
// bundle. They agreed by accident before this existed, and stopped agreeing the
// moment a release was cut — v0.1.1 shipped a bundle whose Info.plist still said
// 0.1.0, because the plist was a committed constant that nothing updated.
package version

import "runtime/debug"

// Version is stamped at build time:
//
//	-ldflags "-X snapshotter/internal/version.Version=0.2.0"
//
// It is deliberately not a constant, and deliberately not read from a file the
// application ships: a value inside the bundle can disagree with the tag it was
// built from, which is exactly the failure this package exists to prevent.
var Version = ""

// devVersion is what an unstamped build reports. It has to be obviously not a
// release rather than a plausible number, because the one place this shows up is
// a bug report saying which version misbehaved.
const devVersion = "dev"

// String is the version to show a person.
//
// An unstamped build falls back to whatever the Go toolchain recorded — `go
// install` of a tagged module knows its own version even though no ldflag was
// passed — and to "dev" when even that is absent, which is the ordinary case for
// a working copy.
func String() string {
	if Version != "" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return devVersion
}

// IsRelease reports whether this build came from a version tag, which is the
// question worth asking before treating the version as meaningful.
func IsRelease() bool { return Version != "" }
