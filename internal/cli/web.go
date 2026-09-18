package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/manxisuo/mincode/internal/agent"
	"github.com/manxisuo/mincode/internal/config"
	"github.com/manxisuo/mincode/internal/instruction"
	"github.com/manxisuo/mincode/internal/memory"
	"github.com/manxisuo/mincode/internal/observability"
	"github.com/manxisuo/mincode/internal/permission"
	"github.com/manxisuo/mincode/internal/server"
	"github.com/manxisuo/mincode/internal/skill"
	"github.com/manxisuo/mincode/internal/tools"
)

// WebOptions configures `mincode web`.
type WebOptions struct {
	ConfigPath string
	Workspace  string
	Addr       string
	Model      string
	Provider   string
}

// localWebApprover auto-approves Ask-level tools for local single-user web mode.
// Destructive shell commands are still denied by ShellAwarePolicy before Approve.
type localWebApprover struct{}

func (localWebApprover) Approve(req permission.Request) (bool, error) {
	fmt.Fprintf(os.Stderr, "mincode web: auto-approved %s (%s)\n", req.Tool, req.Summary)
	return true, nil
}

// RunWeb starts the local Web Inspector (HTTP + SSE) on the workspace.
func RunWeb(ctx context.Context, w WebOptions) error {
	workspace := w.Workspace
	if workspace == "" {
		wd, err := os.Getwd()
		if err != nil {
			return err
		}
		workspace = wd
	}
	if abs, err := filepath.Abs(workspace); err == nil {
		workspace = abs
	}

	cfg, err := config.LoadFrom(w.ConfigPath, workspace)
	if err != nil {
		return err
	}
	if w.Model != "" {
		cfg.Provider.Model = w.Model
	}
	if w.Provider != "" {
		cfg.Provider.Type = w.Provider
	}

	provider, err := buildProvider(cfg)
	if err != nil {
		return err
	}

	sessionID := time.Now().UTC().Format("20060102-150405") + "-" + observability.NewID()[:8]
	if err := config.EnsureTraceDir(cfg.Trace.Dir); err != nil {
		return err
	}
	tracePath := config.TracePath(cfg.Trace.Dir, sessionID)
	recorder, err := observability.NewRecorder(tracePath)
	if err != nil {
		return err
	}
	defer func() { _ = recorder.Close() }()

	bus := observability.NewBus()
	metrics := observability.NewMetricsCollector()
	bus.Subscribe(func(e observability.Event) {
		if err := recorder.Record(e); err != nil {
			fmt.Fprintf(os.Stderr, "trace write error: %v\n", err)
		}
	})
	bus.Subscribe(metrics.Handle)

	ws, err := tools.NewWorkspace(workspace)
	if err != nil {
		return err
	}
	memStore, err := memory.New(workspace)
	if err != nil {
		return err
	}

	registry := tools.NewRegistry()
	registry.Register(&tools.ReadFile{WS: ws})
	registry.Register(&tools.ListDir{WS: ws})
	registry.Register(&tools.Glob{WS: ws})
	registry.Register(&tools.Grep{WS: ws})
	registry.Register(&tools.WriteFile{WS: ws})
	registry.Register(&tools.EditFile{WS: ws})
	registry.Register(&tools.Shell{WS: ws})

	sysPrompt := cfg.Agent.SystemPrompt + config.PlatformShellHint(runtime.GOOS)
	ag := agent.NewWithCompress(provider, registry, bus, sessionID, cfg.Agent.MaxSteps,
		sysPrompt, cfg.Agent.TokenBudget, cfg.Agent.CompressAt)
	if cfg.Agent.ParallelTools != nil {
		ag.ParallelTools = *cfg.Agent.ParallelTools
	}
	ag.MaxParallel = cfg.Agent.MaxParallel
	ag.Stream = cfg.StreamEnabled()
	ag.Approver = localWebApprover{}

	registry.Register(&tools.MemoryAdd{
		Store: memStore,
		OnAdded: func(entry, composed string) {
			ag.Ctx.SetMemory(composed)
		},
	})

	instrLoader, err := instruction.NewLoader(workspace)
	if err == nil {
		ag.Instr = instrLoader
		if f, err := instrLoader.LoadRoot(); err == nil && f != nil {
			ag.Ctx.SetInstructions(instrLoader.Compose())
			bus.Publish(observability.NewEvent(sessionID, 0, observability.EventInstructionLoaded,
				observability.InstructionLoadedData{Path: f.Path, RelPath: f.RelPath, RelDir: f.RelDir, Bytes: len(f.Content)}))
		}
	}
	if skillLoader, err := skill.NewLoader(workspace); err == nil {
		if _, err := skillLoader.Discover(); err != nil {
			fmt.Fprintf(os.Stderr, "mincode web: discover skills: %v\n", err)
		}
	}
	if content, existed, err := memStore.Load(); err == nil && existed && content != "" {
		ag.Ctx.SetMemory(memStore.Compose())
	}

	addr := w.Addr
	if addr == "" {
		addr = "127.0.0.1:8080"
	}

	srv := server.New(server.Options{
		Addr:      addr,
		Workspace: workspace,
		SessionID: sessionID,
		Provider:  provider.Name(),
		Model:     provider.Model(),
	}, ag, bus, metrics)

	bus.Publish(observability.NewEvent(sessionID, 0, observability.EventSessionCreated,
		observability.SessionCreatedData{
			Workspace: workspace,
			Model:     provider.Model(),
			Provider:  provider.Name(),
		}))

	fmt.Printf("MinCode Web Inspector\n")
	fmt.Printf("  workspace  %s\n", workspace)
	fmt.Printf("  provider   %s / %s\n", provider.Name(), provider.Model())
	fmt.Printf("  session    %s\n", sessionID)
	fmt.Printf("  trace      %s\n", tracePath)
	fmt.Printf("  listening  http://%s\n", addr)
	fmt.Printf("  note       W1 本地单用户；Ask 级写操作自动批准（危险 shell 仍拒绝）\n\n")

	return srv.ListenAndServe(ctx)
}
