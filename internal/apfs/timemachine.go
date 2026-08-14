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
func DestinationInfo(ctx context.Context, r Runner) TimeMachine {
	out, err := r.Run(ctx, "tmutil", "destinationinfo")
	detail := strings.TrimSpace(out)
	if err != nil || strings.Contains(out, "No destinations configured") {
		return TimeMachine{HasDestination: false, Detail: detail}
	}
	return TimeMachine{HasDestination: strings.Contains(out, "Name") || strings.Contains(out, "ID"), Detail: detail}
}
