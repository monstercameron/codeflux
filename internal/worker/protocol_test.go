package worker

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

func TestStartupParametersAreVersionedLoopbackAndCredentialFree(t *testing.T) {
	taskID, _ := domain.NewTaskID()
	runID, _ := domain.NewRunID()
	parameters := StartupParameters{
		ProtocolVersion: ProtocolVersion, TaskID: taskID, RunID: runID,
		WorktreePath:   filepath.Join(t.TempDir(), "worktree"),
		PolicyRevision: 4, ToolSchemaVersion: 1,
		CoordinatorEndpoint: "http://127.0.0.1:43117",
		SessionToken:        "0123456789abcdef0123456789abcdef",
	}
	if err := parameters.Validate(); err != nil {
		t.Fatal(err)
	}
	parameters.ProtocolVersion++
	if err := parameters.Validate(); err == nil {
		t.Fatal("protocol mismatch was accepted")
	}
	parameters.ProtocolVersion = ProtocolVersion
	parameters.CoordinatorEndpoint = "https://example.com"
	if err := parameters.Validate(); err == nil {
		t.Fatal("non-loopback coordinator was accepted")
	}
	typ := reflect.TypeOf(StartupParameters{})
	for _, forbidden := range []string{"Credential", "APIKey", "SecretValue"} {
		if _, found := typ.FieldByName(forbidden); found {
			t.Fatalf("startup parameters expose %s", forbidden)
		}
	}
}

func TestMessageAuthenticationPayloadAndReconnectBounds(t *testing.T) {
	taskID, _ := domain.NewTaskID()
	runID, _ := domain.NewRunID()
	token := "0123456789abcdef0123456789abcdef"
	message := Message{
		ProtocolVersion: ProtocolVersion, TaskID: taskID, RunID: runID,
		Sequence: 1, SessionToken: token,
		Heartbeat: &Heartbeat{WorkerPID: 42, LeaseID: "lease-one", ObservedAt: time.Now()},
	}
	if err := message.Validate(token); err != nil {
		t.Fatal(err)
	}
	if err := message.Validate(token + "x"); err == nil {
		t.Fatal("invalid session token was accepted")
	}
	message.Status = &Status{Kind: StatusRunning}
	if err := message.Validate(token); err == nil {
		t.Fatal("multiple payloads were accepted")
	}
	policy := ReconnectPolicy{
		MaximumAttempts: 4, InitialDelay: 100 * time.Millisecond,
		MaximumDelay: 250 * time.Millisecond,
	}
	if err := policy.Validate(); err != nil {
		t.Fatal(err)
	}
	if got := policy.Delay(4); got != 250*time.Millisecond {
		t.Fatalf("bounded reconnect delay = %s", got)
	}
}
