package testfixtures

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// CredentialBoundary names a place a secret must not cross (M22-108).
//
// The set mirrors the redaction boundaries the product declares, because a
// fake store that invented its own boundary names could assert a property the
// real system never enforces.
type CredentialBoundary string

const (
	BoundaryPrompt     CredentialBoundary = "prompt-persistence"
	BoundaryLog        CredentialBoundary = "log-persistence"
	BoundaryUI         CredentialBoundary = "ui-delivery"
	BoundaryDiagnostic CredentialBoundary = "diagnostic-export"
	BoundaryEvent      CredentialBoundary = "durable-event"
	BoundaryArtifact   CredentialBoundary = "failure-artifact"
)

// AllCredentialBoundaries returns every boundary a fake store watches.
func AllCredentialBoundaries() []CredentialBoundary {
	return []CredentialBoundary{
		BoundaryPrompt, BoundaryLog, BoundaryUI,
		BoundaryDiagnostic, BoundaryEvent, BoundaryArtifact,
	}
}

// ErrCredentialCrossedBoundary reports a secret reaching a watched boundary.
var ErrCredentialCrossedBoundary = errors.New("credential crossed a boundary it must not cross")

// FakeCredentialStore holds synthetic secrets and watches for them escaping
// (M22-108).
//
// Its job is not to hold credentials — anything could do that — but to answer
// one question a test cannot otherwise answer: did this exact secret material
// appear in text that crossed a boundary? Asserting on a redaction function's
// return value proves the function works; this proves the system used it.
type FakeCredentialStore struct {
	mutex      sync.RWMutex
	secrets    map[string]string
	crossings  []Crossing
	watchedSet map[CredentialBoundary]bool
}

// Crossing is one recorded escape.
type Crossing struct {
	SecretName string
	Boundary   CredentialBoundary
	// Context is a short, redacted description of where it was seen. The
	// surrounding text is deliberately NOT captured: a leak report that
	// quoted the leaking text would leak it again.
	Context string
}

// NewFakeCredentialStore builds a store watching every boundary.
func NewFakeCredentialStore() *FakeCredentialStore {
	watched := make(map[CredentialBoundary]bool, len(AllCredentialBoundaries()))
	for _, boundary := range AllCredentialBoundaries() {
		watched[boundary] = true
	}
	return &FakeCredentialStore{
		secrets:    map[string]string{},
		watchedSet: watched,
	}
}

// Put stores synthetic secret material under a name.
//
// It refuses material that does not look synthetic. A fixture holding a real
// credential would be a credential committed to the repository, and the
// refusal has to be here rather than in review.
func (store *FakeCredentialStore) Put(name, material string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("a fixture credential requires a name")
	}
	if len(material) < 8 {
		return fmt.Errorf("credential %q is too short to be distinguishable in output", name)
	}
	if !strings.Contains(strings.ToLower(material), "fixture") &&
		!strings.Contains(strings.ToLower(material), "not-a-real") {
		return fmt.Errorf(
			"credential %q does not identify itself as synthetic; fixture material must contain "+
				"\"fixture\" or \"not-a-real\" so a leak is unmistakably a test artifact", name)
	}
	store.mutex.Lock()
	defer store.mutex.Unlock()
	store.secrets[name] = material
	return nil
}

// Seed loads the package's standard fixture material.
func (store *FakeCredentialStore) Seed() error {
	return store.Put("provider-api-key", FixtureCredentialMaterial)
}

// Get returns stored material.
func (store *FakeCredentialStore) Get(name string) (string, bool) {
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	material, ok := store.secrets[name]
	return material, ok
}

// Inspect checks one piece of text about to cross a boundary and records any
// secret found in it.
//
// It returns an error as well as recording, so a test can either fail
// immediately at the crossing or collect every crossing and assert at the end.
func (store *FakeCredentialStore) Inspect(
	boundary CredentialBoundary,
	context string,
	text string,
) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()
	if !store.watchedSet[boundary] {
		return fmt.Errorf("boundary %q is not watched", boundary)
	}
	var found []string
	for name, material := range store.secrets {
		if strings.Contains(text, material) {
			found = append(found, name)
			store.crossings = append(store.crossings, Crossing{
				SecretName: name, Boundary: boundary, Context: context,
			})
		}
	}
	if len(found) == 0 {
		return nil
	}
	sort.Strings(found)
	return fmt.Errorf("%w: %s reached %s at %s",
		ErrCredentialCrossedBoundary, strings.Join(found, ", "), boundary, context)
}

// Crossings returns every recorded escape. The required answer in every test
// is an empty slice.
func (store *FakeCredentialStore) Crossings() []Crossing {
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	crossings := make([]Crossing, len(store.crossings))
	copy(crossings, store.crossings)
	return crossings
}

// AssertNoCrossings returns an error naming every boundary a secret reached.
func (store *FakeCredentialStore) AssertNoCrossings() error {
	crossings := store.Crossings()
	if len(crossings) == 0 {
		return nil
	}
	descriptions := make([]string, 0, len(crossings))
	for _, crossing := range crossings {
		descriptions = append(descriptions,
			fmt.Sprintf("%s at %s (%s)", crossing.SecretName, crossing.Boundary, crossing.Context))
	}
	sort.Strings(descriptions)
	return fmt.Errorf("%w: %s", ErrCredentialCrossedBoundary, strings.Join(descriptions, "; "))
}

// Names returns the stored credential names, sorted.
func (store *FakeCredentialStore) Names() []string {
	store.mutex.RLock()
	defer store.mutex.RUnlock()
	names := make([]string, 0, len(store.secrets))
	for name := range store.secrets {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}
