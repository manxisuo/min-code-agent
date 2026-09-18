package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/manxisuo/mincode/internal/permission"
)

func permReq(tool, args, summary string) permission.Request {
	return permission.Request{Tool: tool, Arguments: args, Summary: summary}
}

func TestPermissionPendingAndDecide(t *testing.T) {
	ag, bus, metrics := testAgent(t)
	srv := New(Options{SessionID: "perm", Workspace: t.TempDir()}, ag, bus, metrics, nil, nil, nil)
	ag.Approver = srv.WebApprover()

	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	// Inject a pending permission via approver in background.
	done := make(chan bool, 1)
	go func() {
		// Use permission.Request through Approver
		ok, err := srv.WebApprover().Approve(permReq("write_file", `{"path":"x.txt","content":"hi"}`, "write_file x.txt"))
		if err != nil {
			t.Error(err)
		}
		done <- ok
	}()

	// Wait for pending
	var id string
	deadline := time.After(2 * time.Second)
	for id == "" {
		res, err := http.Get(ts.URL + "/api/permissions/pending")
		if err != nil {
			t.Fatal(err)
		}
		var body struct {
			Pending []struct {
				ID   string `json:"id"`
				Tool string `json:"tool"`
			} `json:"pending"`
		}
		_ = json.NewDecoder(res.Body).Decode(&body)
		res.Body.Close()
		if len(body.Pending) > 0 {
			id = body.Pending[0].ID
			break
		}
		select {
		case <-deadline:
			t.Fatal("timeout waiting pending permission")
		case <-time.After(20 * time.Millisecond):
		}
	}

	payload, _ := json.Marshal(map[string]bool{"allow": true})
	req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/permissions/"+id, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("decide status=%d", res.StatusCode)
	}

	select {
	case ok := <-done:
		if !ok {
			t.Fatal("expected allow=true")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Approve did not return")
	}
}
