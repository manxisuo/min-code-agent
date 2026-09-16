// mincode is a learning-oriented Code Agent runtime.
//
// Phase 0–6: CLI REPL, providers, tools, context inspector, session, replay.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/mincode/mincode/internal/cli"
)

func main() {
	enableVirtualTerminal()

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

Config resolution (when -config is omitted):
  <workspace>/mincode.yaml → <workspace>/mincode.yml → ./mincode.yaml

Sessions:
  Saved to <workspace>/.mincode/sessions/<id>.json after each turn

REPL commands:
  /help  /timeline  /context  /trace [n]  /metrics  /clear  /exit
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
