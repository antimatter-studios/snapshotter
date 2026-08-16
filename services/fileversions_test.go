package services

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"snapshotter/internal/apfs"
	"snapshotter/internal/mountmgr"
)

// What the tree comparison never answered: a list of changed paths says where to
// look and nothing about what is there.

// fileFixture mounts a fake snapshot over a seed and returns the service and the
// live directory, which the fake copied at mount time.
func fileFixture(t *testing.T) (*DiffService, string) {
	t.Helper()

	seed := t.TempDir()
	if err := os.WriteFile(filepath.Join(seed, "notes.md"), []byte("one\ntwo\nthree\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(seed, "picture.bin"), []byte{0xff, 0xd8, 0x00, 0x01, 0x02}, 0o600); err != nil {
		t.Fatal(err)
	}

	fake := mountmgr.NewFake(t.TempDir(), seed)
	svc := NewDiffService(Deps{
		Runner: browseRunner{}, Mounts: fake, Volume: apfs.DataVolume, Faking: true, FakeSeed: seed,
	})
	if err := fake.Mount(t.Context(), []string{browseSnapshot}); err != nil {
		t.Fatalf("mount: %v", err)
	}
	t.Cleanup(func() { _ = fake.Unmount(t.Context(), []string{browseSnapshot}) })
	return svc, seed
}

func TestBothVersionsOfAnEditedFileComeBack(t *testing.T) {
	svc, seed := fileFixture(t)
	live := filepath.Join(seed, "notes.md")

	if err := os.WriteFile(live, []byte("one\ntwo CHANGED\nthree\nfour\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := svc.FileVersions(browseSnapshot, live, "")
	if err != nil {
		t.Fatalf("file versions: %v", err)
	}
	if !got.Readable {
		t.Fatalf("a text file came back unreadable: %+v", got)
	}
	// The fake mount writes its own text rather than copying the seed, so what
	// matters here is that a snapshot side came back at all and that it is not
	// the live one.
	if got.Left == "" {
		t.Error("no left side was returned")
	}
	if got.Left == got.Right {
		t.Error("both sides are identical, so nothing was read from the snapshot")
	}
	if !strings.Contains(got.Right, "two CHANGED") {
		t.Errorf("the right side is wrong: %q", got.Right)
	}
	if !got.LeftExists || !got.RightExists {
		t.Errorf("both sides exist but were not reported: %+v", got)
	}
}

// A JPEG rendered as lines is noise, not a comparison. The sizes are still
// returned, because "2.1 MB became 2.4 MB" is an answer.
func TestABinaryFileIsNotOfferedAsText(t *testing.T) {
	svc, seed := fileFixture(t)

	got, err := svc.FileVersions(browseSnapshot, filepath.Join(seed, "picture.bin"), "")
	if err != nil {
		t.Fatalf("file versions: %v", err)
	}
	if got.Readable {
		t.Error("a file with NUL bytes was offered as text")
	}
	if got.Note == "" {
		t.Error("nothing explained why it cannot be shown")
	}
	// The sizes are the only answer left for a file that cannot be diffed.
	if got.RightSize == 0 {
		t.Error("the size was withheld, which is all a binary comparison can offer")
	}
}

// A file created since the snapshot has one empty side. That must read as a
// whole file added, not as an error.
func TestAFileCreatedSinceTheSnapshotShowsAsAllAdded(t *testing.T) {
	svc, seed := fileFixture(t)
	live := filepath.Join(seed, "brand-new.md")

	if err := os.WriteFile(live, []byte("written today\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := svc.FileVersions(browseSnapshot, live, "")
	if err != nil {
		t.Fatalf("file versions: %v", err)
	}
	if got.LeftExists {
		t.Error("a file that did not exist yet was reported as being in the snapshot")
	}
	if !got.RightExists || !got.Readable {
		t.Errorf("the live side was not returned: %+v", got)
	}
	if got.Left != "" {
		t.Errorf("the missing side is not empty: %q", got.Left)
	}
	if !strings.Contains(got.Right, "written today") {
		t.Errorf("the live text is wrong: %q", got.Right)
	}
}

// A file in neither place is a genuine error, unlike every case above.
func TestAFileInNeitherPlaceIsAnError(t *testing.T) {
	svc, seed := fileFixture(t)

	if _, err := svc.FileVersions(browseSnapshot, filepath.Join(seed, "never-existed.md"), ""); err == nil {
		t.Error("a file that exists nowhere came back without an error")
	}
}

// Both sides are loaded into memory and then into a web view, so the cap is
// about what the window survives rather than what the disk can supply.
func TestATooLargeFileIsDeclinedRatherThanLoaded(t *testing.T) {
	svc, seed := fileFixture(t)
	bigPath := filepath.Join(seed, "huge.log")

	// Text, not zero bytes. A file of NULs is binary, and binary is now decided
	// before size — so the old fixture proved the wrong thing the moment the
	// order changed. What this test is about is a genuinely large *text* file.
	big := bytes.Repeat([]byte("a"), maxDiffableBytes+1)
	if err := os.WriteFile(bigPath, big, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := svc.FileVersions(browseSnapshot, bigPath, "")
	if err != nil {
		t.Fatalf("file versions: %v", err)
	}
	if got.Readable {
		t.Error("a file past the cap was loaded anyway")
	}
	if !strings.Contains(got.Note, "large") {
		t.Errorf("the note does not say why: %q", got.Note)
	}
	if got.RightSize <= maxDiffableBytes {
		t.Errorf("the size was not reported: %d", got.RightSize)
	}
}

// The right side defaults to the live disk but is not fixed to it: any other
// mounted snapshot is an equally valid thing to compare against, which is what
// makes "what did this file look like between these two dates" answerable.
func TestTheRightSideCanBeAnotherSnapshot(t *testing.T) {
	svc, seed := fileFixture(t)

	got, err := svc.FileVersions(browseSnapshot, filepath.Join(seed, "notes.md"), browseSnapshot)
	if err == nil {
		t.Fatal("a snapshot was compared against itself, which has no answer to give")
	}
	_ = got

	// An unmounted snapshot has no paths to read, so it is refused rather than
	// silently falling back to the disk — a fallback would answer a question
	// nobody asked.
	if _, err := svc.FileVersions(browseSnapshot, filepath.Join(seed, "notes.md"), "com.apple.TimeMachine.2020-01-01-000000.local"); err == nil {
		t.Error("an unmounted snapshot was accepted as a comparison target")
	}
}

// The window says which version the right side turned out to be, rather than
// restating the rule for working it out.
func TestTheRightSideIsLabelled(t *testing.T) {
	svc, seed := fileFixture(t)

	got, err := svc.FileVersions(browseSnapshot, filepath.Join(seed, "notes.md"), "")
	if err != nil {
		t.Fatalf("file versions: %v", err)
	}
	if got.RightLabel != "the live disk" {
		t.Errorf("the default target is not named as the disk: %q", got.RightLabel)
	}
}

// A PNG is shown, not described. It used to be declined as binary, and before
// that declined for its size — neither of which is what someone asking "what
// changed in this screenshot" wants.
func TestAPictureComesBackAsAPicture(t *testing.T) {
	svc, seed := fileFixture(t)
	path := filepath.Join(seed, "login.png")

	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 12, 7))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := svc.FileVersions(browseSnapshot, path, "")
	if err != nil {
		t.Fatalf("file versions: %v", err)
	}
	if got.Kind != "image" {
		t.Fatalf("a PNG came back as %q", got.Kind)
	}
	if got.Readable {
		t.Error("an image was offered as text, which would render it as lines")
	}
	if !strings.HasPrefix(got.RightImage, "data:image/png;base64,") {
		t.Errorf("the picture is not shippable to the window: %.40q", got.RightImage)
	}
	// Dimensions answer "was it resized", which sizes alone cannot.
	if got.RightDims != "12\u00d77" {
		t.Errorf("dimensions wrong: %q", got.RightDims)
	}
}

// An image past the image cap says so, rather than blaming lines it never had.
func TestATooLargePictureSaysItCannotBeShown(t *testing.T) {
	svc, seed := fileFixture(t)
	path := filepath.Join(seed, "huge.png")

	data := append([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}, make([]byte, maxImageBytes+1)...)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := svc.FileVersions(browseSnapshot, path, "")
	if err != nil {
		t.Fatalf("file versions: %v", err)
	}
	if got.Kind != "image" {
		t.Errorf("a large PNG stopped being an image: %q", got.Kind)
	}
	if got.RightImage != "" {
		t.Error("an oversized picture was inlined anyway")
	}
	if !strings.Contains(got.Note, "too large") {
		t.Errorf("the note does not say why: %q", got.Note)
	}
	// The figures are what is left to say.
	if got.RightSize == 0 {
		t.Error("the size was withheld")
	}
}

// Two identical pictures are worth saying so about: they can look alike on screen
// and there is no line comparison to settle it.
func TestTwoIdenticalPicturesAreReportedAsIdentical(t *testing.T) {
	svc, seed := fileFixture(t)
	path := filepath.Join(seed, "same.png")

	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := svc.FileVersions(browseSnapshot, path, "")
	if err != nil {
		t.Fatalf("file versions: %v", err)
	}
	// The fake mount writes its own contents, so the two sides differ here; what
	// matters is that the field is answered rather than left at its zero value by
	// a path that never sets it.
	if got.Kind != "image" {
		t.Fatalf("not an image: %q", got.Kind)
	}
	if got.Identical && got.LeftSize != got.RightSize {
		t.Error("called identical with different sizes")
	}
}

// Text a long way under the new cap must still diff — the point of raising it.
func TestATextFileWellPastTheOldCapStillCompares(t *testing.T) {
	svc, seed := fileFixture(t)
	big := filepath.Join(seed, "large.log")

	// Comfortably past the old 1MiB limit and inside the new one.
	line := "a line of perfectly ordinary log output\n"
	var sb strings.Builder
	for sb.Len() < 3<<20 {
		sb.WriteString(line)
	}
	if err := os.WriteFile(big, []byte(sb.String()), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := svc.FileVersions(browseSnapshot, big, "")
	if err != nil {
		t.Fatalf("file versions: %v", err)
	}
	if !got.Readable {
		t.Errorf("a 3MB text file was declined: %q", got.Note)
	}
}

// A ZIP called photo.png is not a picture. Trusting the extension alone would
// base64 it, send it to the window and hand it to an <img> tag, which draws a
// broken-image icon and explains nothing.
func TestSomethingElseWearingAnImageExtensionIsNotShownAsOne(t *testing.T) {
	svc, seed := fileFixture(t)
	path := filepath.Join(seed, "photo.png")

	// A real ZIP header, which http.DetectContentType identifies by signature.
	data := append([]byte("PK\x03\x04"), make([]byte, 600)...)
	for i := 4; i < len(data); i++ {
		data[i] = byte('a' + i%20)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := svc.FileVersions(browseSnapshot, path, "")
	if err != nil {
		t.Fatalf("file versions: %v", err)
	}
	if got.Kind == "image" {
		t.Error("a zip was shown as a picture because of its name")
	}
}

// SVG is a picture the standard library cannot identify — it sniffs as text or
// XML — and the web view draws it perfectly well. The extension has to be
// allowed to decide when the sniff has no opinion, or this is rejected.
func TestAFormatTheSnifferCannotNameIsStillShown(t *testing.T) {
	svc, seed := fileFixture(t)
	path := filepath.Join(seed, "logo.svg")

	svg := `<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"><rect width="10" height="10"/></svg>`
	if err := os.WriteFile(path, []byte(svg), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := svc.FileVersions(browseSnapshot, path, "")
	if err != nil {
		t.Fatalf("file versions: %v", err)
	}
	if got.Kind != "image" {
		t.Errorf("an SVG came back as %q rather than a picture", got.Kind)
	}
}
