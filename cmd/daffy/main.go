package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/gabriwl165/daffy/internal/cli"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.Run(ctx, cli.RunConfig{Args: os.Args[1:], Out: os.Stdout, ErrOut: os.Stderr, Deps: cli.DefaultDeps(), Version: version})
	stop()
	os.Exit(code)
}
