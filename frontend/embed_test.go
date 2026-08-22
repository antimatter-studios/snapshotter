package frontend

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/application"
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

// What the webview receives, from the same handler the window uses.
//
// The tests above prove the file is in the binary. This proves the asset server
// hands it over — which is the thing that actually failed, and it failed in a way
// nothing could see: Wails returns "no index.html could be found in your Assets
// fs.FS" as the response BODY, so the webview renders it as a page. Nothing is
// written to stderr, nothing exits non-zero, and the application looks like it
// started normally. The report was "a big white page with that text in it", and
// looking for the error in the logs found nothing because it was never there.
func TestTheAssetServerHandsOverThePage(t *testing.T) {
	handler := application.AssetFileServerFS(Assets)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("the window's first request came back %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()

	// The failure renders as a page, so the page has to be checked for it. A test
	// asserting only on the status code would pass against the white page.
	if strings.Contains(body, "could be found in your Assets") {
		t.Fatalf("the asset server served its own error as the page:\n%s", body)
	}
	if !strings.Contains(strings.ToLower(body), "<!doctype html") {
		t.Fatalf("the window's first request did not come back as a document:\n%s", firstLine(body))
	}
}

// And the bundle it asks for next. An index.html whose script is missing renders a
// white page too — a blank one rather than one carrying an explanation, which is
// harder to place.
func TestTheBundleThePageAsksForIsServedToo(t *testing.T) {
	handler := application.AssetFileServerFS(Assets)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	page := rec.Body.String()

	script := regexp.MustCompile(`src="/?([^"]+\.js)"`).FindStringSubmatch(page)
	if script == nil {
		t.Fatalf("the page loads no javascript, so the window would be blank:\n%s", firstLine(page))
	}

	asked := httptest.NewRecorder()
	handler.ServeHTTP(asked, httptest.NewRequest(http.MethodGet, "/"+strings.TrimPrefix(script[1], "/"), nil))
	if asked.Code != http.StatusOK {
		t.Errorf("the page asks for %s and the server answered %d", script[1], asked.Code)
	}
	if asked.Body.Len() == 0 {
		t.Errorf("%s came back empty", script[1])
	}
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
