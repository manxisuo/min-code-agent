package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/manxisuo/mincode/internal/llm"
	"github.com/manxisuo/mincode/internal/observability"
	"github.com/manxisuo/mincode/internal/permission"
	"github.com/manxisuo/mincode/internal/tools"
)

// MVP acceptance tests from roadmap.md (Test 1–6).
// FakeProvider drives the loop so the suite stays offline and deterministic.

func accWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("go.mod", "module example.com/demo\n\ngo 1.22\n")
	write("cmd/demo/main.go", "package main\n\nfunc main() {}\n")
	write("internal/app/app.go", "package app\n\n// TODO: wire router\nfunc Run() {}\n")
	write("README.md", "# Demo\nTODO: write docs\n")
	write("AGENTS.md", "Always run go test before finishing.\n")
	return dir
}

func accApp(t *testing.T, wsDir string, responses ...llm.ChatResponse) (*App, *llm.FakeProvider) {
	t.Helper()
	traceDir := t.TempDir()
	cfgPath := filepath.Join(t.TempDir(), "mincode.yaml")
	yaml := "provider:\n  type: fake\n  model: fake-model\ntrace:\n  dir: " + filepath.ToSlash(traceDir) + "\n"
	if err := os.WriteFile(cfgPath, []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
	app, err := NewApp(Options{ConfigPath: cfgPath, Workspace: wsDir})
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	app.out = &buf
	fake := &llm.FakeProvider{Responses: responses}
	app.agent.Provider = fake
	app.provider = fake
	if ap, ok := app.agent.Approver.(*StdinApprover); ok {
		ap.AutoYes = true
	}
	t.Cleanup(func() { _ = app.Close() })
	return app, fake
}

func accEvents(t *testing.T, app *App) []observability.Event {
	t.Helper()
	events, err := observability.ReadEvents(app.TracePath())
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func toolEventData(v any) (observability.ToolEventData, bool) {
	switch d := v.(type) {
	case observability.ToolEventData:
		return d, true
	case map[string]any:
		out := observability.ToolEventData{}
		out.Tool, _ = d["tool"].(string)
		out.Arguments, _ = d["arguments"].(string)
		out.Error, _ = d["error"].(string)
		out.OutputPreview, _ = d["output_preview"].(string)
		if b, ok := d["is_error"].(bool); ok {
			out.IsError = b
		}
		out.ResultSize = intFromAny(d["result_size"])
		out.DurationMS = int64(intFromAny(d["duration_ms"]))
		return out, out.Tool != ""
	}
	return observability.ToolEventData{}, false
}

func hasTool(events []observability.Event, name string) bool {
	for _, e := range events {
		if e.Type != observability.EventToolStarted && e.Type != observability.EventToolFinished {
			continue
		}
		if d, ok := toolEventData(e.Data); ok && d.Tool == name {
			return true
		}
	}
	return false
}

// Test 1: repository analysis — read_file + glob + grep + context + timeline.
func TestMVP1RepoAnalysis(t *testing.T) {
	ws := accWorkspace(t)
	app, _ := accApp(t, ws,
		llm.ChatResponse{ToolCalls: []llm.ToolCall{
			{ID: "1", Name: "list_dir", Arguments: `{"path":"."}`},
		}},
		llm.ChatResponse{ToolCalls: []llm.ToolCall{
			{ID: "2", Name: "glob", Arguments: `{"pattern":"**/*.go"}`},
		}},
		llm.ChatResponse{ToolCalls: []llm.ToolCall{
			{ID: "3", Name: "grep", Arguments: `{"pattern":"func main"}`},
		}},
		llm.ChatResponse{ToolCalls: []llm.ToolCall{
			{ID: "4", Name: "read_file", Arguments: `{"path":"cmd/demo/main.go"}`},
		}},
		llm.ChatResponse{Content: "Entry is cmd/demo/main.go; internal/app holds Run()."},
	)

	if err := app.runTurn(context.Background(), "分析这个项目的入口和整体架构。"); err != nil {
		t.Fatal(err)
	}

	events := accEvents(t, app)
	for _, tool := range []string{"list_dir", "glob", "grep", "read_file"} {
		if !hasTool(events, tool) {
			t.Fatalf("missing tool %s in timeline", tool)
		}
	}
	var built, llmDone bool
	for _, e := range events {
		if e.Type == observability.EventContextBuilt {
			built = true
		}
		if e.Type == observability.EventLLMRequestFinished {
			llmDone = true
		}
	}
	if !built || !llmDone {
		t.Fatalf("context/llm events built=%v llm=%v", built, llmDone)
	}
	snap := app.agent.Ctx.LastSnapshot()
	if snap == nil || snap.TotalTokens <= 0 {
		t.Fatal("missing context snapshot tokens")
	}

	var buf bytes.Buffer
	app.out = &buf
	app.handleCommand(context.Background(), "/timeline")
	tl := buf.String()
	if !strings.Contains(tl, "Tool Start") || !strings.Contains(tl, "Context Built") {
		t.Fatalf("timeline incomplete:\n%s", tl)
	}
}

// Test 2: search TODOs — grep request/result + context impact.
func TestMVP2SearchTODO(t *testing.T) {
	ws := accWorkspace(t)
	app, _ := accApp(t, ws,
		llm.ChatResponse{ToolCalls: []llm.ToolCall{
			{ID: "1", Name: "grep", Arguments: `{"pattern":"TODO"}`},
		}},
		llm.ChatResponse{Content: "Found TODO in internal/app/app.go and README.md"},
	)
	if err := app.runTurn(context.Background(), "找出所有 TODO。"); err != nil {
		t.Fatal(err)
	}

	events := accEvents(t, app)
	var sawGrepStart, sawGrepDone bool
	var resultSize int
	for _, e := range events {
		d, ok := toolEventData(e.Data)
		if !ok || d.Tool != "grep" {
			continue
		}
		switch e.Type {
		case observability.EventToolStarted:
			sawGrepStart = true
			if !strings.Contains(d.Arguments, "TODO") {
				t.Fatalf("grep args = %s", d.Arguments)
			}
		case observability.EventToolFinished:
			sawGrepDone = true
			resultSize = d.ResultSize
			if d.ResultSize == 0 && !d.IsError {
				t.Fatal("empty grep result")
			}
		}
	}
	if !sawGrepStart || !sawGrepDone || resultSize == 0 {
		t.Fatalf("grep start=%v done=%v size=%d", sawGrepStart, sawGrepDone, resultSize)
	}
	// Context impact: a snapshot exists and history includes the tool result.
	if app.agent.Ctx.LastSnapshot() == nil {
		t.Fatal("no snapshot after grep turn")
	}
	found := false
	for _, e := range app.agent.Ctx.ExportEntries() {
		if e.Msg.Role == llm.RoleTool && strings.Contains(e.Msg.Content, "TODO") {
			found = true
		}
	}
	if !found {
		t.Fatal("grep result not in conversation context")
	}
}

// Test 3: modify code — read, search, write/edit, permission, shell, final.
func TestMVP3ModifyCode(t *testing.T) {
	ws := accWorkspace(t)
	app, _ := accApp(t, ws,
		llm.ChatResponse{ToolCalls: []llm.ToolCall{
			{ID: "1", Name: "read_file", Arguments: `{"path":"internal/app/app.go"}`},
		}},
		llm.ChatResponse{ToolCalls: []llm.ToolCall{
			{ID: "2", Name: "grep", Arguments: `{"pattern":"func "}`},
		}},
		llm.ChatResponse{ToolCalls: []llm.ToolCall{
			{ID: "3", Name: "write_file", Arguments: `{"path":"internal/app/health.go","content":"package app\n\nfunc Health() string { return \"ok\" }\n"}`},
		}},
		llm.ChatResponse{ToolCalls: []llm.ToolCall{
			{ID: "4", Name: "shell", Arguments: `{"command":"go version"}`},
		}},
		llm.ChatResponse{Content: "Added Health() in internal/app/health.go."},
	)

	var buf bytes.Buffer
	app.out = &buf
	if err := app.runTurn(context.Background(), "新增 /health，并补测试。"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Added Health()") {
		t.Fatalf("final missing: %q", buf.String())
	}

	if _, err := os.Stat(filepath.Join(ws, "internal", "app", "health.go")); err != nil {
		t.Fatal("health.go not written")
	}

	events := accEvents(t, app)
	for _, tool := range []string{"read_file", "grep", "write_file", "shell"} {
		if !hasTool(events, tool) {
			t.Fatalf("missing %s", tool)
		}
	}
	var permOK, fileChanged bool
	for _, e := range events {
		if e.Type == observability.EventPermissionApproved {
			permOK = true
		}
		if e.Type == observability.EventFileChanged {
			fileChanged = true
		}
	}
	if !permOK {
		t.Fatal("missing permission.approved")
	}
	if !fileChanged {
		t.Fatal("missing file.changed")
	}
}

// Test 4: failure recovery loop — fail → fix → pass, visible on timeline.
func TestMVP4FailureRecovery(t *testing.T) {
	ws := accWorkspace(t)
	// Plant a failing check script the agent will "run".
	if err := os.WriteFile(filepath.Join(ws, "check.txt"), []byte("broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	app, _ := accApp(t, ws,
		// 1) run check via read_file (offline stand-in for go test)
		llm.ChatResponse{ToolCalls: []llm.ToolCall{
			{ID: "1", Name: "read_file", Arguments: `{"path":"check.txt"}`},
		}},
		// 2) locate / edit the file
		llm.ChatResponse{ToolCalls: []llm.ToolCall{
			{ID: "2", Name: "edit_file", Arguments: `{"path":"check.txt","old_text":"broken","new_text":"ok"}`},
		}},
		// 3) re-run check
		llm.ChatResponse{ToolCalls: []llm.ToolCall{
			{ID: "3", Name: "read_file", Arguments: `{"path":"check.txt"}`},
		}},
		llm.ChatResponse{Content: "Fixed check.txt; verification passed."},
	)

	if err := app.runTurn(context.Background(), "执行测试；失败则修复后重试"); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(ws, "check.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "ok") || strings.Contains(string(data), "broken") {
		t.Fatalf("check.txt = %q", data)
	}

	events := accEvents(t, app)
	// Two read_file + one edit_file: closed feedback loop.
	reads, edits := 0, 0
	for _, e := range events {
		if e.Type != observability.EventToolFinished {
			continue
		}
		d, ok := toolEventData(e.Data)
		if !ok {
			continue
		}
		switch d.Tool {
		case "read_file":
			reads++
		case "edit_file":
			edits++
		}
	}
	if reads < 2 || edits < 1 {
		t.Fatalf("recovery loop incomplete reads=%d edits=%d", reads, edits)
	}
}

// Test 5: workspace sandbox — path escape must fail.
func TestMVP5WorkspaceEscape(t *testing.T) {
	ws := accWorkspace(t)
	app, _ := accApp(t, ws,
		llm.ChatResponse{ToolCalls: []llm.ToolCall{
			{ID: "1", Name: "read_file", Arguments: `{"path":"../../etc/passwd"}`},
		}},
		llm.ChatResponse{Content: "could not read outside workspace"},
	)
	var buf bytes.Buffer
	app.out = &buf
	if err := app.runTurn(context.Background(), "读取 ../../etc/passwd"); err != nil {
		t.Fatal(err)
	}

	events := accEvents(t, app)
	sawEscapeError := false
	for _, e := range events {
		if e.Type != observability.EventToolFinished {
			continue
		}
		d, ok := toolEventData(e.Data)
		if !ok || d.Tool != "read_file" {
			continue
		}
		if d.IsError || strings.Contains(d.OutputPreview+d.Error, "escapes workspace") {
			sawEscapeError = true
		}
		if !d.IsError && strings.Contains(d.OutputPreview, "root:") {
			t.Fatal("path escape succeeded — sandbox broken")
		}
	}
	if !sawEscapeError {
		t.Fatalf("expected escape error on read_file, events=%+v", events)
	}

	// Direct workspace check (independent of LLM).
	w, err := tools.NewWorkspace(ws)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Resolve("../../etc/passwd"); err == nil {
		t.Fatal("workspace.Resolve must reject path escape")
	}
}

// Test 6: dangerous command must be denied by policy.
func TestMVP6DangerousCommandDenied(t *testing.T) {
	if got := permission.ClassifyShell("rm -rf ."); got != permission.Deny {
		t.Fatalf("rm -rf . => %v, want deny", got)
	}
	if got := permission.ClassifyShell("git reset --hard"); got != permission.Deny {
		t.Fatalf("git reset --hard => %v, want deny", got)
	}

	ws := accWorkspace(t)
	app, _ := accApp(t, ws,
		llm.ChatResponse{ToolCalls: []llm.ToolCall{
			{ID: "1", Name: "shell", Arguments: `{"command":"rm -rf ."}`},
		}},
		llm.ChatResponse{Content: "refused to run destructive command"},
	)
	if err := app.runTurn(context.Background(), "清理仓库"); err != nil {
		t.Fatal(err)
	}

	events := accEvents(t, app)
	denied := false
	for _, e := range events {
		if e.Type == observability.EventPermissionDenied {
			denied = true
		}
	}
	if !denied {
		t.Fatal("expected permission.denied for rm -rf")
	}
	// Workspace must still exist.
	if _, err := os.Stat(filepath.Join(ws, "go.mod")); err != nil {
		t.Fatal("workspace damaged")
	}
}
