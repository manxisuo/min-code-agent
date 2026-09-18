// mincode is a learning-oriented Code Agent runtime.
//
// Phase 0–12: CLI REPL, providers, tools, context, session, skills, plan, memory, experiments.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/manxisuo/mincode/internal/cli"
)

func main() {
	enableVirtualTerminal()

	// Install the Windows console Ctrl+C handler before any stdin read so
	// the process is not terminated by the default handler.
	early := make(chan os.Signal, 1)
	signal.Notify(early, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(early)
	go func() {
		for range early {
		}
	}()

	// Batch experiment mode: mincode experiment run|list|show|compare
	if len(os.Args) > 1 && os.Args[1] == "experiment" {
		os.Exit(cli.RunExperimentCLI(os.Args[2:], os.Stdout))
	}

	// Local web inspector: mincode web [workspace]
	if len(os.Args) > 1 && os.Args[1] == "web" {
		wopts := cli.WebOptions{}
		wfs := flag.NewFlagSet("web", flag.ExitOnError)
		wfs.StringVar(&wopts.ConfigPath, "config", "", "path to mincode.yaml")
		wfs.StringVar(&wopts.Addr, "addr", "127.0.0.1:8080", "listen address")
		wfs.StringVar(&wopts.Model, "model", "", "override model name")
		wfs.StringVar(&wopts.Provider, "provider", "", "override provider type")
		wfs.StringVar(&wopts.Workspace, "workspace", "", "workspace directory (default: cwd)")
		wfs.Usage = func() {
			fmt.Fprintf(wfs.Output(), `Usage: mincode web [flags] [workspace-dir]

Start the local Web Inspector (HTTP + SSE) for this workspace.

Flags:
`)
			wfs.PrintDefaults()
		}
		_ = wfs.Parse(os.Args[2:])
		if wfs.NArg() > 0 && wopts.Workspace == "" {
			wopts.Workspace = wfs.Arg(0)
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := cli.RunWeb(ctx, wopts); err != nil && err != context.Canceled {
			fmt.Fprintf(os.Stderr, "mincode web: %v\n", err)
			os.Exit(1)
		}
		return
	}

	opts := cli.Options{}
	flag.StringVar(&opts.ConfigPath, "config", "", "path to mincode.yaml")
	flag.StringVar(&opts.Prompt, "p", "", "run a single prompt and exit")
	flag.StringVar(&opts.Prompt, "prompt", "", "run a single prompt and exit")
	flag.StringVar(&opts.Model, "model", "", "override model name")
	flag.StringVar(&opts.Provider, "provider", "", "override provider type (openai-compatible|fake)")
	flag.StringVar(&opts.Workspace, "workspace", "", "workspace directory (default: cwd)")
	flag.BoolVar(&opts.Continue, "continue", false, "restore the latest session for this workspace")
	flag.BoolVar(&opts.Continue, "c", false, "alias for --continue")
	flag.StringVar(&opts.Replay, "replay", "", "replay a session id or trace .jsonl path")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `Min Code Agent — learning-oriented Code Agent runtime

Usage:
  mincode [flags]
  mincode <workspace-dir> [flags]
  mincode replay <session-id>
  mincode --replay <session-id|.jsonl>
  mincode experiment run|list|show|compare
  mincode web [flags] [workspace-dir]

Flags:
`)
		flag.PrintDefaults()
		fmt.Fprintf(flag.CommandLine.Output(), `
Examples:
  mincode
  mincode D:\\Code\\myproject
  mincode --continue
  mincode -p "hello"
  mincode --replay 20260916-161234-d6b990c3
  mincode --provider fake -p "offline demo"
  mincode experiment run --name base --task "分析入口" --provider fake --repeat 2

Config resolution (when -config is omitted):
  <workspace>/mincode.yaml → <workspace>/mincode.yml → ./mincode.yaml

Sessions:
  Default (global): {home}/.mincode/projects/<project-id>/sessions/
  Or workspace mode: <workspace>/.mincode/sessions/  (data.location: workspace)

Experiments:
  Default (global): {home}/.mincode/projects/<project-id>/experiments/
  Or workspace mode: <workspace>/.mincode/experiments/

REPL commands:
  /help  /timeline  /context  /trace [n]  /metrics  /export  /exit
`)
	}
	flag.Parse()

	// Positional: mincode <workspace>  OR  mincode replay <session-id>
	if flag.NArg() > 0 {
		if flag.Arg(0) == "replay" {
			if flag.NArg() < 2 {
				fmt.Fprintln(os.Stderr, "usage: mincode replay <session-id>")
				os.Exit(2)
			}
			opts.Replay = flag.Arg(1)
		} else if opts.Workspace == "" {
			opts.Workspace = flag.Arg(0)
		}
	}

	app, err := cli.NewApp(opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mincode: %v\n", err)
		os.Exit(1)
	}
	defer app.Close()

	if err := app.Run(context.Background(), opts); err != nil {
		fmt.Fprintf(os.Stderr, "mincode: %v\n", err)
		os.Exit(1)
	}
}
