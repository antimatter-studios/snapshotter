// Package fsevents reads what macOS already remembers about a volume's history.
//
// # What this is for, and what it can never be
//
// Deciding a folder has changed needs one difference. Deciding it has NOT changed
// needs everything under it read, because there is no early exit from proving a
// negative — and on an SD card that is seconds per folder.
//
// macOS keeps a per-volume log of what changed, in /.fseventsd, and will replay
// it from a point in the past. That log is the cheapest possible source of
// candidate paths: instead of walking a tree looking for one difference, ask the
// system where the differences were and check those.
//
// It is a HINT AND ONLY A HINT. The log is best-effort by design — the API has
// three separate flags for "events were dropped" (kFSEventStreamEventFlagUserDropped,
// kFSEventStreamEventFlagKernelDropped, kFSEventStreamEventFlagMustScanSubDirs)
// and macOS is entitled to use them. Worse, /.fseventsd on a removable volume is
// owned by whoever mounted it: on the machine this was written for the SD card's
// log is christhomas:staff, so its own user can edit or delete it, while the
// startup disk's is root:admin.
//
// So nothing here may ever be used to conclude that something did NOT change.
// Every path it produces is verified against the disk before it is believed, and
// a volume whose log says nothing is a volume this package knows nothing about —
// not a volume that is unchanged.
//
// # Why an event id and not a hash
//
// The obvious way to tell whether the log has moved is to hash it. The API
// already offers something strictly better: event ids are a monotonic counter, so
// "has anything happened" is an integer comparison rather than a read of the
// whole structure, and the id doubles as the cursor for replaying exactly what
// happened since. A hash would answer half the question at more cost.
//
// UUID is the other half. It identifies the log itself, so a log that has been
// wiped and started again — which is a thing a person can do to their own SD card
// — is detectable, and every id recorded against the old one can be thrown away.
package fsevents

/*
#cgo LDFLAGS: -framework CoreServices
#include <CoreServices/CoreServices.h>
#include <sys/stat.h>
#include <stdlib.h>
#include <dispatch/dispatch.h>

// deviceFor returns the device number a path lives on, which is what the
// FSEvents device-relative calls are addressed with.
static int deviceFor(const char *path, dev_t *out) {
    struct stat st;
    if (stat(path, &st) != 0) {
        return -1;
    }
    *out = st.st_dev;
    return 0;
}

// uuidFor copies the volume's event-log identity into a buffer as a string.
// Returns 0 on success, -1 when the volume has no log this process can read.
static int uuidFor(dev_t dev, char *buf, int len) {
    CFUUIDRef uuid = FSEventsCopyUUIDForDevice(dev);
    if (uuid == NULL) {
        return -1;
    }
    CFStringRef s = CFUUIDCreateString(NULL, uuid);
    int ok = CFStringGetCString(s, buf, len, kCFStringEncodingUTF8) ? 0 : -1;
    CFRelease(s);
    CFRelease(uuid);
    return ok;
}

extern void snapshotterFSEvent(int handle, char *path, unsigned int flags);

// replayCallback hands each event back to Go one path at a time. The stream is
// created with kFSEventStreamCreateFlagFileEvents, so paths are individual files
// rather than the directories containing them.
static void replayCallback(ConstFSEventStreamRef stream, void *info, size_t count,
                           void *paths, const FSEventStreamEventFlags flags[],
                           const FSEventStreamEventId ids[]) {
    char **names = (char **)paths;
    long handle = (long)info;
    for (size_t i = 0; i < count; i++) {
        snapshotterFSEvent((int)handle, names[i], (unsigned int)flags[i]);
    }
}

// A running replay: the stream and the queue its callbacks arrive on.
typedef struct {
    FSEventStreamRef stream;
    dispatch_queue_t queue;
} replay_t;

// startReplay begins the historical replay and returns immediately.
//
// A dispatch queue rather than a run loop. FSEventStreamScheduleWithRunLoop was
// deprecated in macOS 13, and the queue is the simpler shape anyway: there is no
// loop to own, no thread to lock, and Go can wait on a channel the callback
// closes rather than on a loop it has to stop from the inside.
static replay_t *startReplay(dev_t dev, unsigned long long since, long handle) {
    FSEventStreamContext ctx = {0, (void *)handle, NULL, NULL, NULL};
    // Relative to the device, so "/" is the volume's own root rather than the
    // startup disk's. That is the whole reason for using the device-relative
    // call: a mount point can move, and the history belongs to the volume.
    CFStringRef root = CFSTR("/");
    CFArrayRef paths = CFArrayCreate(NULL, (const void **)&root, 1, &kCFTypeArrayCallBacks);
    FSEventStreamRef stream = FSEventStreamCreateRelativeToDevice(
        NULL, replayCallback, &ctx, dev, paths, (FSEventStreamEventId)since,
        0.0,
        kFSEventStreamCreateFlagNoDefer | kFSEventStreamCreateFlagFileEvents);
    CFRelease(paths);
    if (stream == NULL) {
        return NULL;
    }

    replay_t *r = (replay_t *)malloc(sizeof(replay_t));
    r->stream = stream;
    r->queue = dispatch_queue_create("com.christhomas.snapshotter.fsevents", DISPATCH_QUEUE_SERIAL);
    FSEventStreamSetDispatchQueue(stream, r->queue);
    if (!FSEventStreamStart(stream)) {
        FSEventStreamInvalidate(stream);
        FSEventStreamRelease(stream);
        dispatch_release(r->queue);
        free(r);
        return NULL;
    }
    return r;
}

// stopReplay tears the stream down. It returns once no further callback can
// arrive, which is what makes it safe for Go to drop the handle afterwards.
static void stopReplay(replay_t *r) {
    if (r == NULL) {
        return;
    }
    FSEventStreamStop(r->stream);
    FSEventStreamInvalidate(r->stream);
    FSEventStreamRelease(r->stream);
    // Anything already queued runs before this returns, so a callback cannot be
    // in flight against a handle Go is about to forget.
    dispatch_sync(r->queue, ^{});
    dispatch_release(r->queue);
    free(r);
}

static unsigned long long currentEventID(void) {
    return (unsigned long long)FSEventsGetCurrentEventId();
}

static unsigned long long eventIDBefore(dev_t dev, double when) {
    return (unsigned long long)FSEventsGetLastEventIdForDeviceBeforeTime(dev, (CFAbsoluteTime)when);
}

*/
import "C"

import (
	"fmt"
	"sync"
	"time"
	"unsafe"
)

// Dropped reports that the log could not account for everything in a range.
//
// It is the single most important value in this package. Any of the three
// dropped-event flags means the history is incomplete, and an incomplete history
// is worthless for the only thing this is used for: deciding which paths are
// worth checking rather than walking. A caller seeing this must fall back to
// reading the disk.
type Replay struct {
	// Paths are the files the log says changed, in no particular order and
	// possibly with repeats.
	//
	// Candidates, not findings. Every one has to be compared against the snapshot
	// before it means anything: the log records that something was written, not
	// that the result differs from what a snapshot holds — a file saved twice back
	// to its original contents appears here and differs from nothing.
	Paths []string
	// UpTo is the event id the replay reached, and the cursor for the next one.
	UpTo uint64
	// Dropped says the log admitted to missing events. Nothing here can then be
	// treated as a complete account of the range.
	Dropped bool
}

// UUID identifies a volume's event log.
//
// It changes when the log is wiped and started again, which is something a person
// can do to their own removable disk — so every event id recorded against the old
// value has to be discarded when this moves. An empty string means the volume has
// no log this process can read, and is not an error: a disk without a history is
// simply a disk this package cannot help with.
func UUID(mountPoint string) (string, error) {
	dev, err := device(mountPoint)
	if err != nil {
		return "", err
	}
	var buf [64]C.char
	if C.uuidFor(dev, &buf[0], C.int(len(buf))) != 0 {
		return "", nil
	}
	return C.GoString(&buf[0]), nil
}

// Latest is the current global event counter.
//
// Cheap enough to poll: if it has not moved since the last look, nothing has
// happened anywhere, and no replay is worth starting.
func Latest() uint64 { return uint64(C.currentEventID()) }

// Before is the last event id on a volume before a moment in time.
//
// This is how a replay is anchored to a snapshot: the question being asked is
// "what has changed since this snapshot was taken", and the snapshot's date is
// the only thing known about when that was.
func Before(mountPoint string, t time.Time) (uint64, error) {
	dev, err := device(mountPoint)
	if err != nil {
		return 0, err
	}
	// CFAbsoluteTime counts from 2001-01-01 UTC, which is what this constant is.
	const cfEpoch = 978307200
	return uint64(C.eventIDBefore(dev, C.double(float64(t.Unix()-cfEpoch)))), nil
}

// Since replays what the log remembers of one volume after an event id.
//
// The replay runs on its own thread with its own run loop and stops as soon as
// macOS says the history is finished. The deadline is the backstop for a log that
// never says so, and reaching it is reported as Dropped: a replay that was cut
// short has not accounted for the range it was asked about, and saying otherwise
// would be the one mistake this package must not make.
func Since(mountPoint string, id uint64, deadline time.Duration) (Replay, error) {
	dev, err := device(mountPoint)
	if err != nil {
		return Replay{}, err
	}

	c := &collector{upTo: id, done: make(chan struct{})}
	handle := register(c)
	defer unregister(handle)

	r := C.startReplay(dev, C.ulonglong(id), C.long(handle))
	if r == nil {
		return Replay{}, fmt.Errorf("fsevents: %s: the event log would not replay", mountPoint)
	}
	// Torn down before the handle is forgotten. stopReplay drains the queue, so
	// no callback can be in flight against a collector nothing points at.
	defer C.stopReplay(r)

	timer := time.NewTimer(deadline)
	defer timer.Stop()
	select {
	case <-c.done:
	case <-timer.C:
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	// A replay that never reported the end of history was cut off by the
	// deadline, and has not accounted for the range it was asked about. Saying
	// otherwise would be the one mistake this package must not make.
	if !c.historyDone {
		c.dropped = true
	}
	return Replay{Paths: c.paths, UpTo: c.upTo, Dropped: c.dropped}, nil
}

func device(mountPoint string) (C.dev_t, error) {
	p := C.CString(mountPoint)
	defer C.free(unsafe.Pointer(p))

	var dev C.dev_t
	if C.deviceFor(p, &dev) != 0 {
		return 0, fmt.Errorf("fsevents: cannot identify the volume at %s", mountPoint)
	}
	return dev, nil
}

// collector gathers one replay's results.
type collector struct {
	mu          sync.Mutex
	paths       []string
	upTo        uint64
	dropped     bool
	historyDone bool
	// done is closed once the log says the history is finished, which is the only
	// signal that a replay has delivered everything it is going to.
	done     chan struct{}
	finished sync.Once
}

// finish releases the waiter, at most once: the log is entitled to deliver more
// than one event carrying the history-done flag.
func (c *collector) finish() { c.finished.Do(func() { close(c.done) }) }

// Registered by handle rather than by passing a Go pointer into C, which cgo
// forbids: the context is an integer, and this maps it back.
var (
	live   = map[int]*collector{}
	liveMu sync.Mutex
	nextID int
)

func register(c *collector) int {
	liveMu.Lock()
	defer liveMu.Unlock()

	nextID++
	live[nextID] = c
	return nextID
}

func unregister(handle int) {
	liveMu.Lock()
	defer liveMu.Unlock()

	delete(live, handle)
}

func lookup(handle int) *collector {
	liveMu.Lock()
	defer liveMu.Unlock()

	return live[handle]
}
