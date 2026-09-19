package observability

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sync"
)

// Recorder appends events to a JSONL file, one event per line.
type Recorder struct {
	mu        sync.Mutex
	file      *os.File
	w         *bufio.Writer
	path      string
	unsynced  int
	syncEvery int
}

// NewRecorder opens (or creates) a JSONL trace file.
func NewRecorder(path string) (*Recorder, error) {
	if err := os.MkdirAll(dirOf(path), 0o755); err != nil {
		return nil, fmt.Errorf("create trace dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open trace file: %w", err)
	}
	return &Recorder{
		file:      f,
		w:         bufio.NewWriterSize(f, 32*1024),
		path:      path,
		syncEvery: 32,
	}, nil
}

// Path returns the underlying file path.
func (r *Recorder) Path() string {
	return r.path
}

// Record writes one event as a JSON line. The buffer is flushed so readers
// can see recent lines; fsync is batched to avoid blocking the agent loop
// on every stream delta.
func (r *Recorder) Record(e Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	line, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}
	if _, err := r.w.Write(line); err != nil {
		return fmt.Errorf("write event: %w", err)
	}
	if err := r.w.WriteByte('\n'); err != nil {
		return fmt.Errorf("write newline: %w", err)
	}
	if err := r.w.Flush(); err != nil {
		return fmt.Errorf("flush trace: %w", err)
	}
	r.unsynced++
	every := r.syncEvery
	if every <= 0 {
		every = 32
	}
	if r.unsynced >= every {
		_ = r.file.Sync()
		r.unsynced = 0
	}
	return nil
}

// Close flushes and closes the file.
func (r *Recorder) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.w != nil {
		if err := r.w.Flush(); err != nil {
			return err
		}
	}
	if r.file != nil {
		_ = r.file.Sync()
		return r.file.Close()
	}
	return nil
}

// ReadEvents loads all events from a JSONL trace file (for /trace and replay).
func ReadEvents(path string) ([]Event, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var events []Event
	sc := bufio.NewScanner(f)
	// Allow long tool-result lines later; 1MB per line is plenty for Phase 1.
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var e Event
		if err := json.Unmarshal(line, &e); err != nil {
			// Skip corrupt lines instead of failing the whole trace browse.
			continue
		}
		if e.Type == "" {
			continue
		}
		events = append(events, e)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return events, nil
}

func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' || path[i] == '\\' {
			return path[:i]
		}
	}
	return "."
}
