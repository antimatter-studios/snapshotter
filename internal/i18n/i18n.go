// Package i18n holds the text the Go side shows, in every language this build
// speaks.
//
// The window has a catalogue of its own — i18next, compiled into the frontend —
// because its text is needed for the first paint. This one exists because the
// window is not the only surface: the menu bar is drawn here, notifications are
// posted from here, and the health findings are worded here and shown in both
// places. None of that can reach a JavaScript bundle.
//
// The two share a setting rather than a file. The language lives in the settings,
// the settings watcher already redraws the menu bar when they change, and so
// choosing a language in the window changes both surfaces without a relaunch.
//
// go-i18n does the work: message lookup, fallback, and the CLDR plural rules that
// a map of strings cannot express — Spanish needs two forms where English needs
// two but splits them differently, and French treats zero as singular. Anything
// that counts things has to go through it rather than through fmt.Sprintf.
package i18n

import (
	"embed"
	"encoding/json"
	"log"
	"sync"

	"github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

//go:embed locales/*.json
var locales embed.FS

// Languages are the codes this build carries, in the order a picker offers them.
var Languages = []string{"en", "de", "es", "fr"}

var (
	mu sync.RWMutex
	// bundle holds every message file, loaded once.
	bundle *i18n.Bundle
	// localizer answers in whichever language is currently set. Rebuilt on a
	// change rather than created per call, which is what makes T cheap enough to
	// use inside a menu redraw.
	localizer *i18n.Localizer
	current   = "en"
)

func init() {
	bundle = i18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("json", json.Unmarshal)
	for _, code := range Languages {
		if _, err := bundle.LoadMessageFileFS(locales, "locales/"+code+".json"); err != nil {
			// Embedded at build time, so a failure here is a broken binary rather
			// than a missing file on someone's disk.
			log.Printf("i18n: %s could not be loaded: %v", code, err)
		}
	}
	localizer = i18n.NewLocalizer(bundle, "en")
}

// SetLanguage chooses the language. An unknown code leaves it as it was, so a
// hand-edited settings file cannot empty the menu bar.
func SetLanguage(code string) {
	mu.Lock()
	defer mu.Unlock()

	known := false
	for _, c := range Languages {
		if c == code {
			known = true
			break
		}
	}
	if !known {
		return
	}
	current = code
	// English stays as the fallback behind the chosen language, so a message
	// missing from a translation shows in English rather than as its key.
	localizer = i18n.NewLocalizer(bundle, code, "en")
}

// Language reports what is being spoken.
func Language() string {
	mu.RLock()
	defer mu.RUnlock()
	return current
}

// T returns the text for a message id, filling in any template data.
//
// The variadic pairs are a convenience over go-i18n's config struct: almost every
// call here has no data or one value, and a struct literal at each of those reads
// worse than the sentence it produces.
func T(id string, args ...string) string {
	data := map[string]string{}
	for i := 0; i+1 < len(args); i += 2 {
		data[args[i]] = args[i+1]
	}

	mu.RLock()
	l := localizer
	mu.RUnlock()

	out, err := l.Localize(&i18n.LocalizeConfig{MessageID: id, TemplateData: data})
	if err != nil {
		// The id itself, which is deliberately ugly: a missing string should look
		// like a fault rather than like a terse label, because the second kind
		// gets shipped.
		return id
	}
	return out
}

// N is T for a message that counts something.
//
// Separate because the count has to reach go-i18n as PluralCount for it to pick a
// form, and because a caller that forgets it gets a compile error rather than an
// English plural in a German sentence.
func N(id string, count int, args ...string) string {
	// Capitalised because that is what the message templates reference:
	// {{.Count}}. go-i18n uses PluralCount to pick the form but does not put the
	// number into the template data itself.
	data := map[string]any{"Count": count}
	for i := 0; i+1 < len(args); i += 2 {
		data[args[i]] = args[i+1]
	}

	mu.RLock()
	l := localizer
	mu.RUnlock()

	out, err := l.Localize(&i18n.LocalizeConfig{
		MessageID:    id,
		TemplateData: data,
		PluralCount:  count,
	})
	if err != nil {
		return id
	}
	return out
}
