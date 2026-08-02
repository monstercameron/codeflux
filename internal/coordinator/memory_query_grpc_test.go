package coordinator

import (
	"context"
	"database/sql"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/transport"
	"codeflux.dev/codeflux/migrations"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	_ "modernc.org/sqlite"
)

// TestMemoryServiceReadsOneProjectsMemoryOverGRPC exercises the read the
// memory page depends on against real SQLite and the real boundary: the list
// the page opens with, the detail one row opens, and the refusal that keeps
// one project's learned facts out of another project's console.
func TestMemoryServiceReadsOneProjectsMemoryOverGRPC(t *testing.T) {
	fixture := createMemoryGRPCFixture(t)
	boundary, err := transport.NewBoundary(transport.BoundaryOptions{SessionToken: fixture.token})
	if err != nil {
		t.Fatal(err)
	}
	application, err := NewMemoryQueryService(fixture.repositories)
	if err != nil {
		t.Fatal(err)
	}
	service, err := transport.NewMemoryService(application)
	if err != nil {
		t.Fatal(err)
	}
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer(grpc.UnaryInterceptor(boundary.UnaryInterceptor()))
	codefluxv1.RegisterMemoryServiceServer(server, service)
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close(); <-serveDone })
	connection, err := grpc.NewClient(
		"passthrough:///memory",
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	client := codefluxv1.NewMemoryServiceClient(connection)
	ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs(transport.SessionMetadataKey, fixture.token))

	listed, err := client.ListMemoryArtifacts(ctx, &codefluxv1.ListMemoryArtifactsRequest{
		ProjectId: projectIdentity(fixture.projectID),
		Page:      &codefluxv1.PageRequest{Limit: 50},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(listed.GetArtifacts()) != 1 {
		t.Fatalf("artifacts = %d, want this project's one artifact", len(listed.GetArtifacts()))
	}
	summary := listed.GetArtifacts()[0]
	if summary.GetArtifactId().GetValue() != fixture.artifactID.String() ||
		summary.GetKind() != string(domain.MemoryArtifactKindRepositoryFact) ||
		summary.GetSummary().GetValue() != "The suite runs with go test ./..." {
		t.Fatalf("summary = %#v", summary)
	}

	detail, err := client.GetMemoryArtifact(ctx, &codefluxv1.GetMemoryArtifactRequest{
		ProjectId:  projectIdentity(fixture.projectID),
		ArtifactId: summary.GetArtifactId(),
	})
	if err != nil {
		t.Fatalf("detail read: %v", err)
	}
	labels := make([]string, 0, len(detail.GetFields()))
	for _, field := range detail.GetFields() {
		labels = append(labels, field.GetLabel()+"="+field.GetValue().GetValue())
	}
	joined := strings.Join(labels, "|")
	if !strings.Contains(joined, "Statement=The suite runs with go test ./...") {
		t.Fatalf("detail fields = %s", joined)
	}
	if detail.GetLineage() == nil || detail.GetContentSha256() == "" {
		t.Fatalf("detail = %#v", detail)
	}

	if _, err := client.GetMemoryArtifact(ctx, &codefluxv1.GetMemoryArtifactRequest{
		ProjectId:  projectIdentity(fixture.otherProjectID),
		ArtifactId: summary.GetArtifactId(),
	}); status.Code(err) != codes.NotFound {
		t.Fatalf("cross-project detail code = %v (%v)", status.Code(err), err)
	}

	if _, err := client.ListMemoryArtifacts(t.Context(), &codefluxv1.ListMemoryArtifactsRequest{
		ProjectId: projectIdentity(fixture.projectID),
	}); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("unauthenticated list code = %v (%v)", status.Code(err), err)
	}
}

type memoryGRPCFixture struct {
	repositories   *storage.Repositories
	token          string
	projectID      domain.ProjectID
	otherProjectID domain.ProjectID
	artifactID     domain.MemoryArtifactID
}

func createMemoryGRPCFixture(t *testing.T) memoryGRPCFixture {
	t.Helper()
	path := filepath.Join(t.TempDir(), "memory-grpc.sqlite3")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	sources, err := migrations.Sources()
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range sources {
		if _, err := raw.ExecContext(t.Context(), source.SQL); err != nil {
			t.Fatalf("apply %s: %v", source.Descriptor.Name, err)
		}
	}
	if _, err := raw.ExecContext(t.Context(), "PRAGMA foreign_keys = ON"); err != nil {
		t.Fatal(err)
	}
	projectID, _ := domain.NewProjectID()
	repositoryID, _ := domain.NewRepositoryID()
	otherProjectID, _ := domain.NewProjectID()
	statements := []struct {
		query string
		args  []any
	}{
		{`INSERT INTO projects (id, name, created_at_unix_micros, updated_at_unix_micros, revision) VALUES (?, 'Memory project', 1, 1, 1)`, []any{projectID}},
		{`INSERT INTO projects (id, name, created_at_unix_micros, updated_at_unix_micros, revision) VALUES (?, 'Other project', 1, 1, 1)`, []any{otherProjectID}},
		{`INSERT INTO repositories (id, project_id, canonical_path, git_identity, created_at_unix_micros, updated_at_unix_micros, revision) VALUES (?, ?, 'C:/memory', 'memory-git', 1, 1, 1)`, []any{repositoryID, projectID}},
	}
	for _, statement := range statements {
		if _, err := raw.ExecContext(t.Context(), statement.query, statement.args...); err != nil {
			t.Fatalf("seed memory fixture: %v\n%s", err, statement.query)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	database, err := storage.Open(t.Context(), storage.OpenOptions{Path: path, MaximumConnections: 1})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close(context.Background()) })
	repositories, err := storage.NewRepositories(database, func() time.Time { return time.Unix(1, 0).UTC() })
	if err != nil {
		t.Fatal(err)
	}
	artifactID, _ := domain.NewMemoryArtifactID()
	revisionID, _ := domain.NewMemoryArtifactRevisionID()
	content, err := domain.NewRepositoryFactMemoryContent(domain.RepositoryFactContent{
		Repository: repositoryID, Category: domain.RepositoryFactCategory("build-command"),
		Statement: "The suite runs with go test ./...",
		Binding:   domain.RevisionBinding{Known: true, ExactRevision: "abc1234"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.CreateMemoryArtifact(t.Context(), storage.CreateMemoryArtifact{
		ArtifactID: artifactID, RevisionID: revisionID, ProjectID: projectID,
		Content: content, IdempotencyKey: "memory-grpc-fixture",
	}); err != nil {
		t.Fatal(err)
	}
	return memoryGRPCFixture{
		repositories: repositories, token: strings.Repeat("m", 48),
		projectID: projectID, otherProjectID: otherProjectID, artifactID: artifactID,
	}
}
