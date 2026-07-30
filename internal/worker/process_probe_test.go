package worker

import (
	"os"
	"testing"
)

func TestProcessAliveDetectsCurrentProcessAndRejectsInvalidIdentity(t *testing.T) {
	alive, err := ProcessAlive(os.Getpid())
	if err != nil || !alive {
		t.Fatalf("current process alive = %t, %v", alive, err)
	}
	if _, err := ProcessAlive(0); err == nil {
		t.Fatal("invalid process identity was accepted")
	}
}
