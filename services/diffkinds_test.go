package services

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// Kind is the field the window reads to decide how to show a comparison, and
// Readable is the older field saying whether there are lines. Two fields carrying
// one fact is a standing invitation for them to disagree, and the window branches
// on both — so this pins every value one can take, and that they agree.
//
// It exists because they had already drifted: a file found to be binary at the
// sniff sample said "binary", and one found past it said nothing at all. Same
// outcome, two answers, and anything branching on the field would have been wrong
// for two of the three ways of reaching it.

func versionsOf(t *testing.T, svc *DiffService, live string) FileVersions {
	t.Helper()

	got, err := svc.FileVersions(browseSnapshot, live, "")
	if err != nil {
		t.Fatalf("file versions for %s: %v", live, err)
	}
	return got
}

func TestEveryKindIsNamedAndAgreesWithReadable(t *testing.T) {
	// Past the sniff sample, so the NUL is only found by the full read. This is the
	// case that used to come back unnamed.
	lateBinary := append(bytes.Repeat([]byte("a"), binarySniffBytes+1024), 0x00, 'b')

	svc, seed := compareFixture(t, map[string]string{
		"readable.txt": "one\ntwo\nthree\n",
		"straight.bin": "head\x00tail",
		"slow.bin":     string(lateBinary),
		"snapshot.png": string(onePixelPNG(t)),
	})

	for name, want := range map[string]string{
		// A picture is two pictures, whatever its bytes say about being binary.
		"snapshot.png": "image",
		"readable.txt": "text",
		"straight.bin": "binary",
		"slow.bin":     "binary",
	} {
		got := versionsOf(t, svc, filepath.Join(seed, name))
		if got.Kind != want {
			t.Errorf("%s: kind %q, want %q", name, got.Kind, want)
		}
		// The invariant. Readable means "there are lines in Left and Right", which
		// is true of text and of nothing else.
		if got.Readable != (got.Kind == "text") {
			t.Errorf("%s: kind %q with readable %v", name, got.Kind, got.Readable)
		}
	}
}

func TestAFileInNeitherVersionIsNamedAbsent(t *testing.T) {
	svc, seed := compareFixture(t, nil)

	got := versionsOf(t, svc, filepath.Join(seed, "never-existed.txt"))

	// Reachable by ordinary use, and not an error: there is nothing wrong with
	// asking a question that has no answer.
	if got.Kind != "absent" {
		t.Errorf("kind %q, want absent", got.Kind)
	}
	if got.Readable {
		t.Error("a file in neither version came back readable")
	}
	if got.Note == "" {
		t.Error("nothing was said about why there is nothing to show")
	}
}

func TestTextPastTheSizeWorthRenderingIsNamedLarge(t *testing.T) {
	// Just over the limit, and text throughout, so it passes the binary gate and
	// stops at the size one. Written with a repeated line rather than random bytes
	// so the fake's clone stays cheap.
	big := bytes.Repeat([]byte("this is a line of an enormous log file\n"), (maxDiffableBytes/38)+64)
	svc, seed := compareFixture(t, map[string]string{"enormous.log": string(big)})

	got := versionsOf(t, svc, filepath.Join(seed, "enormous.log"))

	if got.Kind != "large" {
		t.Errorf("kind %q, want large", got.Kind)
	}
	if got.Readable {
		t.Error("a file too large to render came back readable")
	}
	// And it says which of the two reasons it is. Saying "too large" about a
	// picture, or "binary" about a log, sends the reader looking for the wrong
	// thing — a smaller version of a file that would never diff either way.
	if got.Note == "" {
		t.Error("nothing was said about why there are no lines")
	}
}

// onePixelPNG is the smallest thing the image path will accept.
func onePixelPNG(t *testing.T) []byte {
	t.Helper()

	// Written out rather than generated, so the test does not depend on the same
	// encoder the code under test uses to decide this is a picture.
	png := []byte{
		0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a,
		0x00, 0x00, 0x00, 0x0d, 'I', 'H', 'D', 'R',
		0x00, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01,
		0x08, 0x06, 0x00, 0x00, 0x00, 0x1f, 0x15, 0xc4,
		0x89, 0x00, 0x00, 0x00, 0x0a, 'I', 'D', 'A', 'T',
		0x78, 0x9c, 0x63, 0x00, 0x01, 0x00, 0x00, 0x05,
		0x00, 0x01, 0x0d, 0x0a, 0x2d, 0xb4, 0x00, 0x00,
		0x00, 0x00, 'I', 'E', 'N', 'D', 0xae, 0x42, 0x60, 0x82,
	}
	// Proof that it is one: if this stops decoding, every image assertion below
	// would quietly become an assertion about a corrupt file.
	dir := t.TempDir()
	path := filepath.Join(dir, "probe.png")
	if err := os.WriteFile(path, png, 0o600); err != nil {
		t.Fatal(err)
	}
	if imageMediaType(path, true) != "image/png" {
		t.Fatal("the test's own PNG is not recognised as one")
	}
	return png
}
