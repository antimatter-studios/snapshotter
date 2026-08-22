package config

import (
	"fmt"
	"reflect"
	"slices"
	"sort"
	"strconv"
	"strings"

	"snapshotter/internal/watch"
)

// Addressing settings by name, so anything in the file can be read or changed
// without a window — which is what makes the application scriptable, and what
// lets a test put a machine into a known state without hand-writing YAML.
//
// The names are the ones in the file: "schedule.interval_hours", not a second
// vocabulary invented for the command line. They are derived from the yaml tags
// by walking the struct, so a field added to Config is addressable the moment it
// exists and cannot be forgotten here.

// Keys lists every settable name, in file order within each section.
func Keys() []string {
	var keys []string
	walk(reflect.ValueOf(Config{}), "", func(name string, _ reflect.Value) {
		keys = append(keys, name)
	})
	sort.Strings(keys)
	return keys
}

// Get reads one setting as the text the file would hold.
func Get(cfg Config, key string) (string, error) {
	field, err := find(reflect.ValueOf(cfg), key)
	if err != nil {
		return "", err
	}
	return format(field), nil
}

// Themes are the three the window offers. Listed here rather than imported so
// this package stays free of the services that consume it; the window validates
// the same three when it writes.
var themes = map[string]bool{"system": true, "light": true, "dark": true}

// valid checks the values a type cannot: a string field will hold "purple"
// perfectly well, and the application will then have no palette.
//
// Only settings with a closed set of answers are listed. A path or a number has
// no such set, and inventing one here would refuse a machine that is merely
// unusual.
func valid(key, value string) error {
	switch key {
	case "appearance.theme":
		if !themes[value] {
			return fmt.Errorf("config: %s is system, light or dark, not %q", key, value)
		}
	case "schedule.policy":
		// Policies are named in internal/schedule and may be added there, so this
		// checks the shape rather than a list that would go stale: an id is a word,
		// and a tier list is what ParsePolicy accepts.
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("config: %s cannot be empty", key)
		}
	case "appearance.language":
		// The window refused an unknown language and this did not, so a value typed
		// here was accepted and then silently ignored — which reads as the setting
		// not working rather than as the value being wrong.
		if !slices.Contains(Languages, value) {
			return fmt.Errorf("config: %s is one of %s, not %q", key, strings.Join(Languages, ", "), value)
		}
	case "tripwire.sensitivity":
		// A closed set, and one worth refusing rather than falling back on: the
		// watcher would use the default and say so in a log nobody opens, so the
		// person would be left believing they had changed how readily it trips.
		if !watch.Known(watch.Sensitivity(value)) {
			names := make([]string, 0, len(watch.Sensitivities))
			for _, s := range watch.Sensitivities {
				names = append(names, string(s))
			}
			return fmt.Errorf("config: %s is one of %s, not %q", key, strings.Join(names, ", "), value)
		}
	}
	return nil
}

// Set changes one setting in place, refusing a value the field cannot hold.
//
// It takes a pointer because the whole point is to modify the caller's config
// and hand it back to Save — a copy would silently do nothing.
func Set(cfg *Config, key, value string) error {
	field, err := find(reflect.ValueOf(cfg).Elem(), key)
	if err != nil {
		return err
	}
	if err := valid(key, value); err != nil {
		return err
	}

	switch field.Kind() {
	case reflect.String:
		field.SetString(value)
	case reflect.Bool:
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("config: %s takes true or false, not %q", key, value)
		}
		field.SetBool(b)
	case reflect.Int, reflect.Int64:
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return fmt.Errorf("config: %s takes a whole number, not %q", key, value)
		}
		field.SetInt(n)
	case reflect.Float64, reflect.Float32:
		f, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("config: %s takes a number, not %q", key, value)
		}
		field.SetFloat(f)
	case reflect.Slice:
		if field.Type().Elem().Kind() != reflect.String {
			return fmt.Errorf("config: %s is a list of %s, which cannot be set from the command line",
				key, field.Type().Elem().Kind())
		}
		// Comma-separated, because these are lists of short strings — path
		// fragments — and a shell is a poor place to express anything richer.
		// Empty means an empty list rather than a list containing "", which is a
		// distinction that matters: one fragment of "" would match every path.
		var items []string
		for _, part := range strings.Split(value, ",") {
			if part = strings.TrimSpace(part); part != "" {
				items = append(items, part)
			}
		}
		field.Set(reflect.ValueOf(items))

	default:
		return fmt.Errorf("config: %s is a %s, which cannot be set from the command line", key, field.Kind())
	}
	return nil
}

// find resolves a dotted name to the field it addresses.
func find(v reflect.Value, key string) (reflect.Value, error) {
	var found reflect.Value
	walk(v, "", func(name string, field reflect.Value) {
		if name == key {
			found = field
		}
	})
	if !found.IsValid() {
		return found, fmt.Errorf("config: there is no setting called %q (try: snapshotter config keys)", key)
	}
	return found, nil
}

// walk visits every leaf field, naming it by the yaml tags on the way down.
func walk(v reflect.Value, prefix string, visit func(name string, field reflect.Value)) {
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		tag := strings.Split(t.Field(i).Tag.Get("yaml"), ",")[0]
		if tag == "" || tag == "-" {
			continue
		}
		name := tag
		if prefix != "" {
			name = prefix + "." + tag
		}
		field := v.Field(i)
		if field.Kind() == reflect.Struct {
			walk(field, name, visit)
			continue
		}
		visit(name, field)
	}
}

// format renders a value the way the file spells it, so what `get` prints can be
// handed straight back to `set`.
func format(v reflect.Value) string {
	switch v.Kind() {
	case reflect.String:
		return v.String()
	case reflect.Bool:
		return strconv.FormatBool(v.Bool())
	case reflect.Int, reflect.Int64:
		return strconv.FormatInt(v.Int(), 10)
	case reflect.Float64, reflect.Float32:
		// -1 so 6 prints as "6" rather than "6.000000": these are hours and days
		// that people read, and a schedule of "6.000000 hours" reads as a bug.
		return strconv.FormatFloat(v.Float(), 'f', -1, 64)
	case reflect.Slice:
		// Comma-separated, which is what Set accepts, so a value can be read,
		// edited and written back.
		items := make([]string, v.Len())
		for i := range items {
			items[i] = fmt.Sprint(v.Index(i).Interface())
		}
		return strings.Join(items, ",")
	default:
		return fmt.Sprint(v.Interface())
	}
}
