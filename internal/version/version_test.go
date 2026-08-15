package version

import "testing"

// The version is the first thing asked of any bug report, so the failure that
// matters is a build reporting something plausible-but-wrong rather than
// admitting it does not know.

func TestAnUnstampedBuildSaysDevRatherThanAPlausibleNumber(t *testing.T) {
	original := Version
	t.Cleanup(func() { Version = original })

	Version = ""
	got := String()
	// Under `go test` the build info carries no module version, so this is the
	// unstamped path. Whatever it returns must not look like a release.
	if got == "" {
		t.Error("returned nothing at all")
	}
	if got != devVersion && got[0] >= '0' && got[0] <= '9' {
		t.Errorf("an unstamped build reported %q, which reads as a real version", got)
	}
	if IsRelease() {
		t.Error("an unstamped build claims to be a release")
	}
}

func TestAStampedBuildReportsExactlyWhatItWasGiven(t *testing.T) {
	original := Version
	t.Cleanup(func() { Version = original })

	for _, want := range []string{"0.2.0", "1.0.0-rc.1", "0.0.1"} {
		Version = want
		if got := String(); got != want {
			t.Errorf("stamped %q, reported %q", want, got)
		}
		if !IsRelease() {
			t.Errorf("stamped %q but IsRelease() is false", want)
		}
	}
}
