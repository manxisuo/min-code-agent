package permission

import (
	"encoding/json"
	"regexp"
	"strings"
)

// shellAllowPrefixes are safe read-only / test commands (prefix match on first tokens).
var shellAllowPrefixes = []string{
	"go test",
	"go build",
	"go vet",
	"go list",
	"go env",
	"go version",
	"go fmt",
	"gofmt",
	"git status",
	"git diff",
	"git log",
	"git show",
	"git branch",
	"git remote",
	"git rev-parse",
	"npm test",
	"npm run test",
	"npm ls",
	"npm --version",
	"node --version",
	"cargo test",
	"cargo build",
	"cargo check",
	"cargo --version",
	"python -m pytest",
	"python -m unittest",
	"pytest",
	"ls",
	"dir",
	"pwd",
	"echo",
	"cat",
	"head",
	"tail",
	"wc",
	"which",
	"where",
	"whereis",
	"type",
	"find",
	"rg",
	"grep",
	"uname",
	"date",
	"whoami",
}

// shellDenyPatterns match destructive commands (case-insensitive).
var shellDenyPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(^|\s|&|;|\|)\s*rm\s+(-[a-zA-Z]*[rf][a-zA-Z]*\s+)+`),
	regexp.MustCompile(`(?i)\brm\s+-rf\b`),
	regexp.MustCompile(`(?i)\bgit\s+reset\s+--hard\b`),
	regexp.MustCompile(`(?i)\bgit\s+clean\s+-[a-zA-Z]*[f]`),
	regexp.MustCompile(`(?i)\bgit\s+push\s+.*--force`),
	regexp.MustCompile(`(?i)\bgit\s+push\s+-f\b`),
	regexp.MustCompile(`(?i)\bmkfs\b`),
	regexp.MustCompile(`(?i)\bdd\s+if=`),
	regexp.MustCompile(`(?i)\bformat\s+[a-z]:`),
	regexp.MustCompile(`(?i)\bdel\s+/[sfq]\b`),
	regexp.MustCompile(`(?i)\brmdir\s+/s\b`),
	regexp.MustCompile(`(?i)\bshutdown\b`),
	regexp.MustCompile(`(?i)\bdrop\s+table\b`),
	regexp.MustCompile(`(?i)\bcurl\s+[^|]*\|\s*(sh|bash|zsh)\b`),
	regexp.MustCompile(`(?i)\bwget\s+[^|]*\|\s*(sh|bash|zsh)\b`),
	// Workspace escape: shell cwd is sandboxed, but the command text is not.
	// File tools reject "../"; shell must not silently allow the same escape.
	regexp.MustCompile(`(?i)(^|[\s&|;])cd\s+(\.\.)([\s&|;]|$)`),
	regexp.MustCompile(`\.\.[\\/]`),
	regexp.MustCompile(`(^|[\s"'` + "`" + `])\.\.([\s"'` + "`" + `]|$)`),
}

// pathishAllowPrefixes take filesystem arguments; absolute paths outside the
// workspace must not be auto-approved even if the verb is "read-only".
var pathishAllowPrefixes = []string{
	"cat", "head", "tail", "type", "find", "grep", "rg", "ls", "dir", "less", "more",
}

// absPathPattern flags absolute paths in a command (unix or windows).
var absPathPattern = regexp.MustCompile(`(?i)(^|\s)(/[^\s]+|[a-z]:[\\/][^\s]*)`)

// ClassifyShell maps a shell command line to Allow / Ask / Deny.
func ClassifyShell(command string) Level {
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return Deny
	}
	lower := strings.ToLower(cmd)

	for _, re := range shellDenyPatterns {
		if re.MatchString(lower) || re.MatchString(cmd) {
			return Deny
		}
	}

	// Normalize whitespace for prefix checks.
	flat := strings.Join(strings.Fields(lower), " ")
	for _, p := range shellAllowPrefixes {
		if flat == p || strings.HasPrefix(flat, p+" ") {
			// Path-taking verbs with absolute paths require explicit approval.
			if isPathishPrefix(p) && absPathPattern.MatchString(flat) {
				return Ask
			}
			return Allow
		}
	}

	return Ask
}

func isPathishPrefix(p string) bool {
	for _, x := range pathishAllowPrefixes {
		if p == x {
			return true
		}
	}
	return false
}

// Evaluate for DefaultPolicy already handles tool-name levels.
// ShellClassifier augments it when the tool is shell.
type ShellAwarePolicy struct {
	Inner *DefaultPolicy
}

func (p *ShellAwarePolicy) Evaluate(req Request) Level {
	if req.Tool == "shell" {
		var args struct {
			Command string `json:"command"`
		}
		_ = json.Unmarshal([]byte(req.Arguments), &args)
		return ClassifyShell(args.Command)
	}
	if p.Inner != nil {
		return p.Inner.Evaluate(req)
	}
	return NewDefaultPolicy().Evaluate(req)
}
