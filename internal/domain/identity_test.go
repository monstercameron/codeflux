package domain

import (
	"bytes"
	"database/sql"
	"database/sql/driver"
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
		{name: "session", prefix: "ses", parse: wrapIDParser(ParseSessionID)},
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
		{name: "episode", prefix: "epi", parse: wrapIDParser(ParseEpisodeID)},
		{name: "memory artifact", prefix: "mem", parse: wrapIDParser(ParseMemoryArtifactID)},
		{name: "memory artifact revision", prefix: "mrv", parse: wrapIDParser(ParseMemoryArtifactRevisionID)},
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

func TestEveryIdentityTypeJSONAndSQLRoundTrips(t *testing.T) {
	t.Run("project", func(t *testing.T) {
		assertIdentityCodecs(t, "prj", ParseProjectID)
	})
	t.Run("repository", func(t *testing.T) {
		assertIdentityCodecs(t, "repo", ParseRepositoryID)
	})
	t.Run("workspace", func(t *testing.T) {
		assertIdentityCodecs(t, "wsp", ParseWorkspaceID)
	})
	t.Run("thread", func(t *testing.T) {
		assertIdentityCodecs(t, "thr", ParseThreadID)
	})
	t.Run("message", func(t *testing.T) {
		assertIdentityCodecs(t, "msg", ParseMessageID)
	})
	t.Run("task", func(t *testing.T) {
		assertIdentityCodecs(t, "tsk", ParseTaskID)
	})
	t.Run("run", func(t *testing.T) {
		assertIdentityCodecs(t, "run", ParseRunID)
	})
	t.Run("event", func(t *testing.T) {
		assertIdentityCodecs(t, "evt", ParseEventID)
	})
	t.Run("checkpoint", func(t *testing.T) {
		assertIdentityCodecs(t, "ckp", ParseCheckpointID)
	})
	t.Run("approval", func(t *testing.T) {
		assertIdentityCodecs(t, "apr", ParseApprovalID)
	})
	t.Run("graph", func(t *testing.T) {
		assertIdentityCodecs(t, "grf", ParseGraphID)
	})
	t.Run("graph revision", func(t *testing.T) {
		assertIdentityCodecs(t, "grv", ParseGraphRevisionID)
	})
	t.Run("node", func(t *testing.T) {
		assertIdentityCodecs(t, "nod", ParseNodeID)
	})
	t.Run("edge", func(t *testing.T) {
		assertIdentityCodecs(t, "edg", ParseEdgeID)
	})
	t.Run("validation", func(t *testing.T) {
		assertIdentityCodecs(t, "val", ParseValidationID)
	})
	t.Run("evidence", func(t *testing.T) {
		assertIdentityCodecs(t, "evd", ParseEvidenceID)
	})
	t.Run("artifact", func(t *testing.T) {
		assertIdentityCodecs(t, "art", ParseArtifactID)
	})
	t.Run("atom", func(t *testing.T) {
		assertIdentityCodecs(t, "atm", ParseAtomID)
	})
	t.Run("model request", func(t *testing.T) {
		assertIdentityCodecs(t, "mrq", ParseModelRequestID)
	})
	t.Run("provider", func(t *testing.T) {
		assertIdentityCodecs(t, "prv", ParseProviderID)
	})
	t.Run("budget", func(t *testing.T) {
		assertIdentityCodecs(t, "bdg", ParseBudgetID)
	})
	t.Run("episode", func(t *testing.T) {
		assertIdentityCodecs(t, "epi", ParseEpisodeID)
	})
	t.Run("memory artifact", func(t *testing.T) {
		assertIdentityCodecs(t, "mem", ParseMemoryArtifactID)
	})
	t.Run("memory artifact revision", func(t *testing.T) {
		assertIdentityCodecs(t, "mrv", ParseMemoryArtifactRevisionID)
	})
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

func assertIdentityCodecs[T parsedIdentity](
	t *testing.T,
	prefix string,
	parse func(string) (T, error),
) {
	t.Helper()
	raw := prefix + "_" + uuidV7Fixture
	original, err := parse(raw)
	if err != nil {
		t.Fatal(err)
	}

	encoded, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	var fromJSON T
	if err := json.Unmarshal(encoded, &fromJSON); err != nil {
		t.Fatalf("unmarshal JSON: %v", err)
	}
	if fromJSON.String() != raw {
		t.Fatalf("JSON identity = %q, want %q", fromJSON.String(), raw)
	}

	valuer, ok := any(original).(driver.Valuer)
	if !ok {
		t.Fatalf("%T does not implement driver.Valuer", original)
	}
	sqlValue, err := valuer.Value()
	if err != nil {
		t.Fatalf("SQL value: %v", err)
	}
	var fromSQL T
	scanner, ok := any(&fromSQL).(sql.Scanner)
	if !ok {
		t.Fatalf("*%T does not implement sql.Scanner", fromSQL)
	}
	if err := scanner.Scan(sqlValue); err != nil {
		t.Fatalf("SQL scan: %v", err)
	}
	if fromSQL.String() != raw {
		t.Fatalf("SQL identity = %q, want %q", fromSQL.String(), raw)
	}

	if err := json.Unmarshal([]byte("null"), &fromJSON); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("JSON null error = %v, want ErrInvalidID", err)
	}
	if err := scanner.Scan(nil); !errors.Is(err, ErrInvalidID) {
		t.Fatalf("SQL null error = %v, want ErrInvalidID", err)
	}
}
