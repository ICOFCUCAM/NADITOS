// Package normalize converts a raw ANPR read ("ab 12 cd ", "AB-12-CD",
// "AB-O0-CD") into a canonical plate string the rest of the platform
// can compare. Also runs the per-tenant plate_regex sanity check.
package normalize

import (
	"errors"
	"regexp"
	"strings"
)

// Normalize uppercases, strips whitespace and most separators, and corrects
// common OCR confusions when the resulting form would otherwise fail the
// tenant regex.
//
// Examples:
//
//   "ab 12-cd"     -> "AB12CD"
//   " AB-O0-CD"    -> "AB-00-CD" (when target regex allows hyphens but no Os)
//   "ab.12 .cd"    -> "AB12CD"
//
// We never apply confusion corrections without checking the regex,
// because some countries legitimately use 'O' in plates.
func Normalize(raw, plateRegex string) (string, error) {
	if raw == "" {
		return "", errors.New("empty plate")
	}
	s := strings.ToUpper(strings.TrimSpace(raw))
	s = stripSeparators(s)

	if plateRegex == "" {
		return s, nil
	}
	rx, err := regexp.Compile(plateRegex)
	if err != nil {
		return s, nil // bad tenant regex — don't block, just pass through
	}
	if rx.MatchString(s) {
		return s, nil
	}

	// Try common OCR confusions: O/0, I/1, B/8, S/5, Z/2.
	for _, fix := range []func(string) string{
		func(t string) string { return strings.ReplaceAll(t, "O", "0") },
		func(t string) string { return strings.ReplaceAll(t, "I", "1") },
		func(t string) string { return strings.ReplaceAll(t, "B", "8") },
		func(t string) string { return strings.ReplaceAll(t, "S", "5") },
		func(t string) string { return strings.ReplaceAll(t, "Z", "2") },
	} {
		if fixed := fix(s); rx.MatchString(fixed) {
			return fixed, nil
		}
	}
	return s, errors.New("plate does not match tenant regex")
}

func stripSeparators(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case ' ', '-', '.', '_':
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}
