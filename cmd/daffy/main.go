package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/gabriwl165/daffy-duck/internal/cli"
)

var version = "dev"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	code := cli.RunVersion(ctx, os.Args[1:], os.Stdout, os.Stderr, cli.DefaultDeps(), version)
	stop()
	os.Exit(code)
}
