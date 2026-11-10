// Command hyphal is the local-first outliner (§1). Until the CLI lands in P1-25
// it reports its version and runs the server.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/thorhanks/hyphal/internal/server"
)

// version is set at release time with -ldflags "-X main.version=…".
var version = "dev"

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--version" {
		fmt.Println("hyphal " + version)
		return
	}
	if len(os.Args) >= 2 && os.Args[1] == "serve" {
		if err := serve(os.Args[2:]); err != nil {
			fmt.Fprintln(os.Stderr, "hyphal:", err)
			os.Exit(1)
		}
		return
	}
	fmt.Fprintln(os.Stderr, "usage: hyphal --version | hyphal serve [--dev]")
	os.Exit(2)
}

// serve runs `hyphal serve [--dev] [--seed FILE]` in the foreground (§9.3).
// TODO(P1-25): a Cobra command; TODO(P1-30): the detached launch.
func serve(args []string) error {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	dev := fs.Bool("dev", false, "serve assets uncached; with a dev build, also the test hooks")
	seed := seedFlag(fs)
	if err := fs.Parse(args); err != nil {
		return err
	}
	var r io.Reader
	if seed != nil {
		var err error
		if r, err = seed(); err != nil {
			return err
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	defer stop()
	return server.Run(ctx, server.Options{Version: version, Dev: *dev, Seed: r})
}
