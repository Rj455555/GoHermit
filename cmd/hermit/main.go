package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/Rj455555/GoHermit/internal/app"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if len(os.Args) > 1 && os.Args[1] == "loop" {
		os.Exit(runLoop(ctx, os.Stdout, os.Stderr, os.Args[2:]))
	}
	os.Exit((app.CLI{Stdout: os.Stdout, Stderr: os.Stderr}).Run(ctx, os.Args[1:]))
}
