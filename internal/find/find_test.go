package find

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mountLike builds a directory shaped like a mounted snapshot: its root is the
// data volume's root, so /Users/... appears directly inside it.
func mountLike(t *testing.T) string {
	t.Helper()
	mp := t.TempDir()

	files := map[string]string{
		"Users/chris/.ssh/id_rsa":               "private\n",
		"Users/chris/.ssh/id_rsa.pub":           "public\n",
		"Users/chris/Documents/notes.md":        "notes\n",
		"Users/chris/Documents/RSA-notes.txt":   "about rsa\n",
		"Users/chris/Projects/app/vault.kdbx":   "vault\n",
		"Users/chris/Projects/app/src/main.go":  "package main\n",
		"Library/Preferences/com.example.plist": "<plist/>\n",
	}
	for rel, body := range files {
		p := filepath.Join(mp, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return mp
}

const snapName = "com.apple.TimeMachine.2026-08-14-003200.local"

func livePaths(hits []Hit) []string {
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.LivePath
	}
	return out
}

// The question this exists for: "where did my key go", knowing only the name.
func TestSearchFindsEveryVersionOfAName(t *testing.T) {
	mp := mountLike(t)

	hits, err := Search(context.Background(), mp, snapName, "2026-08-14-003200", "id_rsa", Options{})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	got := livePaths(hits)
	if len(got) != 2 {
		t.Fatalf("got %d hits, want id_rsa and id_rsa.pub: %v", len(got), got)
	}
	// Paths must come back in live terms, or they mean nothing to the user and
	// cannot be handed to a restore.
	for _, p := range got {
		if !strings.HasPrefix(p, "/Users/chris/.ssh/") {
			t.Errorf("hit %q is not a live path", p)
		}
	}
	if hits[0].Stamp != "2026-08-14-003200" || hits[0].Snapshot != snapName {
		t.Errorf("hit does not say which snapshot it came from: %+v", hits[0])
	}
}

func TestSearchIsCaseInsensitive(t *testing.T) {
	mp := mountLike(t)
	hits, err := Search(context.Background(), mp, snapName, "s", "rsa", Options{})
	if err != nil {
		t.Fatal(err)
	}
	// id_rsa, id_rsa.pub and RSA-notes.txt — the last one only matches if case is
	// ignored, which is the point.
	if len(hits) != 3 {
		t.Fatalf("got %d hits, want 3 (including RSA-notes.txt): %v", len(hits), livePaths(hits))
	}
}

func TestSearchCanBeConfinedToOneDirectory(t *testing.T) {
	mp := mountLike(t)
	hits, err := Search(context.Background(), mp, snapName, "s", "rsa",
		Options{Under: "/Users/chris/.ssh"})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("got %d hits, want only the two under .ssh: %v", len(hits), livePaths(hits))
	}
	for _, p := range livePaths(hits) {
		if strings.Contains(p, "Documents") {
			t.Errorf("the search escaped its Under directory: %q", p)
		}
	}
}

// Shallow before deep: what you lost is usually nearer the top of a tree.
func TestSearchReturnsShallowestFirst(t *testing.T) {
	mp := mountLike(t)
	hits, err := Search(context.Background(), mp, snapName, "s", "a", Options{})
	if err != nil && !errors.As(err, new(*ErrTruncated)) {
		t.Fatal(err)
	}
	if len(hits) < 2 {
		t.Skip("not enough hits to order")
	}
	prev := strings.Count(hits[0].LivePath, "/")
	for _, h := range hits[1:] {
		d := strings.Count(h.LivePath, "/")
		if d < prev {
			t.Fatalf("deeper hit came before a shallower one: %v", livePaths(hits))
		}
		prev = d
	}
}

// A search that quietly stopped early would read as "that is all there is",
// which is the one wrong answer to this question.
func TestSearchSaysWhenItStoppedEarly(t *testing.T) {
	mp := mountLike(t)
	hits, err := Search(context.Background(), mp, snapName, "s", "a", Options{Limit: 2})

	var truncated *ErrTruncated
	if !errors.As(err, &truncated) {
		t.Fatalf("err = %v, want ErrTruncated", err)
	}
	if len(hits) != 2 {
		t.Errorf("got %d hits with a limit of 2", len(hits))
	}
	if !strings.Contains(truncated.Error(), "narrow the search") {
		t.Errorf("unhelpful message: %v", truncated)
	}
}

func TestSearchWithNoTermIsAnError(t *testing.T) {
	mp := mountLike(t)
	if _, err := Search(context.Background(), mp, snapName, "s", "   ", Options{}); err == nil {
		t.Error("an empty search term was accepted; it would walk the whole volume")
	}
}

func TestSearchStopsWhenTheContextIsCancelled(t *testing.T) {
	mp := mountLike(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := Search(ctx, mp, snapName, "s", "rsa", Options{}); !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want context.Canceled", err)
	}
}

// A snapshot contains other users' home directories, which are readable to
// nobody here. That is normal, and must not fail the search.
func TestSearchSkipsUnreadableDirectories(t *testing.T) {
	mp := mountLike(t)
	locked := filepath.Join(mp, "Users", "someone-else")
	if err := os.MkdirAll(filepath.Join(locked, "deep"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Skipf("cannot make a directory unreadable: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

	hits, err := Search(context.Background(), mp, snapName, "s", "id_rsa", Options{})
	if err != nil {
		t.Fatalf("an unreadable directory failed the search: %v", err)
	}
	if len(hits) != 2 {
		t.Errorf("got %d hits, want 2", len(hits))
	}
}

// The limit bounds answers; the budget bounds reading. Without the second, a term
// with few matches walks an entire volume snapshot — which a small fixture can
// never show, and a real mount showed immediately.
func TestSearchStopsWhenTheWalkRunsOutOfBudget(t *testing.T) {
	mp := mountLike(t)

	// A term that matches nothing, so the hit limit can never fire and only the
	// budget can stop the walk.
	hits, err := Search(context.Background(), mp, snapName, "s", "no-such-name",
		Options{MaxEntries: 3})

	var budget *ErrBudget
	if !errors.As(err, &budget) {
		t.Fatalf("err = %v, want ErrBudget", err)
	}
	if len(hits) != 0 {
		t.Errorf("got %d hits for a term that matches nothing", len(hits))
	}
	if budget.Scanned != 3 {
		t.Errorf("scanned = %d, want the budget of 3", budget.Scanned)
	}
	if !strings.Contains(budget.Error(), "search inside a folder") {
		t.Errorf("the advice is missing: %v", budget)
	}
}

// The two conditions must stay distinguishable, because they want different
// advice: one says narrow the term, the other says point it somewhere.
func TestTheHitLimitAndTheWalkBudgetAreDifferentErrors(t *testing.T) {
	mp := mountLike(t)

	// Plenty of budget, tiny limit: the limit should fire.
	_, err := Search(context.Background(), mp, snapName, "s", "a",
		Options{Limit: 1, MaxEntries: 10_000})
	if !errors.As(err, new(*ErrTruncated)) {
		t.Errorf("hit limit gave %v, want ErrTruncated", err)
	}

	// Plenty of limit, tiny budget: the budget should fire.
	_, err = Search(context.Background(), mp, snapName, "s", "zzz-nothing",
		Options{Limit: 10_000, MaxEntries: 2})
	if !errors.As(err, new(*ErrBudget)) {
		t.Errorf("walk budget gave %v, want ErrBudget", err)
	}
}
