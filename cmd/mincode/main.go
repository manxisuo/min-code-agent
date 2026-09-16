// mincode is a learning-oriented Code Agent runtime.
//
// Phase 0+1: CLI REPL, OpenAI-compatible / Fake provider, Event Bus, JSONL trace.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/mincode/mincode/internal/cli"
)

func main() {
	opts := cli.Options{}
	flag.StringVar(&opts.ConfigPath, "config", "", "path to mincode.yaml")
	flag.StringVar(&opts.Prompt, "p", "", "run a single prompt and exit")
	flag.StringVar(&opts.Prompt, "prompt", "", "run a single prompt and exit")
	flag.StringVar(&opts.Model, "model", "", "override model name")
	flag.StringVar(&opts.Provider, "provider", "", "override provider type (openai-compatible|fake)")
	flag.StringVar(&opts.Workspace, "workspace", "", "workspace directory (default: cwd)")
	flag.Usage = func() {
		fmt.Fprintf(flag.CommandLine.Output(), `Min Code Agent — learning-oriented Code Agent runtime

Usage:
  mincode [flags]

Flags:
`)
		flag.PrintDefaults()
		fmt.Fprintf(flag.CommandLine.Output(), `
Examples:
  mincode
  mincode -p "hello"
  mincode --provider fake -p "offline demo"
  mincode --config ./mincode.yaml

REPL commands:
  /help  /timeline  /trace [n]  /metrics  /clear  /exit
`)
	}
	flag.Parse()

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
