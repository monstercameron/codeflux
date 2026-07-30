package domain

import (
	"bytes"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

const uuidV7Fixture = "01890f3c-4a00-7abc-8def-0123456789ab"

type parsedIdentity interface {
	String() string
	IsZero() bool
}

func TestEveryIdentityTypeParsesItsCanonicalPrefix(t *testing.T) {
	cases := []struct {
		name   string
		prefix string
		parse  func(string) (parsedIdentity, error)
	}{
		{name: "project", prefix: "prj", parse: wrapIDParser(ParseProjectID)},
		{name: "repository", prefix: "repo", parse: wrapIDParser(ParseRepositoryID)},
		{name: "workspace", prefix: "wsp", parse: wrapIDParser(ParseWorkspaceID)},
		{name: "thread", prefix: "thr", parse: wrapIDParser(ParseThreadID)},
		{name: "message", prefix: "msg", parse: wrapIDParser(ParseMessageID)},
		{name: "task", prefix: "tsk", parse: wrapIDParser(ParseTaskID)},
		{name: "run", prefix: "run", parse: wrapIDParser(ParseRunID)},
		{name: "event", prefix: "evt", parse: wrapIDParser(ParseEventID)},
		{name: "checkpoint", prefix: "ckp", parse: wrapIDParser(ParseCheckpointID)},
		{name: "approval", prefix: "apr", parse: wrapIDParser(ParseApprovalID)},
		{name: "graph", prefix: "grf", parse: wrapIDParser(ParseGraphID)},
		{name: "graph revision", prefix: "grv", parse: wrapIDParser(ParseGraphRevisionID)},
		{name: "node", prefix: "nod", parse: wrapIDParser(ParseNodeID)},
		{name: "edge", prefix: "edg", parse: wrapIDParser(ParseEdgeID)},
		{name: "validation", prefix: "val", parse: wrapIDParser(ParseValidationID)},
		{name: "evidence", prefix: "evd", parse: wrapIDParser(ParseEvidenceID)},
		{name: "artifact", prefix: "art", parse: wrapIDParser(ParseArtifactID)},
		{name: "atom", prefix: "atm", parse: wrapIDParser(ParseAtomID)},
		{name: "model request", prefix: "mrq", parse: wrapIDParser(ParseModelRequestID)},
		{name: "provider", prefix: "prv", parse: wrapIDParser(ParseProviderID)},
		{name: "budget", prefix: "bdg", parse: wrapIDParser(ParseBudgetID)},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			raw := test.prefix + "_" + uuidV7Fixture
			identity, err := test.parse(raw)
			if err != nil {
				t.Fatalf("parse canonical identity: %v", err)
			}
			if identity.IsZero() || identity.String() != raw {
				t.Fatalf("identity = %q, zero=%v; want %q", identity.String(), identity.IsZero(), raw)
			}
			if _, err := test.parse(""); !errors.Is(err, ErrInvalidID) {
				t.Fatalf("empty error = %v, want ErrInvalidID", err)
			}
			if _, err := test.parse("wrong_" + uuidV7Fixture); !errors.Is(err, ErrInvalidID) {
				t.Fatalf("wrong-prefix error = %v, want ErrInvalidID", err)
			}
		})
	}
}

func TestIdentityParsingRejectsMalformedUUIDv7(t *testing.T) {
	for _, raw := range []string{
		"prj_01890f3c-4a00-6abc-8def-0123456789ab",
		"prj_01890f3c-4a00-7abc-7def-0123456789ab",
		"prj_01890F3C-4A00-7ABC-8DEF-0123456789AB",
		"prj_01890f3c4a007abc8def0123456789ab",
	} {
		if _, err := ParseProjectID(raw); !errors.Is(err, ErrInvalidID) {
			t.Errorf("ParseProjectID(%q) error = %v, want ErrInvalidID", raw, err)
		}
	}
}

func TestIdentityBoundariesRejectEmptyAndMalformedValues(t *testing.T) {
	valid, err := ParseProjectID("prj_" + uuidV7Fixture)
	if err != nil {
		t.Fatal(err)
	}
	identity := valid
	if err := json.Unmarshal([]byte("null"), &identity); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("JSON null error = %v, want ErrInvalidID", err)
	}
	if !identity.IsZero() {
		t.Fatalf("identity retained stale value after invalid JSON: %q", identity)
	}
	if err := identity.Scan(nil); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("SQL null error = %v, want ErrInvalidID", err)
	}
	if _, err := (ProjectID{}).Value(); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("zero SQL value error = %v, want ErrInvalidID", err)
	}
	if _, err := json.Marshal(ProjectID{}); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("zero JSON error = %v, want ErrInvalidID", err)
	}
}

func TestNewProjectIDGeneratesCanonicalIdentity(t *testing.T) {
	identity, err := NewProjectID()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ParseProjectID(identity.String()); err != nil {
		t.Fatalf("generated identity is not canonical: %v", err)
	}
}

func TestUUIDv7IsLexicographicallyTimeSortable(t *testing.T) {
	entropy := bytes.NewReader(make([]byte, 20))
	first, err := newUUIDv7(time.UnixMilli(1_700_000_000_000), entropy)
	if err != nil {
		t.Fatal(err)
	}
	second, err := newUUIDv7(time.UnixMilli(1_700_000_000_001), entropy)
	if err != nil {
		t.Fatal(err)
	}
	if first >= second {
		t.Fatalf("UUIDv7 order = %q >= %q", first, second)
	}
	if first[14] != '7' || !strings.Contains("89ab", first[19:20]) {
		t.Fatalf("UUIDv7 version or variant is invalid: %q", first)
	}
}

func TestIdentityKindsAreNotAssignableOrConvertible(t *testing.T) {
	project := reflect.TypeOf(ProjectID{})
	task := reflect.TypeOf(TaskID{})
	if project.AssignableTo(task) || task.AssignableTo(project) {
		t.Fatal("distinct identity kinds are assignable")
	}
	if project.ConvertibleTo(task) || task.ConvertibleTo(project) {
		t.Fatal("distinct identity kinds are explicitly convertible")
	}
}

func wrapIDParser[T parsedIdentity](parse func(string) (T, error)) func(string) (parsedIdentity, error) {
	return func(raw string) (parsedIdentity, error) {
		return parse(raw)
	}
}
