// Command codeflux-spike serves the retained Milestone 06 browser conformance
// fixture on a loopback-only random port.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"

	"codeflux.dev/codeflux/internal/transportspike"
)

func main() {
	assets := flag.String("assets", ".artifacts/m06-gwc-shell", "generated GWC asset directory")
	listenAddress := flag.String("listen", "127.0.0.1:0", "literal loopback listen address")
	launchSecretFile := flag.String("launch-secret-file", "", "optional private file used to preserve the launch secret across restarts")
	flag.Parse()

	launchSecret, err := resolveLaunchSecret(*launchSecretFile)
	if err != nil {
		log.Fatal(err)
	}
	handler, _, err := transportspike.NewApplicationHandler(*assets, launchSecret)
	if err != nil {
		log.Fatal(err)
	}
	listener, err := transportspike.ListenLoopbackAt(*listenAddress)
	if err != nil {
		log.Fatal(err)
	}
	server := transportspike.NewHTTPServer(handler)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	go func() {
		<-ctx.Done()
		_ = server.Shutdown(context.Background())
	}()
	fmt.Printf("codeflux-spike: ready at %s\n", transportspike.OriginForListener(listener))
	if err := server.Serve(listener); err != nil && err != http.ErrServerClosed {
		log.Fatal(err)
	}
}

func newLaunchSecret() (string, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return "", fmt.Errorf("generate launch secret: %w", err)
	}
	return hex.EncodeToString(secret), nil
}

func resolveLaunchSecret(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return newLaunchSecret()
	}

	secretBytes, err := os.ReadFile(path)
	if err == nil {
		secret := strings.TrimSpace(string(secretBytes))
		if err := validateLaunchSecret(secret); err != nil {
			return "", fmt.Errorf("read launch secret file: %w", err)
		}
		return secret, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read launch secret file: %w", err)
	}

	secret, err := newLaunchSecret()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return "", fmt.Errorf("create launch secret directory: %w", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return "", fmt.Errorf("create launch secret file: %w", err)
	}
	if _, err := file.WriteString(secret + "\n"); err != nil {
		_ = file.Close()
		return "", fmt.Errorf("write launch secret file: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close launch secret file: %w", err)
	}
	return secret, nil
}

func validateLaunchSecret(secret string) error {
	decoded, err := hex.DecodeString(secret)
	if err != nil || len(decoded) != 32 {
		return errors.New("launch secret must be 32 random bytes encoded as hexadecimal")
	}
	return nil
}
