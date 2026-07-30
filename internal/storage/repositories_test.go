package storage

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

func TestProjectAndRepositoryOperationsUseTypedIdentities(t *testing.T) {
	ctx := context.Background()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 1)
	project, err := repositories.CreateProject(ctx, CreateProject{
		ID:   projectID,
		Name: "Fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	gotProject, err := repositories.GetProject(ctx, projectID)
	if err != nil {
		t.Fatal(err)
	}
	if gotProject != project {
		t.Fatalf("project round trip = %#v, want %#v", gotProject, project)
	}

	repositoryID := testRepositoryID(t, 2)
	repository, err := repositories.CreateRepository(ctx, CreateRepository{
		ID:            repositoryID,
		ProjectID:     projectID,
		CanonicalPath: "/fixture",
		GitIdentity:   "git-fixture",
	})
	if err != nil {
		t.Fatal(err)
	}
	gotRepository, err := repositories.GetRepository(ctx, repositoryID)
	if err != nil {
		t.Fatal(err)
	}
	if gotRepository != repository {
		t.Fatalf("repository round trip = %#v, want %#v", gotRepository, repository)
	}

	if _, err := repositories.CreateProject(ctx, CreateProject{
		ID:   projectID,
		Name: "Duplicate",
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate project error = %v, want conflict", err)
	}
	if _, err := repositories.GetProject(ctx, testProjectID(t, 99)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing project error = %v, want not found", err)
	}
	if _, err := repositories.CreateRepository(ctx, CreateRepository{
		ID:            testRepositoryID(t, 100),
		ProjectID:     testProjectID(t, 101),
		CanonicalPath: "/orphan",
		GitIdentity:   "git-orphan",
	}); !errors.Is(err, ErrConstraint) {
		t.Fatalf("orphan repository error = %v, want constraint", err)
	}
}

func TestThreadCursorPaginationIsStable(t *testing.T) {
	ctx := context.Background()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 10)
	repositoryID := testRepositoryID(t, 11)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)

	ids := []domain.ThreadID{
		testThreadID(t, 12),
		testThreadID(t, 13),
		testThreadID(t, 14),
	}
	for index, id := range ids {
		if _, err := repositories.CreateThread(ctx, CreateThread{
			ID:           id,
			ProjectID:    projectID,
			RepositoryID: repositoryID,
			Title:        fmt.Sprintf("Thread %d", index),
		}); err != nil {
			t.Fatal(err)
		}
	}
	first, err := repositories.ListThreads(ctx, ListThreads{
		RepositoryID: repositoryID,
		Limit:        2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Threads) != 2 || first.Next == nil {
		t.Fatalf("first page = %#v", first)
	}
	if first.Threads[0].ID != ids[2] || first.Threads[1].ID != ids[1] {
		t.Fatalf("first page order = %v, %v", first.Threads[0].ID, first.Threads[1].ID)
	}
	second, err := repositories.ListThreads(ctx, ListThreads{
		RepositoryID: repositoryID,
		Before:       first.Next,
		Limit:        2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Threads) != 1 ||
		second.Threads[0].ID != ids[0] ||
		second.Next != nil {
		t.Fatalf("second page = %#v", second)
	}
}

func TestThreadCreationRejectsCrossProjectRepository(t *testing.T) {
	ctx := context.Background()
	repositories := openTestRepositories(t)
	firstProject := testProjectID(t, 20)
	secondProject := testProjectID(t, 21)
	repositoryID := testRepositoryID(t, 22)
	mustCreateProjectRepository(t, repositories, firstProject, repositoryID)
	if _, err := repositories.CreateProject(ctx, CreateProject{
		ID:   secondProject,
		Name: "Second",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateThread(ctx, CreateThread{
		ID:           testThreadID(t, 23),
		ProjectID:    secondProject,
		RepositoryID: repositoryID,
		Title:        "Mismatched",
	}); !errors.Is(err, ErrConstraint) {
		t.Fatalf("cross-project thread error = %v, want constraint", err)
	}
}

func TestAppendMessageIsIdempotentAndAllocatesSequence(t *testing.T) {
	ctx := context.Background()
	repositories := openTestRepositories(t)
	projectID := testProjectID(t, 30)
	repositoryID := testRepositoryID(t, 31)
	mustCreateProjectRepository(t, repositories, projectID, repositoryID)
	threadID := testThreadID(t, 32)
	if _, err := repositories.CreateThread(ctx, CreateThread{
		ID:           threadID,
		ProjectID:    projectID,
		RepositoryID: repositoryID,
		Title:        "Messages",
	}); err != nil {
		t.Fatal(err)
	}
	input := AppendMessage{
		ID:             testMessageID(t, 33),
		ThreadID:       threadID,
		Role:           MessageRoleUser,
		BodyRedacted:   "first",
		IdempotencyKey: "message-one",
	}
	first, err := repositories.AppendMessage(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	retried, err := repositories.AppendMessage(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if retried != first || first.Sequence != 1 {
		t.Fatalf("idempotent messages = %#v, %#v", first, retried)
	}
	second, err := repositories.AppendMessage(ctx, AppendMessage{
		ID:             testMessageID(t, 34),
		ThreadID:       threadID,
		Role:           MessageRoleAssistant,
		BodyRedacted:   "second",
		IdempotencyKey: "message-two",
	})
	if err != nil {
		t.Fatal(err)
	}
	if second.Sequence != 2 {
		t.Fatalf("second message sequence = %d, want 2", second.Sequence)
	}
	input.BodyRedacted = "changed"
	if _, err := repositories.AppendMessage(ctx, input); !errors.Is(err, ErrConflict) {
		t.Fatalf("changed retry error = %v, want conflict", err)
	}
}

func openTestRepositories(t *testing.T) *Repositories {
	t.Helper()
	database := openMigratedSchema(t)
	current := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	repositories, err := NewRepositories(database, func() time.Time {
		current = current.Add(time.Microsecond)
		return current
	})
	if err != nil {
		t.Fatal(err)
	}
	return repositories
}

func mustCreateProjectRepository(
	t *testing.T,
	repositories *Repositories,
	projectID domain.ProjectID,
	repositoryID domain.RepositoryID,
) {
	t.Helper()
	if _, err := repositories.CreateProject(context.Background(), CreateProject{
		ID:   projectID,
		Name: "Fixture",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateRepository(context.Background(), CreateRepository{
		ID:            repositoryID,
		ProjectID:     projectID,
		CanonicalPath: "/fixture/" + repositoryID.String(),
		GitIdentity:   "git-" + repositoryID.String(),
	}); err != nil {
		t.Fatal(err)
	}
}

func testProjectID(t *testing.T, number int) domain.ProjectID {
	t.Helper()
	id, err := domain.ParseProjectID("prj_" + testUUID(number))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func testRepositoryID(t *testing.T, number int) domain.RepositoryID {
	t.Helper()
	id, err := domain.ParseRepositoryID("repo_" + testUUID(number))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func testThreadID(t *testing.T, number int) domain.ThreadID {
	t.Helper()
	id, err := domain.ParseThreadID("thr_" + testUUID(number))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func testMessageID(t *testing.T, number int) domain.MessageID {
	t.Helper()
	id, err := domain.ParseMessageID("msg_" + testUUID(number))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func testUUID(number int) string {
	return fmt.Sprintf("01890f3c-4a00-7abc-8def-%012x", number)
}
