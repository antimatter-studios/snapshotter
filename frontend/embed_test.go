package frontend

import (
	"io/fs"
	"testing"
)

// The window's own assets have to be in the binary, and nothing else checked.
//
// Every test in this repository exercises the Go or the TypeScript; none opens the
// window, so a binary with no frontend in it passes the whole suite, signs,
// notarizes, installs, and reports the right version — and then fails the moment
// somebody opens it, with "no index.html could be found in your Assets fs.FS".
// That shipped, and the only thing that noticed was a person clicking on it.
//
// Wails searches the tree for index.html and subs to wherever it finds it, so this
// asserts the same thing it asserts: that the file is somewhere in here.

func TestTheWindowIsInTheBinary(t *testing.T) {
	found := ""
	err := fs.WalkDir(Assets, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && d.Name() == "index.html" {
			found = path
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking the embedded assets: %v", err)
	}
	if found == "" {
		t.Fatal("there is no index.html in the embedded assets, so the window cannot open. " +
			"Run `npm --prefix frontend run build` before building the binary.")
	}

	// Not empty, either. An index.html of zero bytes satisfies the search above and
	// still opens a blank window.
	info, err := fs.Stat(Assets, found)
	if err != nil {
		t.Fatalf("stat %s: %v", found, err)
	}
	if info.Size() == 0 {
		t.Fatalf("%s is empty", found)
	}
}

// The scripts and styles it references have to be there too: an index.html that
// loads a bundle which is not in the binary opens a window that is blank rather
// than one that fails, which is harder to diagnose and looks like a hang.
func TestWhatTheWindowLoadsIsInThereWithIt(t *testing.T) {
	assets := 0
	err := fs.WalkDir(Assets, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		switch {
		case len(path) > 3 && path[len(path)-3:] == ".js",
			len(path) > 4 && path[len(path)-4:] == ".css":
			assets++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if assets == 0 {
		t.Fatal("the embedded assets contain no javascript or stylesheets, so the window would open blank")
	}
}
