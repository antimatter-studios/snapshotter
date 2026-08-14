package scenario

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestParseSpanReadsTheUnitsAScenarioIsWrittenIn(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want time.Duration
	}{
		{"30s", 30 * time.Second},
		{"90m", 90 * time.Minute},
		{"6h", 6 * time.Hour},
		{"1h30m", 90 * time.Minute},
		// Days and weeks are the units a retention window is actually discussed
		// in, and time.ParseDuration has neither.
		{"3d", 72 * time.Hour},
		{"14d", 14 * 24 * time.Hour},
		{"2w", 14 * 24 * time.Hour},
		{"1.5d", 36 * time.Hour},
	} {
		got, err := ParseSpan(tc.in)
		if err != nil {
			t.Errorf("ParseSpan(%q): %v", tc.in, err)
			continue
		}
		if got.Duration() != tc.want {
			t.Errorf("ParseSpan(%q) = %s, want %s", tc.in, got.Duration(), tc.want)
		}
	}
}

// A span that is zero or in the future cannot describe a snapshot that exists,
// and a unitless number is the mistake somebody writing "6" for six hours makes.
// Each of these has to be refused where it was written rather than several steps
// later as a baffling listing.
func TestParseSpanRefusesWhatCannotBeAnAge(t *testing.T) {
	for _, in := range []string{"", "  ", "6", "0h", "-3h", "-1d", "soon", "1x", "d", "1.5"} {
		if got, err := ParseSpan(in); err == nil {
			t.Errorf("ParseSpan(%q) = %s, want an error", in, got)
		}
	}
}

// A span has to survive being written to a file and read back, or a scenario
// saved from a built-in would not be the same scenario.
func TestSpanRoundTripsThroughJSON(t *testing.T) {
	for _, want := range []time.Duration{
		30 * time.Second,
		90 * time.Minute,
		6 * time.Hour,
		55 * time.Hour,
		14 * 24 * time.Hour,
	} {
		data, err := json.Marshal(Span(want))
		if err != nil {
			t.Fatalf("marshalling %s: %v", want, err)
		}
		var got Span
		if err := json.Unmarshal(data, &got); err != nil {
			t.Fatalf("unmarshalling %s: %v", data, err)
		}
		if got.Duration() != want {
			t.Errorf("%s round-tripped as %s via %s", want, got.Duration(), data)
		}
	}
}

func TestSpanWritesTheLargestWholeUnit(t *testing.T) {
	for _, tc := range []struct {
		in   time.Duration
		want string
	}{
		{2 * 7 * 24 * time.Hour, "2w"},
		{3 * 24 * time.Hour, "3d"},
		{6 * time.Hour, "6h"},
		{90 * time.Minute, "90m"},
		{30 * time.Second, "30s"},
	} {
		if got := Span(tc.in).String(); got != tc.want {
			t.Errorf("Span(%s) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Every built-in has to be loadable and valid, because the only thing standing
// between a malformed built-in and a confusing run is this test: nothing else
// reads them until somebody sets the environment variable.
func TestEveryBuiltInIsValid(t *testing.T) {
	names := Names()
	if len(names) == 0 {
		t.Fatal("there are no built-in scenarios")
	}
	for _, name := range names {
		spec, err := Load(name)
		if err != nil {
			t.Fatalf("Load(%q): %v", name, err)
		}
		if spec.Name != name {
			t.Errorf("%s: the spec calls itself %q, so the sandbox and the banner would disagree with the selector", name, spec.Name)
		}
		if spec.Summary == "" {
			t.Errorf("%s: no summary, so the startup banner cannot say what it is for", name)
		}
		if err := spec.validate(); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestLoadNamesTheBuiltInsWhenAskedForOneThatDoesNotExist(t *testing.T) {
	_, err := Load("no-such-scenario")
	if err == nil {
		t.Fatal("an unknown scenario loaded")
	}
	// A typo is the likeliest way to get here, so the message has to carry the
	// list rather than sending the reader to the source.
	for _, name := range Names() {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("the error does not mention %s: %v", name, err)
		}
	}
}
