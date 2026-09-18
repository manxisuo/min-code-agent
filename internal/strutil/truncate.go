// Package strutil has small string helpers safe for multi-byte text.
package strutil

// TruncateRunes returns s cut to at most max runes, appending ellipsis when cut.
// max counts runes (characters), not bytes — safe for Chinese and other UTF-8.
func TruncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}
