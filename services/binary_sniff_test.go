package services

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// Whether a file is text, decided from its opening bytes.
//
// This gate runs before the size limit, which is the order that matters: a
// sixteen-megabyte log is worth reading and a sixteen-megabyte disk image is not,
// and asking about the size first meant a large binary was refused for being
// large — which reads as a limit someone could raise.

func sniff(t *testing.T, name string, body []byte) bool {
	t.Helper()

	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	return looksBinary(path, true)
}

func TestTextIsNotBinary(t *testing.T) {
	for name, body := range map[string]string{
		"plain ASCII":    "one\ntwo\nthree\n",
		"accented Latin": "Grüße aus München\n",
		"a language that needs three bytes a character": "日本語のテキスト\n",
		"an emoji, which needs four":                    "a snapshot 📸 was taken\n",
		"a lone newline":                                "\n",
	} {
		if sniff(t, "notes.txt", []byte(body)) {
			t.Errorf("%s was called binary", name)
		}
	}
}

func TestAFileWithNoBytesAtAllIsNotBinary(t *testing.T) {
	// An empty file compares as an empty side, which is a real and readable
	// answer. Calling it binary would refuse to show a file that has nothing to
	// show, and say the wrong reason.
	if sniff(t, "empty.txt", nil) {
		t.Error("an empty file was called binary")
	}
}

func TestANulByteMeansBinary(t *testing.T) {
	// The oldest and still the most reliable sign. Rendering a JPEG as lines
	// produces noise, not a comparison.
	if !sniff(t, "picture.jpg", []byte{0xff, 0xd8, 0xff, 0xe0, 0x00, 0x10, 'J', 'F', 'I', 'F'}) {
		t.Error("a JPEG header was called text")
	}
}

func TestInvalidUTF8MeansBinary(t *testing.T) {
	// No NUL, and still not text: a lone continuation byte cannot begin a
	// character in any encoding this compares.
	if !sniff(t, "thing.dat", []byte{'a', 'b', 0x80, 0x81, 'c'}) {
		t.Error("an invalid sequence was called text")
	}
}

// The subtle one. Only the first 8 KB are read, so a multi-byte character
// straddling that boundary arrives half-present — and a naive check would call a
// perfectly good Japanese file binary for a reason that has nothing to do with
// its contents. Which half of the character is cut off depends on where the text
// happens to sit, so every offset is tried.
func TestACharacterCutAtTheSampleBoundaryIsStillText(t *testing.T) {
	for shift := range 4 {
		body := bytes.Repeat([]byte("a"), binarySniffBytes-2+shift)
		// Four bytes, so at some shift the sample ends inside it.
		body = append(body, []byte("𝄞")...)
		body = append(body, bytes.Repeat([]byte("b"), 100)...)

		if sniff(t, "score.txt", body) {
			t.Errorf("a character cut %d bytes into the sample made the file binary", shift)
		}
	}
}

// The opposite case, and the reason the trimming has to stop rather than eat the
// whole sample: a file that really is binary must not be trimmed until whatever
// is left happens to be valid.
func TestTrimmingDoesNotTurnABinaryFileIntoText(t *testing.T) {
	body := append(bytes.Repeat([]byte{0x80}, 64), []byte("text after the noise\n")...)

	if !sniff(t, "noise.dat", body) {
		t.Error("a file of invalid bytes was trimmed into looking like text")
	}
}

func TestASideThatDoesNotExistIsNotBinary(t *testing.T) {
	// A created or deleted file has one empty side, and that must still compare as
	// a whole side added or removed rather than as a file nobody can look at.
	if looksBinary("/nowhere/at/all", false) {
		t.Error("an absent side was called binary")
	}
}

func TestAFileThatCannotBeOpenedIsNotBinary(t *testing.T) {
	// Unreadable is not the same as binary. The full read that follows produces
	// the honest error — "permission denied" — rather than this guessing at one and
	// reporting a file as an image.
	dir := t.TempDir()
	path := filepath.Join(dir, "sealed.txt")
	if err := os.WriteFile(path, []byte("text\n"), 0o000); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		t.Skip("root can read a file with no permission bits, so there is nothing to test")
	}

	if looksBinary(path, true) {
		t.Error("an unreadable file was called binary")
	}
}

func TestReadableFileGivesBackTheTextAndRefusesTheRest(t *testing.T) {
	dir := t.TempDir()
	text := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(text, []byte("one\ntwo\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "picture.bin")
	if err := os.WriteFile(binary, []byte{'a', 0x00, 'b'}, 0o600); err != nil {
		t.Fatal(err)
	}

	if body, ok := readableFile(text, true); !ok || body != "one\ntwo\n" {
		t.Errorf("text came back as (%q, %v)", body, ok)
	}
	if body, ok := readableFile(binary, true); ok || body != "" {
		t.Errorf("a binary file came back as (%q, %v)", body, ok)
	}
	// An absent side reads as empty and readable, which is what makes a deleted
	// file show as a whole side removed.
	if body, ok := readableFile(filepath.Join(dir, "never"), false); !ok || body != "" {
		t.Errorf("an absent side came back as (%q, %v)", body, ok)
	}
	if _, ok := readableFile(filepath.Join(dir, "never"), true); ok {
		t.Error("a file that is claimed to exist but does not came back readable")
	}
}

func TestTheSniffOnlyReadsItsSample(t *testing.T) {
	// The point of sniffing rather than reading: the answer must not depend on the
	// rest of the file, however large it is. A NUL well past the sample is not
	// found, and that is deliberate — the full read that follows catches it.
	body := append(bytes.Repeat([]byte("a"), binarySniffBytes+1024), 0x00)

	if sniff(t, "long.txt", body) {
		t.Error("the sniff read past its sample")
	}
	// And the full read does catch it, so nothing is rendered as lines that
	// should not be.
	path := filepath.Join(t.TempDir(), "long.txt")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := readableFile(path, true); ok {
		t.Error("a file with a NUL in it was read as text")
	}
}
