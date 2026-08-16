// Package i18n holds the text the Go side shows, in every language this build
// speaks.
//
// The window has a catalogue of its own, compiled into the frontend, because its
// text is needed for the first paint. This one exists because the window is not
// the only surface: the menu bar is drawn here, notifications are posted from
// here, and the health findings are worded here and shown in both places. None of
// that can reach a TypeScript object.
//
// The two catalogues share a setting rather than a file. The language lives in
// the settings, the settings watcher already redraws the menu bar when they
// change, and so choosing a language in the window changes both surfaces without
// a relaunch.
//
// Keys are `area.thing`, matching the window's convention, but the two key sets
// are deliberately separate: almost nothing appears on both surfaces, and a
// shared list would be mostly entries that only one side ever asks for.
package i18n

import (
	"strings"
	"sync"
)

// current is the language everything is answered in.
//
// Package-level rather than threaded through every call, because the alternative
// is a language argument on functions whose callers — a launchd agent posting a
// notification, a menu being rebuilt — have no reason to know about languages at
// all. Guarded because the settings watcher writes it from its own goroutine
// while a menu redraw reads it.
var (
	mu      sync.RWMutex
	current = "en"
)

// catalogues is every language this build carries, English first because it is
// the fallback.
var catalogues = map[string]map[string]string{
	"en": en,
	"de": de,
	"es": es,
	"fr": fr,
}

// SetLanguage chooses the language. An unknown code leaves it as it was, which
// keeps a hand-edited settings file from emptying the menu bar.
func SetLanguage(code string) {
	mu.Lock()
	defer mu.Unlock()
	if _, ok := catalogues[code]; ok {
		current = code
	}
}

// Language reports what is being spoken.
func Language() string {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// T returns the text for a key, with any {placeholders} filled in from pairs of
// name and value.
//
// A key with no entry returns the key itself. That is deliberately ugly: a
// missing string should look like a fault in the application rather than like a
// deliberately terse label, because the second kind gets shipped.
func T(key string, args ...string) string {
	mu.RLock()
	lang := current
	mu.RUnlock()

	text, ok := catalogues[lang][key]
	if !ok {
		// English carries every key by construction — the test in this package
		// requires it — so this is the fallback that matters in practice.
		if text, ok = en[key]; !ok {
			return key
		}
	}

	// Substituted by name rather than by position: the other languages routinely
	// need a different word order, and a translator has to be free to move a
	// placeholder to where the sentence wants it.
	for i := 0; i+1 < len(args); i += 2 {
		text = strings.ReplaceAll(text, "{"+args[i]+"}", args[i+1])
	}
	return text
}
