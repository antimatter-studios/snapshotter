package services

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"snapshotter/internal/mountmgr"
)

// knownTree is a filesystem shaped like the one this application was written
// for, including the kinds of file whose loss is unrecoverable: keys and a
// credential store.
//
// Everything sits in subdirectories on purpose. mountmgr.Fake rewrites and
// removes top-level regular files to manufacture differences for Compare, so
// keeping the tree one level down leaves the expected search results exact.
var knownTree = map[string]string{
	".ssh/id_rsa":                         "PRIVATE KEY\n",
	".ssh/id_rsa.pub":                     "ssh-rsa AAAA\n",
	".ssh/known_hosts":                    "github.com ssh-rsa AAAA\n",
	".ssh/config":                         "Host *\n",
	"Documents/meeting-notes.md":          "notes\n",
	"Documents/RSA-primer.txt":            "how rsa works\n",
	"Documents/invoice-2026-08.pdf":       "%PDF\n",
	"Projects/app/vault.kdbx":             "KDBX\n",
	"Projects/app/src/main.go":            "package main\n",
	"Projects/app/src/main_test.go":       "package main\n",
	"Projects/app/node_modules/lp/i.js":   "module.exports=1\n",
	"Library/Keychains/login.keychain-db": "keychain\n",
}

// searchSim stands up the known tree inside a fake mount and returns a service
// plus the seed root, so expectations can be written as paths relative to it.
func searchSim(t *testing.T) (*SearchService, string) {
	t.Helper()

	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("no home directory: %v", err)
	}
	seed, err := os.MkdirTemp(home, ".snapshotter-searchsim-")
	if err != nil {
		t.Skipf("cannot create a seed under home: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(seed) })

	for rel, body := range knownTree {
		p := filepath.Join(seed, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	fake := mountmgr.NewFake(t.TempDir(), seed)
	if err := fake.Mount(context.Background(), []string{testSnapshot}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = fake.Unmount(context.Background(), []string{testSnapshot}) })

	runner := &searchRunner{stamps: []string{"2026-08-14-003200"}}
	return NewSearchService(Deps{
		Runner: runner, Mounts: fake, Volume: "/System/Volumes/Data", Faking: true,
	}), seed
}

// relHits reduces a result to seed-relative paths, sorted, so a table can state
// exactly what a phrase should find.
func relHits(t *testing.T, res SearchResult, seed string) []string {
	t.Helper()
	out := make([]string, 0, len(res.Hits))
	for _, h := range res.Hits {
		rel, err := filepath.Rel(seed, h.LivePath)
		if err != nil {
			t.Fatalf("hit %q is not under the seed: %v", h.LivePath, err)
		}
		out = append(out, rel)
	}
	sort.Strings(out)
	return out
}

// TestSearchPhrasesAgainstAKnownFilesystem is the panel's behaviour, driven
// through the same service call the panel makes, against a tree whose contents
// are known exactly. Each phrase is one somebody would actually type.
func TestSearchPhrasesAgainstAKnownFilesystem(t *testing.T) {
	svc, seed := searchSim(t)

	cases := []struct {
		name  string
		term  string
		under string
		want  []string
	}{
		{
			name: "the exact thing that was lost",
			term: "id_rsa",
			want: []string{".ssh/id_rsa", ".ssh/id_rsa.pub"},
		},
		{
			name: "a remembered fragment matches case-insensitively",
			term: "rsa",
			// known_hosts contains "ssh-rsa" in its CONTENTS, not its name — a
			// name search must not match on contents, or every phrase becomes a
			// grep over the whole volume.
			want: []string{".ssh/id_rsa", ".ssh/id_rsa.pub", "Documents/RSA-primer.txt"},
		},
		{
			name: "an extension finds the vault",
			term: ".kdbx",
			want: []string{"Projects/app/vault.kdbx"},
		},
		{
			name: "a word finds it too",
			term: "vault",
			want: []string{"Projects/app/vault.kdbx"},
		},
		{
			name: "a suffix matches both the file and its test",
			term: "main",
			want: []string{"Projects/app/src/main.go", "Projects/app/src/main_test.go"},
		},
		{
			name:  "confining the search excludes the keys",
			term:  "rsa",
			under: "Documents",
			want:  []string{"Documents/RSA-primer.txt"},
		},
		{
			name:  "confining it the other way excludes the primer",
			term:  "rsa",
			under: ".ssh",
			want:  []string{".ssh/id_rsa", ".ssh/id_rsa.pub"},
		},
		{
			name: "something that is not there finds nothing",
			term: "tax-return-1998",
			want: []string{},
		},
		{
			name: "a dotfile directory's contents are reachable",
			term: "known_hosts",
			want: []string{".ssh/known_hosts"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			under := ""
			if c.under != "" {
				under = filepath.Join(seed, c.under)
			}

			res, err := svc.Search(context.Background(), c.term, under)
			if err != nil {
				t.Fatalf("Search(%q): %v", c.term, err)
			}
			got := relHits(t, res, seed)

			if strings.Join(got, "|") != strings.Join(c.want, "|") {
				t.Errorf("Search(%q, under=%q)\n got: %v\nwant: %v", c.term, c.under, got, c.want)
			}
		})
	}
}

// Ordering is part of the answer: the thing you lost is usually nearer the top of
// a tree than the bottom of it, and a list that buries it is a worse answer.
func TestSearchOrdersShallowestFirst(t *testing.T) {
	svc, seed := searchSim(t)

	res, err := svc.Search(context.Background(), "main", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) < 2 {
		t.Fatalf("expected both main files, got %d", len(res.Hits))
	}
	prev := strings.Count(res.Hits[0].LivePath, "/")
	for _, h := range res.Hits[1:] {
		if d := strings.Count(h.LivePath, "/"); d < prev {
			t.Fatalf("a deeper hit came first: %v", relHits(t, res, seed))
		} else {
			prev = d
		}
	}
}

// A hit has to carry enough to restore from, or the panel's Restore button has
// nothing to call.
func TestEveryHitCarriesWhatARestoreNeeds(t *testing.T) {
	svc, seed := searchSim(t)

	res, err := svc.Search(context.Background(), "vault", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Hits) != 1 {
		t.Fatalf("got %d hits, want 1", len(res.Hits))
	}
	h := res.Hits[0]

	if h.Snapshot != testSnapshot {
		t.Errorf("snapshot = %q, want %q", h.Snapshot, testSnapshot)
	}
	if h.Stamp != "2026-08-14-003200" {
		t.Errorf("stamp = %q", h.Stamp)
	}
	if !strings.HasPrefix(h.LivePath, seed) {
		t.Errorf("livePath %q is not a live path", h.LivePath)
	}
	if h.Name != "vault.kdbx" {
		t.Errorf("name = %q", h.Name)
	}
	if h.Size == 0 {
		t.Error("size is zero, so the panel would show nothing useful")
	}
	if h.ModTime.IsZero() {
		t.Error("modTime is zero")
	}
}
