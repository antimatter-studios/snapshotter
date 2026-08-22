package mountmgr

import (
	"strings"
	"testing"
)

// The window recognises a Full Disk Access refusal by looking for a phrase in
// this message, and it has to: a bound method's error crosses into JavaScript as
// a string, so there is no type left to switch on by the time the window sees it.
//
// That makes the wording load-bearing. Recognising the refusal is what turns a
// dead end into a banner with instructions and a button to the settings pane, and
// losing it puts the reader back in front of "Operation not permitted" with
// nothing to do about it. This is the assertion that makes rewording the message
// fail here rather than silently there.
//
// The matching constant is refusalMarker in frontend/src/App.tsx.
const windowLooksFor = "Full Disk Access"

func TestTheRefusalStillSaysWhatTheWindowLooksFor(t *testing.T) {
	if !strings.Contains(ErrNeedsFullDiskAccess.Error(), windowLooksFor) {
		t.Errorf("the message no longer contains %q, so the window will not recognise it:\n%v",
			windowLooksFor, ErrNeedsFullDiskAccess)
	}
}

// And in English, whatever the process language is. The window's catalogue held a
// translated copy of this phrase for a while and compared it against this
// message, which meant the check never matched for anyone not using English.
func TestTheRefusalIsNotTranslated(t *testing.T) {
	// A permission macOS itself names only in English. Translating this side would
	// break the match again, in the same silent way.
	for _, word := range []string{"macOS", "Full Disk Access", "mount_apfs"} {
		if !strings.Contains(ErrNeedsFullDiskAccess.Error(), word) {
			t.Errorf("the message no longer names %q", word)
		}
	}
}

func TestARefusalIsRecognisedHoweverMountApfsWordsIt(t *testing.T) {
	// The errno mount_apfs prints is not the one that matches its own message, so
	// the text is what gets matched. Both wordings have been seen in the wild.
	for _, output := range []string{
		"mount_apfs: volume could not be mounted: Operation not permitted",
		"mount_apfs: volume could not be mounted: Permission denied",
		"MOUNT_APFS: OPERATION NOT PERMITTED",
	} {
		if err := classifyMount(output, errFake{}); err == nil || !strings.Contains(err.Error(), windowLooksFor) {
			t.Errorf("%q was not recognised as a refusal: %v", output, err)
		}
	}
}

func TestAnUnfamiliarFailureIsPassedThroughUnchanged(t *testing.T) {
	// Guessing at an unfamiliar failure is worse than reporting it: a disk that is
	// full, or a snapshot that has been deleted, would send the reader to a
	// permissions pane that has nothing to do with it.
	original := errFake{}
	got := classifyMount("mount_apfs: no such snapshot", original)
	if got != error(original) {
		t.Errorf("an unfamiliar failure was reinterpreted as %v", got)
	}
}

type errFake struct{}

func (errFake) Error() string { return "exit status 1" }
