// Command howlforge is the HowlForge command line interface.
//
// HowlForge defines an AI workforce and selects candidates to fill its roles.
// It never launches a worker: selection is separated from execution so that
// HowlPlane, not HowlForge, decides what actually runs.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/howlcipher/howlforge/internal/cli"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	os.Exit(cli.Run(ctx, os.Args[1:], os.Stdout, os.Stderr))
}
