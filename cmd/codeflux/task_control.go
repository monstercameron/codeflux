package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/transport"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
)

const defaultSessionTokenEnvironment = "CODEFLUX_SESSION_TOKEN"

type taskControlArguments struct {
	endpoint        string
	taskID          domain.TaskID
	revision        uint64
	idempotencyKey  string
	reason          string
	sessionTokenEnv string
}

func runTaskControl(
	output io.Writer,
	errorsOutput io.Writer,
	command string,
	args []string,
) int {
	arguments, err := parseTaskControlArguments(command, args)
	if err != nil {
		fmt.Fprintf(errorsOutput, "codeflux %s: %v\n", command, err)
		return exitUsage
	}
	token := os.Getenv(arguments.sessionTokenEnv)
	if len(token) < 32 {
		fmt.Fprintf(
			errorsOutput,
			"codeflux %s: session token environment is unavailable\n",
			command,
		)
		return exitUnavailable
	}
	connection, err := grpc.NewClient(
		arguments.endpoint,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		fmt.Fprintf(errorsOutput, "codeflux %s: local coordinator unavailable\n", command)
		return exitUnavailable
	}
	defer connection.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ctx = metadata.AppendToOutgoingContext(
		ctx,
		transport.SessionMetadataKey,
		token,
	)
	control := &codefluxv1.MutationControl{
		IdempotencyKey:   arguments.idempotencyKey,
		ExpectedRevision: &arguments.revision,
	}
	taskIdentity, err := transport.TaskIDToProto(arguments.taskID)
	if err != nil {
		fmt.Fprintf(errorsOutput, "codeflux %s: invalid task identity\n", command)
		return exitUsage
	}
	client := codefluxv1.NewTaskServiceClient(connection)
	var task *codefluxv1.TaskView
	switch command {
	case "pause":
		response, callErr := client.PauseTask(
			ctx,
			&codefluxv1.PauseTaskRequest{
				Control: control,
				TaskId:  taskIdentity,
				Reason:  arguments.reason,
			},
		)
		if callErr != nil {
			err = callErr
		} else {
			task = response.GetTask()
		}
	case "resume":
		response, callErr := client.ResumeTask(
			ctx,
			&codefluxv1.ResumeTaskRequest{
				Control: control,
				TaskId:  taskIdentity,
			},
		)
		if callErr != nil {
			err = callErr
		} else {
			task = response.GetTask()
		}
	case "cancel":
		response, callErr := client.CancelTask(
			ctx,
			&codefluxv1.CancelTaskRequest{
				Control: control,
				TaskId:  taskIdentity,
				Reason:  arguments.reason,
			},
		)
		if callErr != nil {
			err = callErr
		} else {
			task = response.GetTask()
		}
	}
	if err != nil {
		fmt.Fprintf(errorsOutput, "codeflux %s: control request failed\n", command)
		return exitFailure
	}
	if task == nil {
		fmt.Fprintf(errorsOutput, "codeflux %s: coordinator returned no task\n", command)
		return exitFailure
	}
	fmt.Fprintf(
		output,
		"task: %s\nstate: %s\nrevision: %d\n",
		task.GetTaskId().GetValue(),
		task.GetState(),
		task.GetRevision(),
	)
	return exitOK
}

func parseTaskControlArguments(
	command string,
	args []string,
) (taskControlArguments, error) {
	parsed := taskControlArguments{
		sessionTokenEnv: defaultSessionTokenEnvironment,
	}
	var taskText string
	for index := 0; index < len(args); index++ {
		var target *string
		switch args[index] {
		case "--endpoint":
			target = &parsed.endpoint
		case "--task":
			target = &taskText
		case "--idempotency-key":
			target = &parsed.idempotencyKey
		case "--reason":
			target = &parsed.reason
		case "--session-token-env":
			target = &parsed.sessionTokenEnv
		case "--revision":
			index++
			if index >= len(args) {
				return taskControlArguments{}, errors.New(
					"--revision requires a value",
				)
			}
			revision, err := strconv.ParseUint(args[index], 10, 64)
			if err != nil {
				return taskControlArguments{}, errors.New(
					"--revision must be an unsigned integer",
				)
			}
			parsed.revision = revision
			continue
		default:
			return taskControlArguments{}, fmt.Errorf(
				"unknown argument %q",
				args[index],
			)
		}
		index++
		if index >= len(args) || args[index] == "" {
			return taskControlArguments{}, fmt.Errorf(
				"%s requires a value",
				args[index-1],
			)
		}
		*target = args[index]
	}
	if err := validateLoopbackEndpoint(parsed.endpoint); err != nil {
		return taskControlArguments{}, err
	}
	taskID, err := domain.ParseTaskID(taskText)
	if err != nil {
		return taskControlArguments{}, errors.New("--task is invalid")
	}
	parsed.taskID = taskID
	if parsed.revision == 0 {
		return taskControlArguments{}, errors.New("--revision is required")
	}
	if parsed.idempotencyKey == "" ||
		len(parsed.idempotencyKey) > 128 {
		return taskControlArguments{}, errors.New(
			"--idempotency-key is required and must not exceed 128 characters",
		)
	}
	if strings.TrimSpace(parsed.sessionTokenEnv) !=
		parsed.sessionTokenEnv ||
		parsed.sessionTokenEnv == "" {
		return taskControlArguments{}, errors.New(
			"--session-token-env is invalid",
		)
	}
	if command != "resume" && strings.TrimSpace(parsed.reason) == "" {
		return taskControlArguments{}, errors.New("--reason is required")
	}
	if len(parsed.reason) > 2048 {
		return taskControlArguments{}, errors.New("--reason is too long")
	}
	return parsed, nil
}

func validateLoopbackEndpoint(endpoint string) error {
	host, port, err := net.SplitHostPort(endpoint)
	if err != nil || port == "" {
		return errors.New("--endpoint must be a loopback host:port")
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	address := net.ParseIP(host)
	if address == nil || !address.IsLoopback() {
		return errors.New("--endpoint must use a loopback address")
	}
	return nil
}
