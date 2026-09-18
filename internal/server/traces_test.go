package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/manxisuo/mincode/internal/observability"
)

func TestTraceListAndShow(t *testing.T) {
	dir := t.TempDir()
	traces := filepath.Join(dir, "traces")
	if err := os.MkdirAll(traces, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(traces, "sess-1.jsonl")
	rec, err := observability.NewRecorder(path)
	if err != nil {
		t.Fatal(err)
	}
	ev := observability.NewEvent("sess-1", 1, observability.EventToolStarted, observability.ToolEventData{
		Tool: "read_file",
	})
	if err := rec.Record(ev); err != nil {
		t.Fatal(err)
	}
	if err := rec.Close(); err != nil {
		t.Fatal(err)
	}

	ag, bus, metrics := testAgent(t)
	srv := New(Options{SessionID: "sess-1", Workspace: t.TempDir(), TraceDir: traces}, ag, bus, metrics, nil, nil, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	res, err := http.Get(ts.URL + "/api/traces")
	if err != nil {
		t.Fatal(err)
	}
	var list map[string]any
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	items, _ := list["traces"].([]any)
	if len(items) != 1 {
		t.Fatalf("traces=%+v dir=%v", list["traces"], list["dir"])
	}

	res2, err := http.Get(ts.URL + "/api/traces/sess-1")
	if err != nil {
		t.Fatal(err)
	}
	var show map[string]any
	if err := json.NewDecoder(res2.Body).Decode(&show); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(show)
	res2.Body.Close()
	if res2.StatusCode != 200 {
		t.Fatalf("status=%d body=%s", res2.StatusCode, body)
	}
	events, _ := show["events"].([]any)
	if len(events) != 1 {
		t.Fatalf("events=%+v body=%s", show["events"], body)
	}
	_ = time.Now()
}
