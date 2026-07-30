package worker

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"
)

// DefaultIsolationLabel is the user-facing security boundary of the local
// worker runner.
const DefaultIsolationLabel = "mediated workspace confinement, not a perfect sandbox"

// LaunchOptions are coordinator-owned subprocess inputs.
type LaunchOptions struct {
	Executable          string
	ExecutableArguments []string
	Startup             StartupParameters
	ParentEnvironment   []string
	AdditionalAllowed   []string
	AdditionalSensitive []string
	ContainerCommand    []string
}

// Process owns one task worker and its process-tree cancellation.
type Process struct {
	command  *exec.Cmd
	done     chan struct{}
	waitMu   sync.Mutex
	waitErr  error
	stopOnce sync.Once
}

// Launch starts one credential-free worker in its exact task worktree. Startup
// secrets cross an anonymous stdin pipe, not argv or environment.
func Launch(ctx context.Context, options LaunchOptions) (*Process, error) {
	if err := options.Startup.Validate(); err != nil {
		return nil, err
	}
	executable, arguments, err := buildWorkerCommand(options)
	if err != nil {
		return nil, err
	}
	command := exec.Command(executable, arguments...)
	command.Dir = options.Startup.WorktreePath
	command.Env, err = BuildMinimumWorkerEnvironment(
		options.ParentEnvironment,
		options.AdditionalAllowed,
		options.AdditionalSensitive,
	)
	if err != nil {
		return nil, err
	}
	command.Stdout = io.Discard
	command.Stderr = io.Discard
	prepareProcessTree(command)
	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, err
	}
	if err := command.Start(); err != nil {
		stdin.Close()
		return nil, err
	}
	if err := json.NewEncoder(stdin).Encode(options.Startup); err != nil {
		stdin.Close()
		_ = terminateProcessTree(command)
		_ = command.Wait()
		return nil, err
	}
	if err := stdin.Close(); err != nil {
		_ = terminateProcessTree(command)
		_ = command.Wait()
		return nil, err
	}
	process := &Process{command: command, done: make(chan struct{})}
	go func() {
		err := command.Wait()
		process.waitMu.Lock()
		process.waitErr = err
		process.waitMu.Unlock()
		close(process.done)
	}()
	go func() {
		select {
		case <-ctx.Done():
			_ = process.Stop()
		case <-process.done:
		}
	}()
	return process, nil
}

func (process *Process) PID() int {
	if process == nil || process.command == nil || process.command.Process == nil {
		return 0
	}
	return process.command.Process.Pid
}

func (process *Process) Wait(ctx context.Context) error {
	if process == nil {
		return errors.New("worker process is nil")
	}
	select {
	case <-process.done:
		process.waitMu.Lock()
		defer process.waitMu.Unlock()
		return process.waitErr
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (process *Process) Stop() error {
	if process == nil {
		return nil
	}
	var stopErr error
	process.stopOnce.Do(func() {
		stopErr = terminateProcessTree(process.command)
	})
	return stopErr
}

func DecodeStartup(reader io.Reader) (StartupParameters, error) {
	var startup StartupParameters
	if err := decodeSingleJSON(reader, maxStartupBytes, &startup); err != nil {
		return StartupParameters{}, err
	}
	if err := startup.Validate(); err != nil {
		return StartupParameters{}, err
	}
	return startup, nil
}

func buildWorkerCommand(options LaunchOptions) (string, []string, error) {
	if options.Executable == "" || strings.ContainsRune(options.Executable, 0) {
		return "", nil, errors.New("worker executable is required")
	}
	if !slices.Equal(options.ContainerCommand, options.Startup.ContainerCommand) {
		return "", nil, errors.New("worker container command must match startup metadata")
	}
	for _, argument := range options.ExecutableArguments {
		if strings.ContainsRune(argument, 0) {
			return "", nil, errors.New("worker executable argument is invalid")
		}
	}
	if len(options.ContainerCommand) == 0 {
		return options.Executable,
			append([]string(nil), options.ExecutableArguments...), nil
	}
	executable := options.ContainerCommand[0]
	arguments := append([]string(nil), options.ContainerCommand[1:]...)
	arguments = append(arguments, options.Executable)
	arguments = append(arguments, options.ExecutableArguments...)
	return executable, arguments, nil
}

// GracePeriod is the bounded checkpoint-and-stop window used by supervisors.
const GracePeriod = 5 * time.Second
