//go:build integration

package storage

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/events"
)

func TestThreadServiceRepositoryLifecycleIsScopedAndIdempotent(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 600)
	repositoryID := testRepositoryID(t, 601)
	workspaceID := testWorkspaceID(t, 602)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	if _, err := repositories.database.sql.ExecContext(ctx, `INSERT INTO workspaces (
		id, repository_id, canonical_path, state,
		created_at_unix_micros, updated_at_unix_micros, revision
	) VALUES (?, ?, ?, 'active', 1, 1, 0)`, workspaceID, repositoryID, "/fixture/workspace"); err != nil {
		t.Fatal(err)
	}

	artifactID, err := domain.ParseArtifactID("art_" + testUUID(603))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.database.sql.ExecContext(ctx, `INSERT INTO artifacts (
		id, project_id, repository_id, artifact_type, immutable_version,
		content_hash, media_type, storage_class, sanitized_content, created_at_unix_micros
	) VALUES (?, ?, ?, 'composer-attachment', 1, ?, 'text/plain',
		'permanent-semantic', ?, 1)`, artifactID, projectID, repositoryID, strings.Repeat("a", 64), []byte("fixture")); err != nil {
		t.Fatal(err)
	}

	threadID := testThreadID(t, 604)
	sessionID := testSessionID(t, 608)
	created, err := repositories.CreateThread(ctx, CreateThread{
		ID:             threadID,
		SessionID:      sessionID,
		ProjectID:      projectID,
		RepositoryID:   repositoryID,
		WorkspaceID:    workspaceID,
		Title:          "Durable thread",
		IdempotencyKey: "create-one",
	})
	if err != nil {
		t.Fatal(err)
	}
	retriedCreate, err := repositories.CreateThread(ctx, CreateThread{
		ID:             testThreadID(t, 605),
		SessionID:      testSessionID(t, 609),
		ProjectID:      projectID,
		RepositoryID:   repositoryID,
		WorkspaceID:    workspaceID,
		Title:          "Durable thread",
		IdempotencyKey: "create-one",
	})
	if err != nil {
		t.Fatal(err)
	}
	if retriedCreate != created {
		t.Fatalf("create retry = %#v, want %#v", retriedCreate, created)
	}

	messageCommit, err := repositories.AppendMessageAndDraftTask(ctx, AppendMessageAndDraftTask{Message: AppendMessage{
		ID:             testMessageID(t, 606),
		ThreadID:       threadID,
		Role:           MessageRoleUser,
		BodyRedacted:   "Please inspect the artifact.",
		AttachmentIDs:  []domain.ArtifactID{artifactID},
		IdempotencyKey: "message-one",
	}, ExpectedRevision: created.Revision})
	if err != nil {
		t.Fatal(err)
	}
	message := messageCommit.Message
	retriedCommit, err := repositories.AppendMessageAndDraftTask(ctx, AppendMessageAndDraftTask{Message: AppendMessage{
		ID:             testMessageID(t, 607),
		ThreadID:       threadID,
		Role:           MessageRoleUser,
		BodyRedacted:   "Please inspect the artifact.",
		AttachmentIDs:  []domain.ArtifactID{artifactID},
		IdempotencyKey: "message-one",
	}, ExpectedRevision: created.Revision})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(retriedCommit.Message, message) || len(retriedCommit.Events) != 0 {
		t.Fatalf("message retry = %#v, want %#v", retriedCommit, message)
	}
	page, err := repositories.ListMessages(ctx, ListMessages{ThreadID: threadID, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 1 || !reflect.DeepEqual(page.Messages[0].AttachmentIDs, []domain.ArtifactID{artifactID}) {
		t.Fatalf("message page = %#v", page)
	}

	afterMessage, err := repositories.GetThread(ctx, threadID)
	if err != nil {
		t.Fatal(err)
	}
	if afterMessage.Revision != created.Revision+1 {
		t.Fatalf("thread revision after message = %d, want %d", afterMessage.Revision, created.Revision+1)
	}
	renamed, err := repositories.RenameThread(ctx, RenameThread{
		ThreadID:         threadID,
		ExpectedRevision: afterMessage.Revision,
		Title:            "Renamed thread",
		IdempotencyKey:   "rename-one",
	})
	if err != nil {
		t.Fatal(err)
	}
	retriedRename, err := repositories.RenameThread(ctx, RenameThread{
		ThreadID:         threadID,
		ExpectedRevision: afterMessage.Revision,
		Title:            "Renamed thread",
		IdempotencyKey:   "rename-one",
	})
	if err != nil {
		t.Fatal(err)
	}
	if retriedRename != renamed {
		t.Fatalf("rename retry = %#v, want %#v", retriedRename, renamed)
	}
	if _, err := repositories.RenameThread(ctx, RenameThread{
		ThreadID:         threadID,
		ExpectedRevision: afterMessage.Revision,
		Title:            "Stale rename",
		IdempotencyKey:   "rename-stale",
	}); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale rename error = %v", err)
	}

	archived, err := repositories.ArchiveThread(ctx, ArchiveThread{
		ThreadID:         threadID,
		ExpectedRevision: renamed.Revision,
		Archived:         true,
		IdempotencyKey:   "archive-one",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !archived.Archived || archived.Revision != renamed.Revision+1 {
		t.Fatalf("archived thread = %#v", archived)
	}
	active, err := repositories.ListThreads(ctx, ListThreads{
		RepositoryID: repositoryID,
		WorkspaceID:  workspaceID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(active.Threads) != 0 {
		t.Fatalf("active thread page = %#v", active)
	}
	withArchived, err := repositories.ListThreads(ctx, ListThreads{
		RepositoryID:    repositoryID,
		WorkspaceID:     workspaceID,
		IncludeArchived: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(withArchived.Threads) != 1 || withArchived.Threads[0] != archived {
		t.Fatalf("archived thread page = %#v, want %#v", withArchived, archived)
	}
	replayed, err := repositories.ReplaySessionEvents(ctx, ReplaySessionEvents{SessionID: sessionID, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	wantKinds := []events.Kind{events.KindThreadCreated, events.KindMessageFinal, events.KindThreadRenamed, events.KindThreadArchived}
	if len(replayed) != len(wantKinds) {
		t.Fatalf("thread lifecycle replay = %#v", replayed)
	}
	for index, kind := range wantKinds {
		if replayed[index].Sequence != uint64(index+1) || replayed[index].Kind != kind || !replayed[index].CorrectnessBearing() {
			t.Fatalf("thread lifecycle event %d = %#v", index, replayed[index])
		}
	}
}

func TestThreadServiceRepositoryRejectsCrossWorkspaceAuthority(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 620)
	firstRepositoryID := testRepositoryID(t, 621)
	secondRepositoryID := testRepositoryID(t, 622)
	workspaceID := testWorkspaceID(t, 623)
	mustCreateProjectRepository(t, repositories, projectID, firstRepositoryID)
	if _, err := repositories.CreateRepository(ctx, CreateRepository{
		ID: secondRepositoryID, ProjectID: projectID,
		CanonicalPath: "/fixture/second", GitIdentity: "git-second",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.database.sql.ExecContext(ctx, `INSERT INTO workspaces (
		id, repository_id, canonical_path, state,
		created_at_unix_micros, updated_at_unix_micros, revision
	) VALUES (?, ?, '/fixture/workspace', 'active', 1, 1, 0)`, workspaceID, firstRepositoryID); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateThread(ctx, CreateThread{
		ID: testThreadID(t, 624), SessionID: testSessionID(t, 625), ProjectID: projectID, RepositoryID: secondRepositoryID,
		WorkspaceID: workspaceID, Title: "Denied", IdempotencyKey: "denied",
	}); !errors.Is(err, ErrConstraint) {
		t.Fatalf("cross-workspace create error = %v, want constraint", err)
	}
	if _, err := repositories.ListThreads(ctx, ListThreads{
		RepositoryID: secondRepositoryID, WorkspaceID: workspaceID,
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-workspace list error = %v, want not found", err)
	}
}

func TestAppendMessageAndDraftTaskIsOneIdempotentTransaction(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 640)
	repositoryID := testRepositoryID(t, 641)
	workspaceID := testWorkspaceID(t, 642)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	if _, err := repositories.database.sql.ExecContext(ctx, `INSERT INTO workspaces (
		id, repository_id, canonical_path, state,
		created_at_unix_micros, updated_at_unix_micros, revision
	) VALUES (?, ?, '/fixture/atomic', 'active', 1, 1, 0)`, workspaceID, repositoryID); err != nil {
		t.Fatal(err)
	}
	thread, err := repositories.CreateThread(ctx, CreateThread{
		ID: testThreadID(t, 643), SessionID: testSessionID(t, 648), ProjectID: projectID, RepositoryID: repositoryID,
		WorkspaceID: workspaceID, Title: "Atomic send", IdempotencyKey: "atomic-thread",
	})
	if err != nil {
		t.Fatal(err)
	}
	command := AppendMessageAndDraftTask{
		Message: AppendMessage{ID: testMessageID(t, 644), ThreadID: thread.ID, Role: MessageRoleUser,
			BodyRedacted: "Create the task", IdempotencyKey: "atomic-send"},
		DraftTask: &CreateTask{ID: testTaskID(t, 645), ThreadID: thread.ID, RepositoryID: repositoryID,
			PolicyPreset: domain.PolicyPresetCorrectness, ReasoningEffort: domain.ReasoningEffortMaximum,
			RiskLevel: domain.RiskLevelRoutine, RequiredAssurance: domain.AssuranceLevelContractChecked,
			SettingsRevision: 999, IdempotencyKey: "atomic-send"},
	}
	if _, err := repositories.AppendMessageAndDraftTask(ctx, command); err == nil {
		t.Fatal("send with missing settings revision succeeded")
	}
	page, err := repositories.ListMessages(ctx, ListMessages{ThreadID: thread.ID})
	if err != nil {
		t.Fatal(err)
	}
	afterRollback, err := repositories.GetThread(ctx, thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 0 || afterRollback.Revision != thread.Revision {
		t.Fatalf("rolled-back send left page=%#v thread=%#v", page, afterRollback)
	}

	command.DraftTask.SettingsRevision = 0
	command.ExpectedRevision = 99
	if _, err := repositories.AppendMessageAndDraftTask(ctx, command); !errors.Is(err, ErrStaleRevision) {
		t.Fatalf("stale atomic send error = %v", err)
	}
	command.ExpectedRevision = thread.Revision
	first, err := repositories.AppendMessageAndDraftTask(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	command.Message.ID = testMessageID(t, 646)
	command.DraftTask.ID = testTaskID(t, 647)
	retried, err := repositories.AppendMessageAndDraftTask(ctx, command)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(retried.Message, first.Message) || !reflect.DeepEqual(retried.DraftTask, first.DraftTask) ||
		len(retried.Events) != 0 || first.DraftTask == nil || first.DraftTask.RequestMessageID == nil ||
		*first.DraftTask.RequestMessageID != first.Message.ID {
		t.Fatalf("atomic retry = %#v, want %#v", retried, first)
	}
	changedChoice := command
	changedChoice.DraftTask = nil
	if _, err := repositories.AppendMessageAndDraftTask(ctx, changedChoice); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed draft-task choice error = %v", err)
	}
}
