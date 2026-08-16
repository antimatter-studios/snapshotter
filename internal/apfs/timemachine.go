package apfs

import (
	"context"
	"strings"
)

// TimeMachine describes whether Time Machine owns the snapshots on this
// machine, which decides whether a configured retention window is honest.
//
// Every snapshot named com.apple.TimeMachine.*.local belongs to backupd. With
// no backup destination configured, backupd never runs a backup cycle and so
// never thins them, and a retention window of weeks holds. Attach a
// destination and backupd starts thinning local snapshots to roughly a day as
// part of each cycle, silently collapsing that window. The UI has to say so,
// otherwise the retention it displays is a promise the system will break.
type TimeMachine struct {
	HasDestination bool
	// Detail is the raw tmutil response, shown verbatim so an unexpected
	// configuration is visible rather than flattened into a boolean.
	Detail string
}

// DestinationInfo reports the Time Machine destination configuration.
//
// Verified on macOS 26: with nothing configured, tmutil prints "No destinations
// configured." and exits ZERO. An earlier comment here claimed it exits non-zero,
// which was wrong, and a check written on that belief alone would report a
// destination that does not exist.
//
// Both are therefore treated as "no destination": a non-zero exit because older
// releases did behave that way, and the message because this one does. A positive
// answer needs positive evidence — a Name or an ID in the output — rather than
// merely the absence of a failure.
// ThinningWarning is what a configured destination does to local snapshots,
// stated once.
//
// It lives here, beside DestinationInfo, because everything that needs to say it
// has just called that function to find out whether to say it at all — the
// window, the menu bar and the command line. It was written out separately in
// each of them, which is how it came to be worded three different ways.
const ThinningWarning = "Time Machine has a backup destination configured, and its backup cycle " +
	"thins local snapshots to roughly 24 hours. A longer retention window will not hold."

func DestinationInfo(ctx context.Context, r Runner) TimeMachine {
	out, err := r.Run(ctx, "tmutil", "destinationinfo")
	detail := strings.TrimSpace(out)
	if err != nil || strings.Contains(out, "No destinations configured") {
		return TimeMachine{HasDestination: false, Detail: detail}
	}
	return TimeMachine{HasDestination: strings.Contains(out, "Name") || strings.Contains(out, "ID"), Detail: detail}
}
