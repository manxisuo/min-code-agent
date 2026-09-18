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

// LocationWorkspace stores data under <workspace>/.mincode (legacy).
const LocationWorkspace = "workspace"

// Layout is the resolved data directories for one workspace.
type Layout struct {
	Location    string
	DataRoot    string // user data root (global) or workspace/.mincode
	ProjectID   string
	ProjectDir  string
	TracesDir   string
	SessionsDir string
	Experiments string

	// Legacy locations (always under <workspace>/.mincode) for read compatibility.
	LegacyTraces      string
	LegacySessions    string
	LegacyExperiments string
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
	// Strip trailing slash except root-like "C:/"
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
	legacyRoot := filepath.Join(workspace, ".mincode")
	loc := location
	if loc == "" {
		loc = LocationGlobal
	}
	if dataRoot == "" {
		dataRoot = UserHomeDataRoot()
	}

	l := Layout{
		Location:          loc,
		ProjectID:         ProjectID(workspace),
		LegacyTraces:      filepath.Join(legacyRoot, "traces"),
		LegacySessions:    filepath.Join(legacyRoot, "sessions"),
		LegacyExperiments: filepath.Join(legacyRoot, "experiments"),
	}

	switch loc {
	case LocationWorkspace:
		l.DataRoot = legacyRoot
		l.ProjectDir = legacyRoot
		l.TracesDir = filepath.Join(legacyRoot, "traces")
		l.SessionsDir = filepath.Join(legacyRoot, "sessions")
		l.Experiments = filepath.Join(legacyRoot, "experiments")
		// Legacy == primary
		l.LegacyTraces = l.TracesDir
		l.LegacySessions = l.SessionsDir
		l.LegacyExperiments = l.Experiments
	default: // global
		l.DataRoot = dataRoot
		l.ProjectDir = filepath.Join(dataRoot, "projects", l.ProjectID)
		l.TracesDir = filepath.Join(l.ProjectDir, "traces")
		l.SessionsDir = filepath.Join(l.ProjectDir, "sessions")
		l.Experiments = filepath.Join(l.ProjectDir, "experiments")
	}
	return l
}

// EnsureDirs creates primary data directories.
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

// TraceSearchDirs returns directories to search when listing/reading traces
// (primary first, then legacy when different).
func (l Layout) TraceSearchDirs() []string {
	out := []string{l.TracesDir}
	if l.LegacyTraces != "" && !samePath(l.LegacyTraces, l.TracesDir) {
		out = append(out, l.LegacyTraces)
	}
	return out
}

// ExperimentSearchDirs returns experiment roots (primary, then legacy).
func (l Layout) ExperimentSearchDirs() []string {
	out := []string{l.Experiments}
	if l.LegacyExperiments != "" && !samePath(l.LegacyExperiments, l.Experiments) {
		out = append(out, l.LegacyExperiments)
	}
	return out
}

// SessionSearchDirs returns session store directories.
func (l Layout) SessionSearchDirs() []string {
	out := []string{l.SessionsDir}
	if l.LegacySessions != "" && !samePath(l.LegacySessions, l.SessionsDir) {
		out = append(out, l.LegacySessions)
	}
	return out
}

func samePath(a, b string) bool {
	if a == b {
		return true
	}
	aa, err1 := filepath.Abs(a)
	bb, err2 := filepath.Abs(b)
	if err1 != nil || err2 != nil {
		return false
	}
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(aa), filepath.Clean(bb))
	}
	return filepath.Clean(aa) == filepath.Clean(bb)
}
