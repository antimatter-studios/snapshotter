package apfs

import (
	"context"
	"os"
	"testing"
	"time"
)

// The integration tests talk to the real tmutil. They are guarded by an
// environment variable because they change the machine's state: one snapshot is
// created and then deleted again.
//
// They only ever delete the snapshot they created themselves. Nothing here
// prunes by age, because that would destroy restore points belonging to the
// person running the tests.
func requireIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv("SNAPSHOTTER_INTEGRATION") != "1" {
		t.Skip("set SNAPSHOTTER_INTEGRATION=1 to run tests against the real tmutil")
	}
}

func TestIntegrationListParsesRealOutput(t *testing.T) {
	requireIntegration(t)

	snaps, err := List(context.Background(), SystemRunner(), DataVolume)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range snaps {
		if s.Stamp == "" || s.Taken.IsZero() {
			t.Errorf("snapshot parsed with empty fields: %+v", s)
		}
		if s.Taken.After(time.Now().Add(time.Minute)) {
			t.Errorf("snapshot dated in the future: %+v", s)
		}
	}
	t.Logf("parsed %d snapshots from the real volume", len(snaps))
}

func TestIntegrationCreateThenDeleteItsOwnSnapshot(t *testing.T) {
	requireIntegration(t)
	ctx := context.Background()
	runner := SystemRunner()

	created, err := Create(ctx, runner)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("created %s", created.Stamp)

	if !contains(t, ctx, runner, created.Stamp) {
		t.Fatalf("the snapshot just created is missing from the listing: %s", created.Stamp)
	}

	if err := Delete(ctx, runner, created.Stamp); err != nil {
		t.Fatalf("deleting the snapshot this test created: %v", err)
	}
	if contains(t, ctx, runner, created.Stamp) {
		t.Errorf("%s survived deletion", created.Stamp)
	}
}

func TestIntegrationDestinationInfoAnswers(t *testing.T) {
	requireIntegration(t)

	tm := DestinationInfo(context.Background(), SystemRunner())
	if tm.Detail == "" {
		t.Error("tmutil said nothing about destinations")
	}
	t.Logf("destination configured: %v (%s)", tm.HasDestination, tm.Detail)
}

func contains(t *testing.T, ctx context.Context, r Runner, stamp string) bool {
	t.Helper()
	snaps, err := List(ctx, r, DataVolume)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range snaps {
		if s.Stamp == stamp {
			return true
		}
	}
	return false
}
