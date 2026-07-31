package transport

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/review"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type editorOpenApplicationStub struct {
	command review.OpenEditorCommand
	target  review.EditorTarget
	err     error
}

type reviewEvidenceApplicationStub struct {
	query  review.EvidenceQuery
	bundle review.EvidenceBundle
	err    error
}

func (stub *reviewEvidenceApplicationStub) GetEvidenceReport(
	_ context.Context,
	query review.EvidenceQuery,
) (review.EvidenceBundle, error) {
	stub.query = query
	return stub.bundle, stub.err
}

func (stub *editorOpenApplicationStub) OpenInEditor(
	_ context.Context,
	command review.OpenEditorCommand,
) (review.EditorTarget, error) {
	stub.command = command
	return stub.target, stub.err
}

func TestReviewServiceOpensValidatedEditorTarget(t *testing.T) {
	root := t.TempDir()
	name := filepath.Join(root, "internal", "review", "editor.go")
	if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte("package review\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	target, err := review.ResolveEditorTarget(root, "internal/review/editor.go", 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	workspaceID, _ := domain.NewWorkspaceID()
	workspace, _ := WorkspaceIDToProto(workspaceID)
	application := &editorOpenApplicationStub{target: target}
	service, err := NewReviewService(application, &reviewEvidenceApplicationStub{})
	if err != nil {
		t.Fatal(err)
	}
	response, err := service.OpenInEditor(context.Background(), &codefluxv1.OpenInEditorRequest{
		Control:     &codefluxv1.MutationControl{IdempotencyKey: "editor-open-1"},
		WorkspaceId: workspace, WorkspaceRelativePath: "internal/review/editor.go",
		Line: 1, Column: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if response.GetDecision() != "opened" || response.GetPath().GetWorkspaceRelativeSlashPath() != "internal/review/editor.go" {
		t.Fatalf("response = %+v", response)
	}
	if application.command.WorkspaceID != workspaceID || application.command.IdempotencyKey != "editor-open-1" ||
		application.command.Line != 1 || application.command.Column != 1 {
		t.Fatalf("application command = %+v", application.command)
	}
}

func TestReviewServiceMapsEditorPathEscapeToPermissionDenied(t *testing.T) {
	workspaceID, _ := domain.NewWorkspaceID()
	workspace, _ := WorkspaceIDToProto(workspaceID)
	service, err := NewReviewService(&editorOpenApplicationStub{err: review.ErrEditorSourceOutsideRepository}, &reviewEvidenceApplicationStub{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.OpenInEditor(context.Background(), &codefluxv1.OpenInEditorRequest{
		Control:     &codefluxv1.MutationControl{IdempotencyKey: "editor-open-escape"},
		WorkspaceId: workspace, WorkspaceRelativePath: "../outside.go", Line: 1, Column: 1,
	})
	if status.Code(err) != codes.PermissionDenied {
		t.Fatalf("status = %s, error = %v", status.Code(err), err)
	}
}

func TestReviewServiceRejectsMissingIdempotencyBeforeApplication(t *testing.T) {
	application := &editorOpenApplicationStub{err: errors.New("must not be reached")}
	service, err := NewReviewService(application, &reviewEvidenceApplicationStub{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.OpenInEditor(context.Background(), &codefluxv1.OpenInEditorRequest{})
	var validation *RequestValidationError
	if !errors.As(err, &validation) || validation.Field != "control.idempotency_key" ||
		!application.command.WorkspaceID.IsZero() {
		t.Fatalf("validation = %#v, command = %+v", err, application.command)
	}
}
