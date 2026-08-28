package fsevents

/*
#include <CoreServices/CoreServices.h>
*/
import "C"

// A file of its own, because cgo forbids a preamble containing definitions in any
// file that also uses //export — and the preamble next door is most of the work.

//export snapshotterFSEvent
func snapshotterFSEvent(handle C.int, path *C.char, flags C.uint) {
	c := lookup(int(handle))
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Any of the three is the log admitting it did not keep up. What follows is
	// then an incomplete account of the range, and the only safe reading of an
	// incomplete account is that it proves nothing about what is missing.
	//
	// MustScanSubDirs is the bluntest: it means "something under here changed and
	// I did not record what". Treated the same as the two dropped flags, because
	// for this purpose they say the same thing.
	if flags&(C.kFSEventStreamEventFlagUserDropped|
		C.kFSEventStreamEventFlagKernelDropped|
		C.kFSEventStreamEventFlagMustScanSubDirs) != 0 {
		c.dropped = true
	}

	// The end of the replay. Everything after this would be live events, which is
	// a different question from "what happened while we were not looking".
	if flags&C.kFSEventStreamEventFlagHistoryDone != 0 {
		c.historyDone = true
		c.finish()
		return
	}

	if path != nil {
		c.paths = append(c.paths, C.GoString(path))
	}
}
