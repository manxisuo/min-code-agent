package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPlanGetEmpty(t *testing.T) {
	ag, bus, metrics := testAgent(t)
	srv := New(Options{SessionID: "p1", Workspace: t.TempDir()}, ag, bus, metrics, nil, nil, nil)
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	res, err := http.Get(ts.URL + "/api/plan")
	if err != nil {
		t.Fatal(err)
	}
	var body map[string]any
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("status=%d body=%v", res.StatusCode, body)
	}
	if body["plan"] != nil {
		t.Fatalf("expected null plan, got %v", body["plan"])
	}
}
