// Package paths resolves where MinCode stores runtime data
// (traces, sessions, experiments) for a given workspace.
package paths

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// LocationGlobal stores data under the user home data root.
const LocationGlobal = "global"

// LocationWorkspace stores data under <workspace>/.mincode.
const LocationWorkspace = "workspace"

// Layout is the resolved data directories for one workspace.
// Only these paths are used — no legacy workspace/.mincode fallback.
type Layout struct {
	Location    string
	DataRoot    string
	ProjectID   string
	ProjectDir  string
	TracesDir   string
	SessionsDir string
	Experiments string
}

// UserHomeDataRoot returns {home}/.mincode.
func UserHomeDataRoot() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".", ".mincode-user")
	}
	return filepath.Join(home, ".mincode")
}

// NormalizeForID canonicalizes a path for hashing (stable across slash/case on Windows).
func NormalizeForID(p string) string {
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	p = filepath.Clean(p)
	p = filepath.ToSlash(p)
	p = strings.TrimRight(p, "/")
	if runtime.GOOS == "windows" {
		p = strings.ToLower(p)
	}
	return p
}

// SlugName returns a filesystem-safe name from the workspace basename.
func SlugName(workspace string) string {
	abs, err := filepath.Abs(workspace)
	if err != nil || abs == "" {
		abs = workspace
	}
	base := filepath.Base(filepath.Clean(abs))
	if base == "" || base == "." || base == string(filepath.Separator) {
		base = "workspace"
	}
	if runtime.GOOS == "windows" && len(base) == 2 && base[1] == ':' {
		base = "drive-" + strings.ToLower(string(base[0]))
	}
	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		default:
			b.WriteByte('-')
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		s = "workspace"
	}
	if len(s) > 40 {
		s = s[:40]
	}
	return s
}

// ProjectID returns slug + first 8 hex of SHA-256(normalized abs path).
// Avoids collisions like D:\A-B\C vs D:\A\B-C.
func ProjectID(workspace string) string {
	norm := NormalizeForID(workspace)
	sum := sha256.Sum256([]byte(norm))
	return SlugName(workspace) + "-" + hex.EncodeToString(sum[:])[:8]
}

// Resolve computes the data layout for a workspace.
// location: LocationGlobal (default) or LocationWorkspace.
// dataRoot: override for global root; empty → UserHomeDataRoot().
func Resolve(workspace, location, dataRoot string) Layout {
	if workspace == "" {
		workspace = "."
	}
	absWS, err := filepath.Abs(workspace)
	if err == nil {
		workspace = absWS
	}
	loc := location
	if loc == "" {
		loc = LocationGlobal
	}
	if dataRoot == "" {
		dataRoot = UserHomeDataRoot()
	}

	l := Layout{
		Location:  loc,
		ProjectID: ProjectID(workspace),
	}

	switch loc {
	case LocationWorkspace:
		l.DataRoot = filepath.Join(workspace, ".mincode")
		l.ProjectDir = l.DataRoot
	default: // global
		l.DataRoot = dataRoot
		l.ProjectDir = filepath.Join(dataRoot, "projects", l.ProjectID)
	}
	l.TracesDir = filepath.Join(l.ProjectDir, "traces")
	l.SessionsDir = filepath.Join(l.ProjectDir, "sessions")
	l.Experiments = filepath.Join(l.ProjectDir, "experiments")
	return l
}

// EnsureDirs creates data directories.
func (l Layout) EnsureDirs() error {
	for _, d := range []string{l.TracesDir, l.SessionsDir, l.Experiments} {
		if d == "" {
			continue
		}
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}
