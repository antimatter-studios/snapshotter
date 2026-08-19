package i18n

import (
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// Bytes words a size the way the current language writes numbers.
//
// Not fmt.Sprintf("%.1f"): the decimal separator is a comma in German, Spanish
// and French, so "45.2 GB" is wrong in three of the four languages this ships
// with. x/text/message knows the rule per locale and is already a dependency,
// go-i18n having brought it in for the plural tables.
//
// The unit symbols themselves are left alone. GB and MB are written the same way
// in all four, and inventing a translation for them would be worse than leaving
// them.
func Bytes(n uint64) string {
	const step = 1024
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}

	value := float64(n)
	unit := 0
	for value >= step && unit < len(units)-1 {
		value /= step
		unit++
	}

	p := message.NewPrinter(language.Make(Language()))
	if unit == 0 {
		// Whole bytes: a fraction of a byte is not a thing.
		return p.Sprintf("%d %s", n, units[unit])
	}
	// One decimal below ten, none above: "1.4 GB" is a useful distinction and
	// "437.2 GB" is noise.
	if value < 10 {
		return p.Sprintf("%.1f %s", value, units[unit])
	}
	return p.Sprintf("%.0f %s", value, units[unit])
}
