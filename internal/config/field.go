package config

import (
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
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

// Set changes one setting in place, refusing a value the field cannot hold.
//
// It takes a pointer because the whole point is to modify the caller's config
// and hand it back to Save — a copy would silently do nothing.
func Set(cfg *Config, key, value string) error {
	field, err := find(reflect.ValueOf(cfg).Elem(), key)
	if err != nil {
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
	default:
		return fmt.Sprint(v.Interface())
	}
}
