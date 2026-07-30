// Command codeflux-worker is the credential-free subprocess entry point for
// one active Codeflux task.
package main

import (
	"context"
	"os"
	"os/signal"

	"codeflux.dev/codeflux/internal/worker"
)

func main() {
	startup, err := worker.DecodeStartup(os.Stdin)
	if err != nil {
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	if err := worker.Run(ctx, startup, worker.ClientOptions{}); err != nil {
		os.Exit(1)
	}
}
