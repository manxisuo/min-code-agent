package strutil

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateRunesCJK(t *testing.T) {
	s := "该项目没有任何源代码项目描述文件（无 README）"
	out := TruncateRunes(s, 10)
	if !utf8.ValidString(out) {
		t.Fatalf("invalid utf8: %q", out)
	}
	if strings.ContainsRune(out, utf8.RuneError) {
		t.Fatalf("replacement rune in %q", out)
	}
	if !strings.HasSuffix(out, "…") {
		t.Fatalf("suffix = %q", out)
	}
	// 10 runes + ellipsis
	if n := len([]rune(out)); n != 11 {
		t.Fatalf("rune count = %d out=%q", n, out)
	}
}

func TestTruncateRunesShort(t *testing.T) {
	if got := TruncateRunes("hello", 10); got != "hello" {
		t.Fatalf("got %q", got)
	}
	if got := TruncateRunes("abc", 0); got != "" {
		t.Fatalf("got %q", got)
	}
}
