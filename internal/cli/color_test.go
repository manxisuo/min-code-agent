package cli

import (
	"bytes"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/manxisuo/mincode/internal/observability"
)

func TestColorPrompt(t *testing.T) {
	p := promptString()
	if !strings.Contains(p, "mincode") || !strings.Contains(p, ">") {
		t.Fatalf("prompt = %q", p)
	}
	if !strings.Contains(p, "\x1b[") {
		t.Fatalf("expected ANSI codes: %q", p)
	}
}

func TestNOColorDisables(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	p := promptString()
	if strings.Contains(p, "\x1b[") {
		t.Fatalf("NO_COLOR should disable ANSI: %q", p)
	}
	if !strings.Contains(p, "mincode>") {
		t.Fatalf("prompt text = %q", p)
	}
}

func TestStripANSI(t *testing.T) {
	s := paint(ansiCyan, "hi") + " there"
	got := stripANSI(s)
	if got != "hi there" {
		t.Fatalf("strip = %q", got)
	}
}

func TestTruncateUTF8Safe(t *testing.T) {
	// "黄了大半" is multi-byte; cut in the middle of a rune must not produce invalid UTF-8.
	s := "楼下的梧桐叶已经黄了大半，带着一种温吞吞的暖意"
	for n := 1; n < len(s); n++ {
		out := truncateUTF8(s, n)
		if !utf8.ValidString(out) {
			t.Fatalf("invalid UTF-8 at max=%d: %q", n, out)
		}
	}
	// Byte length stays within max+3 ("...")
	for n := 10; n < len(s); n++ {
		out := truncateUTF8(s, n)
		if len(out) > n+3 {
			t.Fatalf("too long at max=%d: %d", n, len(out))
		}
	}
}

func TestItoaSize(t *testing.T) {
	if itoaSize(12) != "12" {
		t.Fatal(itoaSize(12))
	}
	if !strings.HasSuffix(itoaSize(2500), "k") {
		t.Fatal(itoaSize(2500))
	}
}

func TestEchoToolEventColors(t *testing.T) {
	var buf bytes.Buffer
	t.Setenv("NO_COLOR", "1")
	echoToolEvent(&buf, observability.Event{
		Type: observability.EventToolStarted,
		Data: observability.ToolEventData{Tool: "read_file", Arguments: `{"path":"a"}`},
	})
	echoToolEvent(&buf, observability.Event{
		Type: observability.EventToolFinished,
		Data: observability.ToolEventData{Tool: "read_file", ResultSize: 42, DurationMS: 3},
	})
	out := buf.String()
	if !strings.Contains(out, "read_file") || !strings.Contains(out, "ok") {
		t.Fatalf("out = %q", out)
	}
}
