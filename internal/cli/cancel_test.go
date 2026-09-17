package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestReportTurnErrorCancelled(t *testing.T) {
	app, _ := newTestAppWithWorkspace(t)
	var buf bytes.Buffer
	app.out = &buf

	app.reportTurnError(context.Canceled)
	if !strings.Contains(buf.String(), "cancelled") || !strings.Contains(buf.String(), "session still alive") {
		t.Fatalf("cancel msg = %q", buf.String())
	}

	buf.Reset()
	app.reportTurnError(errors.New("boom"))
	if !strings.Contains(buf.String(), "error: boom") {
		t.Fatalf("error msg = %q", buf.String())
	}

	buf.Reset()
	app.reportTurnError(nil)
	if buf.String() != "" {
		t.Fatalf("nil err should print nothing, got %q", buf.String())
	}
}

func TestBeginTurnCancelDoesNotPoisonParent(t *testing.T) {
	parent := context.Background()
	turn, stop := beginTurn(parent)
	stop()
	if turn.Err() == nil {
		t.Fatal("turn should be cancelled after stop")
	}
	if parent.Err() != nil {
		t.Fatal("parent must stay alive")
	}

	// A new turn from the same parent must work.
	turn2, stop2 := beginTurn(parent)
	defer stop2()
	if turn2.Err() != nil {
		t.Fatalf("second turn ctx = %v", turn2.Err())
	}
}
