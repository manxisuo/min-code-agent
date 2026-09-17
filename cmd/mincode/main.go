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
  Saved to <workspace>/.mincode/sessions/<id>.json after each turn

Experiments:
  Results in <workspace>/.mincode/experiments/<name>/

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
