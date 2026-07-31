// Command codeflux-local runs the loopback-only CodeFlux coordinator and its
// generated GWC frontend for development and browser verification.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"codeflux.dev/codeflux/internal/coordinator"
)

func main() {
	assetsFlag := flag.String(
		"assets",
		"",
		"generated GWC asset directory",
	)
	dataFlag := flag.String(
		"data",
		filepath.Join(".artifacts", "local-frontend"),
		"disposable local data directory",
	)
	listenFlag := flag.String(
		"listen",
		"127.0.0.1:0",
		"loopback frontend address",
	)
	flag.Parse()
	if *assetsFlag == "" {
		fmt.Fprintln(os.Stderr, "codeflux-local: -assets is required")
		os.Exit(2)
	}
	assets, err := filepath.Abs(*assetsFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "codeflux-local: resolve assets: %v\n", err)
		os.Exit(1)
	}
	data, err := filepath.Abs(*dataFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "codeflux-local: resolve data: %v\n", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(data, 0o700); err != nil {
		fmt.Fprintf(os.Stderr, "codeflux-local: create data directory: %v\n", err)
		os.Exit(1)
	}

	runContext, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()
	application, err := coordinator.StartApplication(
		runContext,
		coordinator.ApplicationOptions{
			DatabasePath:            filepath.Join(data, "codeflux.sqlite3"),
			BackupDirectory:         filepath.Join(data, "backups"),
			ListenAddress:           *listenFlag,
			FrontendAssetsDirectory: assets,
		},
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "codeflux-local: start: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf(
		"CODEFLUX_FRONTEND_URL=http://%s/\n",
		application.Address(),
	)
	<-runContext.Done()

	shutdownContext, cancel := context.WithTimeout(
		context.Background(),
		15*time.Second,
	)
	defer cancel()
	if err := application.Shutdown(shutdownContext); err != nil &&
		!errors.Is(err, context.Canceled) {
		fmt.Fprintf(os.Stderr, "codeflux-local: shutdown: %v\n", err)
		os.Exit(1)
	}
}
