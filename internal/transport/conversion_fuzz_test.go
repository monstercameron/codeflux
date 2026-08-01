package transport

import (
	"strings"
	"testing"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
)

// FuzzIdentityConversionNeverPanicsAndRoundTrips is M22-025's protobuf/domain
// conversion fuzz.
//
// Conversion sits directly on untrusted input: a client controls every byte
// of a StableIdentity. The properties are that conversion never panics, that
// an accepted identity round-trips through the wire form unchanged, and that
// an identity of one kind is never accepted as another — kind confusion here
// would let a client address a resource it does not own.
func FuzzIdentityConversionNeverPanicsAndRoundTrips(f *testing.F) {
	f.Add("tsk_019fbc20-39ff-764d-8d13-e55c7ddcc4b1", int32(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK))
	f.Add("tsk_019fbc20-39ff-764d-8d13-e55c7ddcc4b1", int32(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_RUN))
	f.Add("", int32(0))
	f.Add("thr_019fbc20-39ff-764d-8d13-e55c7ddcc4b1", int32(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD))
	f.Add(strings.Repeat("x", 8192), int32(1))
	f.Add("tsk_\x00", int32(1))
	f.Add("../../etc/passwd", int32(1))

	f.Fuzz(func(t *testing.T, raw string, kind int32) {
		identity := &codefluxv1.StableIdentity{
			Value: raw,
			Kind:  codefluxv1.StableIdentityKind(kind),
		}

		taskID, taskErr := TaskIDFromProto(identity)
		if taskErr == nil {
			if taskID.IsZero() {
				t.Fatalf("accepted %q/%d as a zero task identity", raw, kind)
			}
			wire, err := TaskIDToProto(taskID)
			if err != nil {
				t.Fatalf("accepted task identity failed to convert back: %v", err)
			}
			if wire.GetValue() != raw {
				t.Fatalf("task identity did not round-trip: %q became %q", raw, wire.GetValue())
			}
			if wire.GetKind() != codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK {
				t.Fatalf("task identity round-tripped with kind %v", wire.GetKind())
			}
		}

		threadID, threadErr := ThreadIDFromProto(identity)
		if threadErr == nil && threadID.IsZero() {
			t.Fatalf("accepted %q/%d as a zero thread identity", raw, kind)
		}

		// Kind confusion: the same envelope must never satisfy two kinds.
		if taskErr == nil && threadErr == nil {
			t.Fatalf("%q/%d was accepted as BOTH a task and a thread identity", raw, kind)
		}

		// A nil envelope must be refused, never dereferenced.
		if _, err := TaskIDFromProto(nil); err == nil {
			t.Fatal("a nil identity envelope must be refused")
		}
	})
}
