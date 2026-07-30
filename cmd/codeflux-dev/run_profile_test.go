package main

import (
	"bytes"
	"encoding/json"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestDevelopmentProfilesDeclareRequiredBoundaries(t *testing.T) {
	profiles := developmentProfiles()
	if len(profiles) != 4 {
		t.Fatalf("profiles = %d, want 4", len(profiles))
	}
	wantNames := []string{"deterministic", "interactive-fake", "live-provider", "fault-injection"}
	for index, want := range wantNames {
		if profiles[index].Name != want {
			t.Errorf("profile %d = %q, want %q", index, profiles[index].Name, want)
		}
	}
	if profiles[0].ExternalProvider || profiles[0].Network != "loopback listener only; external network disabled" {
		t.Fatalf("deterministic network boundary = %#v", profiles[0])
	}
	if !profiles[2].ExternalProvider {
		t.Fatal("live-provider profile does not declare external provider access")
	}
	if len(profiles[3].FaultBoundaries) != 13 {
		t.Fatalf("fault boundaries = %d, want 13", len(profiles[3].FaultBoundaries))
	}
}

func TestRunDeterministicProfileOnceUsesLoopbackAndFakeState(t *testing.T) {
	root := t.TempDir()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	code := runDeterministicProfile(t.Context(), &stdout, &stderr, commandInvocation{
		JSON: true,
		Once: true,
		Root: root,
	})
	if code != exitSuccess {
		t.Fatalf("run exit = %d, stderr=%q", code, stderr.String())
	}
	var result deterministicRunResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("decode startup JSON: %v\n%s", err, stdout.String())
	}
	address, err := url.Parse(result.Address)
	if err != nil {
		t.Fatal(err)
	}
	host, _, err := net.SplitHostPort(address.Host)
	if err != nil {
		t.Fatal(err)
	}
	if !net.ParseIP(host).IsLoopback() {
		t.Fatalf("listener host = %q, want loopback", host)
	}
	info, err := os.Stat(result.Database)
	if err != nil {
		t.Fatalf("SQLite target: %v", err)
	}
	if info.Size() != 0 || filepath.Ext(result.Database) != ".sqlite" {
		t.Fatalf("SQLite target = %s (%d bytes)", result.Database, info.Size())
	}
	if result.Profile.CredentialStore != "in-memory fake credentials" ||
		result.Profile.Provider != "fixed scripted provider" ||
		result.Profile.ExternalProvider {
		t.Fatalf("deterministic dependencies = %#v", result.Profile)
	}
	credentialFiles, err := filepath.Glob(filepath.Join(filepath.Dir(result.Database), "*credential*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(credentialFiles) != 0 {
		t.Fatalf("fake credentials were persisted: %v", credentialFiles)
	}
}

func TestIsRepositoryArtifactChild(t *testing.T) {
	root := t.TempDir()
	if !isRepositoryArtifactChild(root, filepath.Join(root, ".artifacts", "run", "child")) {
		t.Fatal("valid artifact child was rejected")
	}
	if isRepositoryArtifactChild(root, filepath.Join(root, ".artifacts")) {
		t.Fatal("artifact root was accepted as child")
	}
	if isRepositoryArtifactChild(root, filepath.Join(root, "source")) {
		t.Fatal("source path was accepted as artifact child")
	}
}
