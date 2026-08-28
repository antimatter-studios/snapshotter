// Package manual is the built-in manual: the topic pages compiled into the
// binary, and the lookup `snapshotter help` uses.
//
// # Why the pages are documents rather than extracted from comments
//
// The sibling approach — a generator lifting blocks out of the source, so the
// paragraph describing a rule sits in the same diff as the rule — is the right
// answer when the manual documents behaviour that lives next to code. It is not
// the answer here.
//
// This application's hardest documentation is narrative: why a snapshot can
// vanish, why one command writes to every disk, why reading your own files needs
// a password. Those are essays about a system rather than annotations on a
// function, and cutting them into blocks scattered across packages would make
// them worse rather than fresher. There is a mechanical objection too: the best
// prose here is attached to declarations, and gofmt reformats doc comments —
// re-indenting code blocks and rewriting list markers — so an extractor would
// force that prose off the declarations it describes.
//
// What the manual is for is DISTRIBUTION. All of this was already written and
// none of it reached anyone: the documents are in the repository, and somebody
// who installed a disk image has the binary and nothing else.
package manual

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed topics/*.md
var files embed.FS

// Topic is one manual page.
type Topic struct {
	// Name is the page's address — what a reader types after `snapshotter help`.
	Name string
	// Title and Summary come from the page's headers. Summary is the one line the
	// listing shows, so a page without one is invisible in the only place anybody
	// goes looking for it; load refuses that rather than listing a blank row.
	Title   string
	Summary string
	// Aliases are other names that reach this page, because a topic's best name
	// and the phrase someone reaches for are not always the same word. Somebody
	// who has just watched a snapshot disappear types "purgeable", not
	// "snapshots", and answering "no such topic" to a question the manual plainly
	// covers teaches nothing.
	Aliases []string
	// Body is the markdown, with the headers stripped.
	Body string
}

// All returns every topic, ordered by name.
//
// A page that will not parse is left out rather than failing the command: the
// manual is an aid, and `snapshotter help` refusing to list anything because one
// page has a malformed header is a worse outcome than one missing page. The test
// suite asserts every embedded page loads, so the gap cannot survive a build.
func All() []Topic {
	entries, err := fs.ReadDir(files, "topics")
	if err != nil {
		return nil
	}
	out := make([]Topic, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		t, err := load(e.Name())
		if err != nil {
			continue
		}
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Lookup finds one topic by name or alias.
//
// Hyphens, underscores and case are interchangeable, because the name is a
// phrase a reader half-remembers rather than an identifier they copied:
// `help bulk-deletion` and `help bulk_deletion` are the same question, and
// refusing one of them teaches nothing.
func Lookup(name string) (Topic, bool) {
	want := normalise(name)
	if want == "" {
		return Topic{}, false
	}
	for _, t := range All() {
		if normalise(t.Name) == want {
			return t, true
		}
		for _, a := range t.Aliases {
			if normalise(a) == want {
				return t, true
			}
		}
	}
	return Topic{}, false
}

// normalise reduces a name to what two spellings of it have in common.
func normalise(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	return strings.ReplaceAll(s, "_", "-")
}

// load reads one page: a short header block, a blank line, then markdown.
//
// The headers are a handful of "key: value" lines rather than YAML front matter,
// because three fields do not need a parser and a dependency that reads
// arbitrary YAML is a large thing to add for "title:".
func load(file string) (Topic, error) {
	raw, err := files.ReadFile("topics/" + file)
	if err != nil {
		return Topic{}, err
	}
	name := strings.TrimSuffix(file, ".md")

	t := Topic{Name: name}
	lines := strings.Split(string(raw), "\n")
	var i int
	for ; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			i++
			break
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			return Topic{}, fmt.Errorf("manual: %s: %q is not a header", file, line)
		}
		value = strings.TrimSpace(value)
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "title":
			t.Title = value
		case "summary":
			t.Summary = value
		case "aliases":
			for _, a := range strings.Split(value, ",") {
				if a = strings.TrimSpace(a); a != "" {
					t.Aliases = append(t.Aliases, a)
				}
			}
		default:
			return Topic{}, fmt.Errorf("manual: %s: unknown header %q", file, key)
		}
	}

	if t.Title == "" || t.Summary == "" {
		return Topic{}, fmt.Errorf("manual: %s needs a title and a summary", file)
	}
	t.Body = strings.TrimSpace(strings.Join(lines[i:], "\n"))
	if t.Body == "" {
		return Topic{}, fmt.Errorf("manual: %s has no body", file)
	}
	return t, nil
}

// Suggest names the topics closest to something that did not match, so a near
// miss is answered with the page rather than with a list of everything.
//
// Substring both ways: "mount" should reach "mounting", and someone typing
// "restoring-files" should reach "restoring".
func Suggest(name string) []string {
	want := normalise(name)
	if want == "" {
		return nil
	}
	var out []string
	for _, t := range All() {
		hay := normalise(t.Name)
		if strings.Contains(hay, want) || strings.Contains(want, hay) {
			out = append(out, t.Name)
			continue
		}
		for _, a := range t.Aliases {
			if alias := normalise(a); strings.Contains(alias, want) || strings.Contains(want, alias) {
				out = append(out, t.Name)
				break
			}
		}
	}
	return out
}
