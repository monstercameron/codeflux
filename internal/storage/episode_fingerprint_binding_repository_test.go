package storage

import (
	"errors"
	"testing"
)

// TestRecordTaskExactFingerprintBindingIsImmutableAndIdempotent proves
// MEM-001's fingerprint carry: the first recording sticks, a byte-identical
// retry is idempotent, and a divergent retry -- the same task claiming a
// DIFFERENT exact fingerprint -- is a typed conflict rather than a silent
// overwrite. The final raw-SQL attack proves that even bypassing the
// repository cannot mutate an already-recorded binding.
func TestMEM001_RecordTaskExactFingerprintBindingIsImmutableAndIdempotent(t *testing.T) {
	ctx := t.Context()
	repositories, task := createTaskFixture(t, 5500)

	hash := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	first, err := repositories.RecordTaskExactFingerprintBinding(ctx, RecordTaskExactFingerprintBinding{
		TaskID: task.ID, FingerprintSchemaVersion: 1, FingerprintHash: hash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if first.FingerprintHash != hash || first.FingerprintSchemaVersion != 1 {
		t.Fatalf("first binding = %#v", first)
	}

	// Idempotent retry with the identical fingerprint.
	retried, err := repositories.RecordTaskExactFingerprintBinding(ctx, RecordTaskExactFingerprintBinding{
		TaskID: task.ID, FingerprintSchemaVersion: 1, FingerprintHash: hash,
	})
	if err != nil {
		t.Fatal(err)
	}
	if retried.FingerprintHash != first.FingerprintHash {
		t.Fatalf("retried binding = %#v, want %#v", retried, first)
	}

	// A divergent retry -- the SAME task claiming a different fingerprint --
	// is refused. If this succeeded silently, an episode opened later would
	// read back a fingerprint that was never the one intake actually
	// computed, defeating the exact-identity retrieval this binding exists
	// to serve.
	otherHash := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, err := repositories.RecordTaskExactFingerprintBinding(ctx, RecordTaskExactFingerprintBinding{
		TaskID: task.ID, FingerprintSchemaVersion: 1, FingerprintHash: otherHash,
	}); !errors.Is(err, ErrConflict) {
		t.Fatalf("divergent binding error = %v, want ErrConflict", err)
	}

	fetched, err := repositories.GetTaskExactFingerprintBinding(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if fetched.FingerprintHash != hash {
		t.Fatalf("fetched binding = %#v, want the original hash %q", fetched, hash)
	}

	// Raw-SQL attack: bypass the repository and attempt to mutate the
	// binding directly. The immutability trigger must refuse it.
	if _, err := repositories.database.sql.ExecContext(
		ctx, `UPDATE task_exact_fingerprint_bindings SET fingerprint_hash = ? WHERE task_id = ?`, otherHash, task.ID,
	); !errors.Is(classify("raw mutate fingerprint binding", err), ErrConstraint) {
		t.Fatalf("raw UPDATE of a fingerprint binding error = %v, want ErrConstraint", err)
	}
	if _, err := repositories.database.sql.ExecContext(
		ctx, `DELETE FROM task_exact_fingerprint_bindings WHERE task_id = ?`, task.ID,
	); !errors.Is(classify("raw delete fingerprint binding", err), ErrConstraint) {
		t.Fatalf("raw DELETE of a fingerprint binding error = %v, want ErrConstraint", err)
	}
}

// TestGetTaskExactFingerprintBindingRejectsUnknownTask proves the boundary
// case: a task that never had a binding recorded reports ErrNotFound rather
// than a zero value that would read as a real, empty fingerprint.
func TestMEM001_GetTaskExactFingerprintBindingRejectsUnknownTask(t *testing.T) {
	ctx := t.Context()
	repositories, task := createTaskFixture(t, 5510)
	if _, err := repositories.GetTaskExactFingerprintBinding(ctx, task.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get binding for a task with none error = %v, want ErrNotFound", err)
	}
}
