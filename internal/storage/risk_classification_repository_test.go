package storage

import (
	"context"
	"errors"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/internal/review"
)

func TestTaskRiskClassificationPersistsInputsExplanationAndMonotonicEscalation(t *testing.T) {
	repositories, task := createTaskFixture(t, 980)
	ctx := context.Background()
	initial, err := repositories.RecordInitialTaskRiskClassification(ctx, task.ID, []review.RiskSignal{review.RiskSignalNarrowScopedChange}, "")
	if err != nil {
		t.Fatal(err)
	}
	if initial.Revision != 1 || initial.Classification.SelectedRisk() != domain.RiskLevelRoutine {
		t.Fatalf("initial = %#v", initial)
	}
	if _, err := repositories.RecordInitialTaskRiskClassification(ctx, task.ID, []review.RiskSignal{review.RiskSignalDocumentationOnly}, ""); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate initial error = %v", err)
	}
	escalated, err := repositories.EscalateTaskRiskClassification(ctx, task.ID, []review.RiskSignal{review.RiskSignalExternalEffect}, domain.RiskLevelRoutine)
	if err != nil {
		t.Fatal(err)
	}
	if escalated.Revision != 2 || escalated.Classification.SelectedRisk() != domain.RiskLevelProtected {
		t.Fatalf("escalated = %#v", escalated)
	}
	retained, err := repositories.EscalateTaskRiskClassification(ctx, task.ID, []review.RiskSignal{review.RiskSignalDocumentationOnly}, "")
	if err != nil {
		t.Fatal(err)
	}
	if retained.Revision != 3 || retained.Classification.SelectedRisk() != domain.RiskLevelProtected {
		t.Fatalf("retained = %#v", retained)
	}
	loaded, err := repositories.GetLatestTaskRiskClassification(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Revision != retained.Revision || loaded.Classification.Explanation() != retained.Classification.Explanation() {
		t.Fatalf("loaded = %#v, want %#v", loaded, retained)
	}
}

func TestTaskRiskClassificationRowsAreImmutable(t *testing.T) {
	repositories, task := createTaskFixture(t, 990)
	ctx := context.Background()
	if _, err := repositories.RecordInitialTaskRiskClassification(ctx, task.ID, []review.RiskSignal{review.RiskSignalConfiguration}, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := repositories.database.sql.ExecContext(ctx, `
		UPDATE task_risk_classifications SET selected_risk = 'routine'
		WHERE task_id = ? AND revision = 1`, task.ID); err == nil {
		t.Fatal("immutable task risk classification accepted update")
	}
}
