package cli

import (
	"os"
	"strings"
)

// ANSI colors. Disabled when NO_COLOR is set or stdout is not a terminal-like writer
// that we cannot detect easily — we still emit codes on modern Windows Terminal.
const (
	ansiReset   = "\x1b[0m"
	ansiBold    = "\x1b[1m"
	ansiDim     = "\x1b[2m"
	ansiRed     = "\x1b[31m"
	ansiGreen   = "\x1b[32m"
	ansiYellow  = "\x1b[33m"
	ansiBlue    = "\x1b[34m"
	ansiCyan    = "\x1b[36m"
	ansiWhite   = "\x1b[37m"
	ansiGray    = "\x1b[90m"
	ansiMagenta = "\x1b[35m"
)

func colorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if os.Getenv("MINICODE_COLOR") == "0" {
		return false
	}
	return true
}

func paint(code, s string) string {
	if !colorEnabled() || code == "" {
		return s
	}
	return code + s + ansiReset
}

func bold(s string) string    { return paint(ansiBold, s) }
func dim(s string) string     { return paint(ansiDim, s) }
func green(s string) string   { return paint(ansiGreen, s) }
func red(s string) string     { return paint(ansiRed, s) }
func cyan(s string) string    { return paint(ansiCyan, s) }
func yellow(s string) string  { return paint(ansiYellow, s) }
func gray(s string) string    { return paint(ansiGray, s) }
func blue(s string) string    { return paint(ansiBlue, s) }
func magenta(s string) string { return paint(ansiMagenta, s) }

func promptString() string {
	return bold(cyan("mincode")) + bold(yellow(">")) + " "
}

func toolOutLine(tool, status string, size int, dur string) string {
	arrow := cyan("→")
	statusCol := green(status)
	if status == "error" {
		statusCol = red(status)
	}
	return "  " + arrow + " " + bold(tool) + " " + gray(dur) +
		"\n  " + dim("←") + " " + statusCol + " " + gray(itoaSize(size)+" bytes") + " " + gray(dur)
}

func itoaSize(n int) string {
	if n < 1000 {
		return itoaInt(n)
	}
	// 1.2k style
	if n < 1_000_000 {
		return itoaInt(n/1000) + "." + itoaInt((n%1000)/100) + "k"
	}
	return itoaInt(n/1_000_000) + "M"
}

func itoaInt(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func bannerLine(label, value string) string {
	return bold(cyan(label)) + "=" + value
}

func colorizeToolArgs(args string) string {
	if !colorEnabled() {
		return args
	}
	return gray(args)
}

func stripANSI(s string) string {
	if !strings.Contains(s, "\x1b[") {
		return s
	}
	var b strings.Builder
	inEsc := false
	for i := 0; i < len(s); i++ {
		if s[i] == 0x1b {
			inEsc = true
			continue
		}
		if inEsc {
			if (s[i] >= 'a' && s[i] <= 'z') || (s[i] >= 'A' && s[i] <= 'Z') {
				inEsc = false
			}
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
