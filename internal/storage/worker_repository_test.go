package storage

import (
	"context"
	"errors"
	"testing"
	"time"
)

const workerTokenHash = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

func TestWorkerLeasePreventsDuplicateOwnershipAndRecordsHeartbeat(t *testing.T) {
	ctx := context.Background()
	repositories, task := createTaskFixture(t, 1400)
	runID := createToolTestRun(t, repositories, task.ID, 1404)
	input := AcquireWorkerLease{
		ID: "lease-1400", TaskID: task.ID, RunID: runID,
		ProtocolVersion: 1, ToolSchemaVersion: 1, PolicyRevision: 3,
		WorktreePath: "/fixture/worktree", Endpoint: "http://127.0.0.1:43117",
		SessionTokenSHA256: workerTokenHash,
	}
	lease, err := repositories.AcquireWorkerLease(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	duplicate := input
	duplicate.ID = "lease-1401"
	if _, err := repositories.AcquireWorkerLease(ctx, duplicate); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate ownership error = %v", err)
	}
	started, err := repositories.RecordWorkerProcessStarted(
		ctx,
		RecordWorkerProcessStarted{
			ID: lease.ID, ExpectedRevision: lease.Revision, ProcessID: 1234,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if started.ProcessID == nil || *started.ProcessID != 1234 {
		t.Fatalf("started process metadata = %#v", started)
	}
	heartbeat, err := repositories.RecordWorkerHeartbeat(ctx, RecordWorkerHeartbeat{
		ID: lease.ID, ExpectedRevision: started.Revision, Sequence: 1,
		State: WorkerLeaseRunning, ProcessID: 1234,
		ObservedAt: lease.StartedAt.Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if heartbeat.ProcessID == nil || *heartbeat.ProcessID != 1234 ||
		heartbeat.LastSequence != 1 || heartbeat.State != WorkerLeaseRunning {
		t.Fatalf("heartbeat lease = %#v", heartbeat)
	}
	if _, err := repositories.RecordWorkerHeartbeat(ctx, RecordWorkerHeartbeat{
		ID: lease.ID, ExpectedRevision: heartbeat.Revision, Sequence: 1,
		State: WorkerLeaseRunning, ProcessID: 1234,
		ObservedAt: lease.StartedAt.Add(2 * time.Second),
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("replayed heartbeat error = %v", err)
	}
	if _, err := repositories.RecordWorkerHeartbeat(ctx, RecordWorkerHeartbeat{
		ID: lease.ID, ExpectedRevision: heartbeat.Revision, Sequence: 2,
		State: WorkerLeaseRunning, ProcessID: 4321,
		ObservedAt: lease.StartedAt.Add(2 * time.Second),
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed worker process error = %v", err)
	}
	reported, err := repositories.RecordWorkerReport(ctx, RecordWorkerReport{
		ID: lease.ID, ExpectedRevision: heartbeat.Revision, Sequence: 2,
		TaskID: task.ID, RunID: runID, Kind: "status",
		PayloadJSON: `{"Kind":"checkpointed","Summary":"redacted"}`,
		OccurredAt:  lease.StartedAt.Add(2 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	reports, err := repositories.ListWorkerReports(ctx, runID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 1 || reports[0].Sequence != 2 ||
		reports[0].Kind != "status" {
		t.Fatalf("worker reports = %#v", reports)
	}
	exit := 0
	finished, err := repositories.FinishWorkerLease(ctx, FinishWorkerLease{
		ID: lease.ID, ExpectedRevision: reported.Revision,
		State: WorkerLeaseExited, ExitCode: &exit,
	})
	if err != nil {
		t.Fatal(err)
	}
	if finished.State != WorkerLeaseExited || finished.EndedAt == nil {
		t.Fatalf("finished lease = %#v", finished)
	}
	if _, err := repositories.AcquireWorkerLease(ctx, duplicate); err != nil {
		t.Fatalf("new ownership after terminal lease: %v", err)
	}
}

func TestExpiredHeartbeatAtomicallyRequiresRecovery(t *testing.T) {
	ctx := context.Background()
	repositories, task := createTaskFixture(t, 1420)
	runID := createToolTestRun(t, repositories, task.ID, 1424)
	lease, err := repositories.AcquireWorkerLease(ctx, AcquireWorkerLease{
		ID: "lease-1420", TaskID: task.ID, RunID: runID,
		ProtocolVersion: 1, ToolSchemaVersion: 1,
		WorktreePath: "/fixture/worktree", Endpoint: "http://127.0.0.1:43117",
		SessionTokenSHA256: workerTokenHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := repositories.ExpireWorkerHeartbeats(
		ctx, lease.StartedAt.Add(time.Second),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].Lease.ID != lease.ID {
		t.Fatalf("recovery candidates = %#v", candidates)
	}
	var taskState, runState, leaseState string
	if err := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT task.state, run.state, lease.state
		 FROM tasks task
		 JOIN runs run ON run.task_id = task.id
		 JOIN worker_leases lease ON lease.run_id = run.id
		 WHERE task.id = ?`,
		task.ID,
	).Scan(&taskState, &runState, &leaseState); err != nil {
		t.Fatal(err)
	}
	if taskState != "recovery-required" || runState != "recovery-required" ||
		leaseState != "expired" {
		t.Fatalf("recovery states = %s/%s/%s", taskState, runState, leaseState)
	}
	repeated, err := repositories.ExpireWorkerHeartbeats(
		ctx, lease.StartedAt.Add(2*time.Second),
	)
	if err != nil || len(repeated) != 0 {
		t.Fatalf("repeated expiry = %#v, %v", repeated, err)
	}
}

func TestCoordinatorRestartAbandonsEveryActiveLeaseWithDistinctReason(t *testing.T) {
	ctx := context.Background()
	repositories, task := createTaskFixture(t, 1440)
	runID := createToolTestRun(t, repositories, task.ID, 1444)
	lease, err := repositories.AcquireWorkerLease(ctx, AcquireWorkerLease{
		ID: "lease-1440", TaskID: task.ID, RunID: runID,
		ProtocolVersion: 1, ToolSchemaVersion: 1,
		WorktreePath: "/fixture/worktree", Endpoint: "http://127.0.0.1:43117",
		SessionTokenSHA256: workerTokenHash,
	})
	if err != nil {
		t.Fatal(err)
	}
	heartbeat, err := repositories.RecordWorkerHeartbeat(ctx, RecordWorkerHeartbeat{
		ID: lease.ID, ExpectedRevision: lease.Revision, Sequence: 1,
		State: WorkerLeaseRunning, ProcessID: 1440,
		ObservedAt: lease.StartedAt.Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	candidates, err := repositories.AbandonActiveWorkerLeasesAfterRestart(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 ||
		candidates[0].Reason != WorkerRecoveryCoordinatorRestarted ||
		candidates[0].Reason == WorkerRecoveryHeartbeatExpired ||
		candidates[0].Lease.ProcessID == nil ||
		*candidates[0].Lease.ProcessID != 1440 {
		t.Fatalf("restart candidates = %#v", candidates)
	}
	var taskState, runState, leaseState, reason string
	if err := repositories.database.sql.QueryRowContext(
		ctx,
		`SELECT task.state, run.state, lease.state, task.invalidation_reason
		 FROM tasks task
		 JOIN runs run ON run.task_id = task.id
		 JOIN worker_leases lease ON lease.run_id = run.id
		 WHERE lease.id = ?`,
		heartbeat.ID,
	).Scan(&taskState, &runState, &leaseState, &reason); err != nil {
		t.Fatal(err)
	}
	if taskState != "recovery-required" || runState != "recovery-required" ||
		leaseState != "expired" || reason != WorkerRecoveryCoordinatorRestarted {
		t.Fatalf(
			"restart recovery = %s/%s/%s reason=%q",
			taskState, runState, leaseState, reason,
		)
	}
	repeated, err := repositories.AbandonActiveWorkerLeasesAfterRestart(ctx)
	if err != nil || len(repeated) != 0 {
		t.Fatalf("repeated restart abandonment = %#v, %v", repeated, err)
	}
}
