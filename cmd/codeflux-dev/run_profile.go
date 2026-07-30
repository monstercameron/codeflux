package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type deterministicRunResult struct {
	SchemaVersion int                `json:"schema_version"`
	Status        string             `json:"status"`
	Profile       developmentProfile `json:"profile"`
	Address       string             `json:"address"`
	Database      string             `json:"database"`
	Script        []string           `json:"script"`
}

func runDeterministicProfile(
	ctx context.Context,
	stdout io.Writer,
	stderr io.Writer,
	invocation commandInvocation,
) int {
	repository, err := repositoryRoot()
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev run: repository: %v\n", err)
		return exitFailure
	}
	profileRoot, err := resolveCommandRoot(repository, "run", invocation.Root)
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev run: root: %v\n", err)
		return exitUsage
	}
	if err := os.MkdirAll(profileRoot, 0o755); err != nil {
		fmt.Fprintf(stderr, "codeflux-dev run: create profile root: %v\n", err)
		return exitFailure
	}
	runRoot, err := os.MkdirTemp(profileRoot, "deterministic-")
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev run: create temporary run root: %v\n", err)
		return exitFailure
	}
	cleanup := isRepositoryArtifactChild(repository, runRoot)
	if cleanup {
		defer func() {
			if err := removeArtifactChild(repository, runRoot); err != nil {
				fmt.Fprintf(stderr, "codeflux-dev run: cleanup: %v\n", err)
			}
		}()
	}
	databasePath := filepath.Join(runRoot, "codeflux.sqlite")
	database, err := os.OpenFile(databasePath, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0o600)
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev run: create SQLite target: %v\n", err)
		return exitFailure
	}
	if err := database.Close(); err != nil {
		fmt.Fprintf(stderr, "codeflux-dev run: close SQLite target: %v\n", err)
		return exitFailure
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintf(stderr, "codeflux-dev run: listen on loopback: %v\n", err)
		return exitFailure
	}
	profile := developmentProfiles()[0]
	result := deterministicRunResult{
		SchemaVersion: 1,
		Status:        "ready",
		Profile:       profile,
		Address:       "http://" + listener.Addr().String(),
		Database:      databasePath,
		Script: []string{
			"provider.ready",
			"task.accepted",
			"provider.completed",
		},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		response.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(response).Encode(struct {
			Status  string `json:"status"`
			Profile string `json:"profile"`
		}{Status: "ok", Profile: profile.Name})
	})
	mux.HandleFunc("/profile", func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "application/json")
		response.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(response).Encode(result)
	})
	server := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       10 * time.Second,
	}
	serverError := make(chan error, 1)
	go func() {
		serverError <- server.Serve(listener)
	}()

	if invocation.JSON {
		if err := json.NewEncoder(stdout).Encode(result); err != nil {
			fmt.Fprintf(stderr, "codeflux-dev run: encode startup result: %v\n", err)
			return exitFailure
		}
	} else {
		fmt.Fprintln(stdout, "Codeflux deterministic profile ready")
		fmt.Fprintf(stdout, "Address: %s\n", result.Address)
		fmt.Fprintf(stdout, "SQLite target: %s\n", result.Database)
		fmt.Fprintln(stdout, "Credentials: in-memory fake")
		fmt.Fprintln(stdout, "Provider: fixed scripted; external network disabled")
	}

	if invocation.Once {
		if err := verifyDeterministicProfile(ctx, result.Address); err != nil {
			_ = server.Close()
			fmt.Fprintf(stderr, "codeflux-dev run: self-check: %v\n", err)
			return exitFailure
		}
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			fmt.Fprintf(stderr, "codeflux-dev run: shutdown: %v\n", err)
			return exitFailure
		}
		return exitSuccess
	}

	select {
	case <-ctx.Done():
		shutdownContext, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownContext); err != nil {
			fmt.Fprintf(stderr, "codeflux-dev run: shutdown: %v\n", err)
			return exitFailure
		}
		return exitSuccess
	case err := <-serverError:
		if err == http.ErrServerClosed {
			return exitSuccess
		}
		fmt.Fprintf(stderr, "codeflux-dev run: serve loopback profile: %v\n", err)
		return exitFailure
	}
}

func verifyDeterministicProfile(ctx context.Context, address string) error {
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			Proxy: nil,
		},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address+"/healthz", nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("health status %s", response.Status)
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, 4096))
	if err != nil {
		return err
	}
	if !strings.Contains(string(content), `"profile":"deterministic"`) {
		return fmt.Errorf("unexpected health response %q", content)
	}
	return nil
}

func isRepositoryArtifactChild(repository, target string) bool {
	artifactRoot, err := filepath.Abs(filepath.Join(repository, ".artifacts"))
	if err != nil {
		return false
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return false
	}
	relative, err := filepath.Rel(artifactRoot, target)
	return err == nil && relative != "." && relative != "" && relative != ".." &&
		!strings.HasPrefix(relative, ".."+string(filepath.Separator))
}
