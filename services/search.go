package services

import (
	"context"
	"errors"
	"fmt"

	"snapshotter/internal/apfs"
	"snapshotter/internal/diffs"
	"snapshotter/internal/find"
	"snapshotter/internal/vfs"
)

// SearchService answers the question that follows a deletion.
//
// Browse assumes you know where to look. After losing something you know what
// it was called and roughly when it still existed, and almost never which
// directory it was in. Every other screen here is organised by place; this one
// is organised by name.
type SearchService struct{ Deps }

// NewSearchService builds the service.
func NewSearchService(d Deps) *SearchService { return &SearchService{Deps: d} }

// SearchResult is every match, plus an honest account of what was not looked at.
type SearchResult struct {
	Term string     `json:"term"`
	Hits []find.Hit `json:"hits"`
	// Searched and Skipped name the snapshots that were and were not looked
	// inside. An unmounted snapshot cannot be searched, and saying so is the
	// difference between "it is not there" and "nobody looked".
	Searched []string `json:"searched"`
	Skipped  []string `json:"skipped"`
	// Truncated reports that a limit stopped the walk, so absence here proves
	// nothing.
	Truncated bool `json:"truncated"`
	// Incomplete reports that the walk ran out of budget before it ran out of
	// tree. Distinct from Truncated: there were not too many answers, there was
	// too much to read, and the advice differs.
	Incomplete bool   `json:"incomplete"`
	Note       string `json:"note,omitempty"`
}

// Search looks inside every mounted snapshot for entries whose name contains
// term.
//
// Only mounted snapshots can be searched, and rather than mounting on the user's
// behalf — which costs an authorization prompt per batch — the unsearched ones
// are named so the user can decide to open them.
func (s *SearchService) Search(ctx context.Context, term, under string) (SearchResult, error) {
	out := SearchResult{Term: term}

	snaps, err := apfs.List(ctx, s.Runner, s.Volume)
	if err != nil {
		return out, err
	}

	for _, snap := range snaps {
		mounted, err := s.Mounts.IsMounted(snap.Name)
		if err != nil {
			return out, err
		}
		if !mounted {
			out.Skipped = append(out.Skipped, snap.Stamp)
			continue
		}
		mountPoint, err := s.Mounts.MountPoint(snap.Name)
		if err != nil {
			return out, err
		}

		hits, err := find.Search(ctx, mountPoint, snap.Name, snap.Stamp, term,
			find.Options{Under: under})
		out.Hits = append(out.Hits, hits...)
		out.Searched = append(out.Searched, snap.Stamp)

		var truncated *find.ErrTruncated
		var budget *find.ErrBudget
		switch {
		case errors.As(err, &truncated):
			out.Truncated = true
		case errors.As(err, &budget):
			out.Incomplete = true
		case err != nil:
			return out, err
		}
	}

	switch {
	case len(out.Searched) == 0 && len(out.Skipped) > 0:
		out.Note = fmt.Sprintf("No snapshot is open, so nothing was searched. Open one of the %d "+
			"snapshots to search inside it.", len(out.Skipped))
	case len(out.Skipped) > 0:
		out.Note = fmt.Sprintf("%d snapshot(s) were not searched because they are not open, so "+
			"this is not the whole history.", len(out.Skipped))
	case out.Truncated:
		out.Note = "Stopped early at the match limit — narrow the search to see the rest."
	case out.Incomplete:
		// A snapshot is a whole volume, so an unscoped search reads every
		// application and framework on the disk before it reaches anything the
		// user recognises. Saying so is the difference between "not found" and
		// "not looked at".
		out.Note = "A snapshot covers the whole volume, so this stopped before reading all of " +
			"it. Search inside a folder — your home directory, say — to cover it properly."
	}
	return out, nil
}

// DeletedSince lists what a folder held when the snapshot was taken and no
// longer holds.
//
// This is the recovery view. Compare shows everything that differs, which after
// a week of ordinary work is mostly noise; the only rows that matter when
// something has gone missing are the ones that are gone.
func (s *SearchService) DeletedSince(ctx context.Context, snapshotName, livePath string, deep bool) (diffs.Result, error) {
	var out diffs.Result

	mounted, err := s.Mounts.IsMounted(snapshotName)
	if err != nil {
		return out, err
	}
	if !mounted {
		return out, errNotMounted
	}
	mountPoint, err := s.Mounts.MountPoint(snapshotName)
	if err != nil {
		return out, err
	}
	snapshotDir, err := vfs.ToSnapshot(mountPoint, livePath)
	if err != nil {
		return out, err
	}

	res, err := diffs.Compare(ctx, snapshotDir, livePath, diffs.Options{Deep: deep}, nil)
	if err != nil {
		return out, err
	}

	// Filter rather than ask Compare for less: it walks both sides either way,
	// and the unfiltered counts are still wanted for the summary.
	kept := make([]diffs.Change, 0, len(res.Changes))
	for _, c := range res.Changes {
		if c.Status == diffs.OnlyInSnapshot {
			kept = append(kept, c)
		}
	}
	res.Changes = kept
	return res, nil
}
