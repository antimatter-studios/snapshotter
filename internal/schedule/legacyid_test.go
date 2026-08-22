package schedule

import (
	"testing"
	"time"
)

// Identifiers written by an earlier version have to keep working, because the
// settings file is what the startup reconciliation reads as the intent.
//
// This is the test that was missing. The presets were renamed when their reach
// stopped being fixed — correctly, since a name with a number in it is wrong for
// four of the five windows once the reach follows the window — but a settings file
// saying "tiered-52-weeks" then resolved to nothing. The consequence was not a
// blank dropdown: an upgrade removes both launchd agents, the application puts
// them back from this file, and an identifier it could not resolve meant it put
// nothing back. The rename took the protection off the machine.

func TestAnIdentifierFromAnEarlierVersionStillResolves(t *testing.T) {
	for old, want := range map[string]string{
		// Matched by what each shape did, not by its name: tiered-13-weeks kept
		// everything for two days, then daily for a fortnight, then weekly out to
		// thirteen weeks — the daily-then-weekly shape. tiered-52-weeks added a
		// monthly band reaching a year.
		"tiered-13-weeks": "tiered-daily-weekly",
		"tiered-52-weeks": "tiered-weekly-monthly",
	} {
		if got := CanonicalPolicyID(old); got != want {
			t.Errorf("%s maps to %q, want %q", old, got, want)
		}

		policy, ok := PolicyByID(old, 3*time.Hour, 14*day)
		if !ok {
			t.Errorf("%s does not resolve to a policy, so a settings file naming it restores nothing", old)
			continue
		}
		// And to the same policy the current name gives, or the two names would
		// describe different schedules.
		current, _ := PolicyByID(want, 3*time.Hour, 14*day)
		if !policy.Equal(current) {
			t.Errorf("%s resolves to a different policy than %s", old, want)
		}
	}
}

func TestACurrentIdentifierIsLeftAlone(t *testing.T) {
	for _, id := range []string{FlatID, "tiered-daily-weekly", "tiered-weekly-monthly", "custom"} {
		if got := CanonicalPolicyID(id); got != id {
			t.Errorf("%s was rewritten to %q", id, got)
		}
	}
}

func TestAnIdentifierNobodyKnowsIsPassedThroughRatherThanGuessedAt(t *testing.T) {
	// A settings file edited by hand, or written by a version newer than this one.
	// Returning it unchanged lets the caller say it does not resolve; inventing a
	// mapping would install a schedule nobody chose.
	const invented = "tiered-every-third-tuesday"
	if got := CanonicalPolicyID(invented); got != invented {
		t.Errorf("an unknown identifier became %q", got)
	}
	if _, ok := PolicyByID(invented, 3*time.Hour, 14*day); ok {
		t.Error("an unknown identifier resolved to a policy anyway")
	}
}

// Every identifier this build writes must be one it can read back. A preset
// renamed again without an entry here would break the same way.
func TestEveryIdentifierThisBuildWritesCanBeReadBack(t *testing.T) {
	period, window := 3*time.Hour, 14*day

	ids := []string{FlatID}
	for _, p := range Presets(period, window) {
		ids = append(ids, p.ID)
	}
	for _, id := range ids {
		if _, ok := PolicyByID(id, period, window); !ok {
			t.Errorf("this build offers %q and cannot resolve it", id)
		}
	}

	// And what IdentifyPolicy names a policy must resolve back to that policy,
	// since that is the round trip the settings file makes.
	for _, p := range Presets(period, window) {
		name := IdentifyPolicy(p.Policy)
		back, ok := PolicyByID(name, period, window)
		if !ok {
			t.Errorf("%s was identified as %q, which does not resolve", p.ID, name)
			continue
		}
		if !back.Equal(p.Policy) {
			t.Errorf("%s round-tripped through %q into a different policy", p.ID, name)
		}
	}
}
