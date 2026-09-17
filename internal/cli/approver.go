package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"

	"github.com/manxisuo/mincode/internal/permission"
	"github.com/manxisuo/mincode/internal/tools"
)

// StdinApprover asks y/n on the terminal for Ask-level tool calls.
type StdinApprover struct {
	mu  sync.Mutex
	in  *bufio.Scanner
	out io.Writer
	// WS is optional; when set, write/edit prompts include a diff preview.
	WS *tools.Workspace
	// AutoYes skips the prompt (for tests / --yes).
	AutoYes bool
	// AutoNo rejects everything (for tests).
	AutoNo bool
}

// NewStdinApprover creates an interactive approver.
func NewStdinApprover(out io.Writer, ws *tools.Workspace) *StdinApprover {
	sc := bufio.NewScanner(os.Stdin)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	return &StdinApprover{in: sc, out: out, WS: ws}
}

func (a *StdinApprover) Approve(req permission.Request) (bool, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.AutoNo {
		fmt.Fprintf(a.out, "%s %s → %s\n", yellow("?"), req.Summary, red("auto-denied"))
		return false, nil
	}
	if a.AutoYes {
		fmt.Fprintf(a.out, "%s %s → %s\n", yellow("?"), req.Summary, green("auto-approved"))
		return true, nil
	}

	fmt.Fprintf(a.out, "\n%s %s\n", yellow("Permission required:"), bold(req.Summary))
	fmt.Fprintf(a.out, "  tool=%s\n", req.Tool)
	if preview := a.changePreview(req); preview != "" {
		fmt.Fprintf(a.out, "\n%s\n", bold("Proposed change:"))
		fmt.Fprint(a.out, indentBlock(preview, "  "))
		fmt.Fprintln(a.out)
	} else if req.Arguments != "" {
		fmt.Fprintf(a.out, "  args=%s\n", gray(truncateStr(compactJSON(req.Arguments), 200)))
	}
	fmt.Fprintf(a.out, "%s approve this change? [%s/%s] ", cyan("?"), green("y"), red("n"))

	if a.in == nil || !a.in.Scan() {
		return false, fmt.Errorf("approval input closed")
	}
	line := strings.ToLower(strings.TrimSpace(a.in.Text()))
	switch line {
	case "y", "yes":
		fmt.Fprintf(a.out, "%s\n", green("approved"))
		return true, nil
	default:
		fmt.Fprintf(a.out, "%s\n", red("denied"))
		return false, nil
	}
}

func (a *StdinApprover) changePreview(req permission.Request) string {
	if a.WS == nil {
		return ""
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(req.Arguments), &args); err != nil {
		return ""
	}
	path, _ := args["path"].(string)
	switch req.Tool {
	case "edit_file":
		oldT, _ := args["old_text"].(string)
		newT, _ := args["new_text"].(string)
		diff, err := tools.PreviewEdit(a.WS, path, oldT, newT)
		if err != nil {
			return gray("(preview unavailable: " + err.Error() + ")")
		}
		return colorizeDiff(diff)
	case "write_file":
		content, _ := args["content"].(string)
		return colorizeDiff(tools.PreviewWrite(path, content))
	}
	return ""
}

func colorizeDiff(diff string) string {
	if !colorEnabled() {
		return diff
	}
	lines := strings.Split(diff, "\n")
	for i, ln := range lines {
		switch {
		case strings.HasPrefix(ln, "+++") || strings.HasPrefix(ln, "---"):
			lines[i] = bold(ln)
		case strings.HasPrefix(ln, "@@"):
			lines[i] = cyan(ln)
		case strings.HasPrefix(ln, "+"):
			lines[i] = green(ln)
		case strings.HasPrefix(ln, "-"):
			lines[i] = red(ln)
		default:
			lines[i] = dim(ln)
		}
	}
	return strings.Join(lines, "\n")
}

func indentBlock(s, prefix string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, ln := range lines {
		lines[i] = prefix + ln
	}
	return strings.Join(lines, "\n") + "\n"
}
