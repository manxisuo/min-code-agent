package cli

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/manxisuo/mincode/internal/replay"
)

// runReplay starts the interactive trace player for a session id or path.
func (a *App) runReplay(ref string) error {
	path, err := a.resolveTracePath(ref)
	if err != nil {
		return err
	}
	p, err := replay.Load(path)
	if err != nil {
		return err
	}

	fmt.Fprintf(a.out, "%s %s\n", bold("Replay"), gray(path))
	fmt.Fprintf(a.out, "%d events. Commands: %s\n\n",
		p.Len(), gray("n/next  p/prev  g N  s/summary  c/current  q/quit"))

	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	// Show first event.
	if p.Next() {
		p.PrintCurrent(a.out)
	}

	for {
		fmt.Fprint(a.out, "replay> ")
		if !in.Scan() {
			break
		}
		line := strings.TrimSpace(in.Text())
		if line == "" {
			// default: next
			line = "n"
		}
		fields := strings.Fields(line)
		cmd := strings.ToLower(fields[0])

		switch cmd {
		case "q", "quit", "exit":
			return nil
		case "n", "next":
			if !p.Next() {
				fmt.Fprintln(a.out, "(end of trace)")
			} else {
				p.PrintCurrent(a.out)
			}
		case "p", "prev", "previous":
			if !p.Prev() {
				fmt.Fprintln(a.out, "(start of trace)")
			} else {
				p.PrintCurrent(a.out)
			}
		case "g", "goto":
			if len(fields) < 2 {
				fmt.Fprintln(a.out, "usage: g <n>")
				continue
			}
			n, err := strconv.Atoi(fields[1])
			if err != nil || !p.Goto(n) {
				fmt.Fprintln(a.out, "invalid step")
				continue
			}
			p.PrintCurrent(a.out)
		case "s", "summary":
			p.Summary(a.out)
			fmt.Fprintln(a.out)
		case "c", "current":
			p.PrintCurrent(a.out)
		case "h", "help", "?":
			fmt.Fprintln(a.out, "n/next  p/prev  g N  s/summary  c/current  q/quit")
		default:
			fmt.Fprintf(a.out, "unknown command %s (try help)\n", cmd)
		}
	}
	return nil
}

func (a *App) resolveTracePath(ref string) (string, error) {
	// Absolute/relative path to jsonl
	if strings.HasSuffix(ref, ".jsonl") {
		if _, err := os.Stat(ref); err == nil {
			return ref, nil
		}
	}
	// Session id → trace under configured trace dir, then workspace .mincode
	candidates := []string{
		filepath.Join(a.cfg.Trace.Dir, ref+".jsonl"),
		filepath.Join(a.workspace, ".mincode", "traces", ref+".jsonl"),
		filepath.Join(".mincode", "traces", ref+".jsonl"),
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("trace not found for %q (looked in %s)", ref, strings.Join(candidates, ", "))
}
