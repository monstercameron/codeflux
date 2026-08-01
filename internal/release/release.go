// Package release describes what a CodeFlux release artifact is and verifies
// that one is complete, reproducible, checksummed, and signed (M23-052..063).
//
// The package models the contract rather than performing the build: a release
// is produced by the build system, and what has to be checkable in code is
// whether the thing it produced meets the contract.
package release

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"slices"
	"sort"
	"strings"
)

// Platform is one declared prototype target (M23-052).
//
// The set is deliberately small. Every platform here must be tested on real
// hardware before a release; a target nobody runs is a promise nobody keeps.
type Platform struct {
	OS   string
	Arch string
}

// String renders the platform as it appears in an artifact name.
func (platform Platform) String() string { return platform.OS + "-" + platform.Arch }

// SupportedPlatforms are the prototype targets (M23-052).
func SupportedPlatforms() []Platform {
	return []Platform{
		{OS: "windows", Arch: "amd64"},
		{OS: "windows", Arch: "arm64"},
		{OS: "darwin", Arch: "arm64"},
		{OS: "linux", Arch: "amd64"},
		{OS: "linux", Arch: "arm64"},
	}
}

// Supported reports whether a platform is declared.
func (platform Platform) Supported() bool {
	return slices.Contains(SupportedPlatforms(), platform)
}

// ExecutableName is the artifact name for this platform.
func (platform Platform) ExecutableName() string {
	name := "codeflux-" + platform.String()
	if platform.OS == "windows" {
		name += ".exe"
	}
	return name
}

// ReproducibilityFlags are the build settings a reproducible artifact requires
// (M23-052).
//
// Each removes one source of machine-specific content from the binary. Without
// them the same source produces different bytes on two machines, and a
// checksum stops meaning "this is that source".
func ReproducibilityFlags() []string {
	return []string{
		// -trimpath removes the building machine's directory layout.
		"-trimpath",
		// A pinned build ID keeps the binary from carrying the toolchain's
		// working state.
		"-buildvcs=false",
		// -s -w strip the symbol and DWARF tables, which carry paths.
		"-ldflags=-s -w",
	}
}

// RequiredContent is what every artifact must contain (M23-053..056).
type RequiredContent string

const (
	ContentFrontendAssets RequiredContent = "frontend-assets"
	ContentMigrations     RequiredContent = "migrations"
	ContentLicense        RequiredContent = "license"
	ContentNotices        RequiredContent = "third-party-notices"
	ContentVersion        RequiredContent = "version-metadata"
)

// AllRequiredContent returns everything an artifact must carry.
func AllRequiredContent() []RequiredContent {
	return []RequiredContent{
		ContentFrontendAssets, ContentMigrations,
		ContentLicense, ContentNotices, ContentVersion,
	}
}

// Describe explains why a piece of content is required.
func (content RequiredContent) Describe() string {
	switch content {
	case ContentFrontendAssets:
		return "the browser interface, embedded so a released executable needs nothing beside it"
	case ContentMigrations:
		return "the schema history, embedded so an executable can always open its own database"
	case ContentLicense:
		return "the licence this software is distributed under"
	case ContentNotices:
		return "the licences of the dependencies compiled into it"
	case ContentVersion:
		return "the version and commit, so a bug report identifies exactly what ran"
	default:
		return ""
	}
}

// Artifact is one built release file.
type Artifact struct {
	Name     string
	Platform Platform
	// SizeBytes is the artifact's size. Zero is refused: an empty artifact
	// that was checksummed and signed is still an empty artifact.
	SizeBytes int64
	// SHA256 is the checksum (M23-057).
	SHA256 string
	// Signature is the detached signature (M23-058).
	Signature string
	// Contains records the required content actually present.
	Contains []RequiredContent
}

// Validate rejects an artifact that could not be released.
func (artifact Artifact) Validate() error {
	switch {
	case strings.TrimSpace(artifact.Name) == "":
		return errors.New("an artifact requires a name")
	case !artifact.Platform.Supported():
		return fmt.Errorf("artifact %q targets undeclared platform %s",
			artifact.Name, artifact.Platform)
	case artifact.Name != artifact.Platform.ExecutableName():
		return fmt.Errorf("artifact %q does not match the name for %s (%q)",
			artifact.Name, artifact.Platform, artifact.Platform.ExecutableName())
	case artifact.SizeBytes <= 0:
		return fmt.Errorf("artifact %q is empty", artifact.Name)
	}
	if err := validateSHA256(artifact.SHA256); err != nil {
		return fmt.Errorf("artifact %q checksum: %w", artifact.Name, err)
	}
	if strings.TrimSpace(artifact.Signature) == "" {
		return fmt.Errorf("artifact %q is unsigned", artifact.Name)
	}
	var missing []string
	for _, required := range AllRequiredContent() {
		if !slices.Contains(artifact.Contains, required) {
			missing = append(missing, string(required))
		}
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		return fmt.Errorf("artifact %q is missing %s", artifact.Name, strings.Join(missing, ", "))
	}
	return nil
}

func validateSHA256(value string) error {
	if len(value) != 64 {
		return fmt.Errorf("a SHA-256 checksum is 64 hex characters, got %d", len(value))
	}
	if _, err := hex.DecodeString(value); err != nil {
		return errors.New("a SHA-256 checksum must be hexadecimal")
	}
	if strings.ToLower(value) != value {
		return errors.New("a SHA-256 checksum must be lower case, so two manifests compare equal")
	}
	return nil
}

// ComputeSHA256 returns the checksum of some content (M23-057).
func ComputeSHA256(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// Manifest is the complete release (M23-052, M23-057, M23-059).
type Manifest struct {
	Version   string
	Commit    string
	Artifacts []Artifact
}

// Validate checks a release is complete and internally consistent.
func (manifest Manifest) Validate() error {
	if strings.TrimSpace(manifest.Version) == "" {
		return errors.New("a release requires a version")
	}
	if strings.TrimSpace(manifest.Commit) == "" {
		return errors.New("a release requires a commit")
	}
	if len(manifest.Artifacts) == 0 {
		return errors.New("a release has no artifacts")
	}

	seen := map[string]bool{}
	covered := map[Platform]bool{}
	for _, artifact := range manifest.Artifacts {
		if err := artifact.Validate(); err != nil {
			return err
		}
		if seen[artifact.Name] {
			return fmt.Errorf("artifact %q appears twice", artifact.Name)
		}
		seen[artifact.Name] = true
		covered[artifact.Platform] = true
	}
	// Every declared platform must be built. A release that quietly skipped
	// one leaves those users on an older version with no way to know why.
	var absent []string
	for _, platform := range SupportedPlatforms() {
		if !covered[platform] {
			absent = append(absent, platform.String())
		}
	}
	if len(absent) > 0 {
		sort.Strings(absent)
		return fmt.Errorf("release %s has no artifact for %s",
			manifest.Version, strings.Join(absent, ", "))
	}
	return nil
}

// Verifier checks a detached signature (M23-059).
type Verifier func(artifact Artifact) error

// ErrSignatureInvalid reports a signature that did not verify.
var ErrSignatureInvalid = errors.New("artifact signature did not verify")

// VerifyRelease checks every artifact's signature before a release is
// published (M23-059).
//
// It verifies ALL artifacts and reports every failure, rather than stopping at
// the first: a release with two bad signatures should be fixed once, not
// discovered twice.
func VerifyRelease(manifest Manifest, verify Verifier) error {
	if err := manifest.Validate(); err != nil {
		return err
	}
	if verify == nil {
		return errors.New(
			"verifying a release requires a verifier; publishing unverified signatures " +
				"would make signing decorative")
	}
	var failures []string
	for _, artifact := range manifest.Artifacts {
		if err := verify(artifact); err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", artifact.Name, err))
		}
	}
	if len(failures) > 0 {
		sort.Strings(failures)
		return fmt.Errorf("%w: %s", ErrSignatureInvalid, strings.Join(failures, "; "))
	}
	return nil
}

// ChecksumFile renders the manifest as a standard checksum file (M23-057).
func (manifest Manifest) ChecksumFile() string {
	artifacts := append([]Artifact(nil), manifest.Artifacts...)
	sort.Slice(artifacts, func(left, right int) bool {
		return artifacts[left].Name < artifacts[right].Name
	})
	var builder strings.Builder
	for _, artifact := range artifacts {
		// Two spaces then the name is the sha256sum format, so the file can be
		// checked with the tool people already have.
		fmt.Fprintf(&builder, "%s  %s\n", artifact.SHA256, artifact.Name)
	}
	return builder.String()
}

// InstallLocation describes where a platform keeps user data (M23-060,
// M23-063).
type InstallLocation struct {
	Platform Platform
	// DataDirectory is where the database and backups live, as a shape rather
	// than a resolved path: the real one contains the user's name.
	DataDirectory string
	// Removable is what an uninstall may delete.
	Removable []string
	// Preserved is what an uninstall must leave alone.
	Preserved []string
}

// InstallLocations returns the per-platform layout (M23-060).
func InstallLocations() []InstallLocation {
	locations := make([]InstallLocation, 0, len(SupportedPlatforms()))
	for _, platform := range SupportedPlatforms() {
		location := InstallLocation{
			Platform: platform,
			Removable: []string{
				"the CodeFlux data directory",
				"the CodeFlux executable",
			},
			Preserved: []string{
				"your repositories",
				"your Git history",
				"credentials in the operating system credential store",
			},
		}
		switch platform.OS {
		case "windows":
			location.DataDirectory = "%LOCALAPPDATA%\\codeflux"
		case "darwin":
			location.DataDirectory = "~/Library/Application Support/codeflux"
		default:
			location.DataDirectory = "~/.local/share/codeflux"
		}
		locations = append(locations, location)
	}
	return locations
}

// Validate rejects a layout that would make an uninstall unsafe.
func (location InstallLocation) Validate() error {
	if !location.Platform.Supported() {
		return fmt.Errorf("undeclared platform %s", location.Platform)
	}
	if strings.TrimSpace(location.DataDirectory) == "" {
		return fmt.Errorf("%s has no data directory", location.Platform)
	}
	// A per-user location is required: installing into a machine-wide
	// directory would need administrator rights, which M23-062 forbids.
	if !strings.Contains(location.DataDirectory, "~") &&
		!strings.Contains(location.DataDirectory, "%LOCALAPPDATA%") {
		return fmt.Errorf("%s installs outside the user's own directory", location.Platform)
	}
	if len(location.Removable) == 0 {
		return fmt.Errorf("%s says nothing about what uninstall removes", location.Platform)
	}
	if len(location.Preserved) == 0 {
		return fmt.Errorf("%s says nothing about what uninstall preserves", location.Platform)
	}
	// The repository must be named as preserved. It is the thing a user is
	// actually worried about, and leaving it unstated is what makes people
	// afraid to uninstall.
	preserved := strings.ToLower(strings.Join(location.Preserved, " "))
	if !strings.Contains(preserved, "repositor") {
		return fmt.Errorf("%s does not promise repositories are preserved", location.Platform)
	}
	return nil
}

// PathHazard is a path shape an installation must handle (M23-061).
//
// Each is a real thing on real machines, and each has broken software before:
// a space needs quoting, a non-ASCII name needs correct encoding, and a very
// long path needs the platform's long-path handling.
func PathHazards() []string {
	return []string{
		`C:\Users\Ana María\Documents\my projects\codeflux`,
		`C:\Users\Ünïcödé\AppData\Local\codeflux`,
		"/home/josé/my projects/codeflux",
		"/Users/中文用户/Library/Application Support/codeflux",
		"/home/user/a directory with several spaces/codeflux",
	}
}

// NormalizeInstallPath checks a path is usable (M23-061).
//
// It refuses the shapes that cannot work rather than trying to repair them:
// silently rewriting a user's chosen path is how software ends up writing
// somewhere the user cannot find.
func NormalizeInstallPath(candidate string) (string, error) {
	trimmed := strings.TrimSpace(candidate)
	if trimmed == "" {
		return "", errors.New("an install path must not be empty")
	}
	if strings.ContainsRune(trimmed, 0) {
		return "", errors.New("an install path must not contain a null byte")
	}
	// Leading and trailing spaces in a path component are accepted by some
	// filesystems and silently trimmed by others, which makes the same path
	// mean two things. Refusing is the only stable answer.
	for _, component := range strings.FieldsFunc(trimmed, func(r rune) bool {
		return r == '/' || r == '\\'
	}) {
		if component != strings.TrimSpace(component) {
			return "", fmt.Errorf(
				"path component %q has leading or trailing space, which some filesystems drop",
				component)
		}
	}
	return path.Clean(strings.ReplaceAll(trimmed, "\\", "/")), nil
}

// RequiresAdministrator reports whether an install location needs elevation
// (M23-062).
//
// CodeFlux must never need it: a coding agent that asks for administrator
// rights to install is asking for far more trust than it needs.
func RequiresAdministrator(location InstallLocation) bool {
	directory := strings.ToLower(location.DataDirectory)
	machineWide := []string{
		"c:\\program files", "%programfiles%", "%programdata%",
		"/usr/local", "/opt/", "/library/application support",
	}
	for _, prefix := range machineWide {
		if strings.HasPrefix(directory, prefix) {
			return true
		}
	}
	return false
}

// UninstallInstructions renders what an uninstall does (M23-063).
func (location InstallLocation) UninstallInstructions() []string {
	lines := []string{
		fmt.Sprintf("On %s, CodeFlux keeps everything it stores in %s.",
			location.Platform.OS, location.DataDirectory),
		"",
		"Removing CodeFlux deletes:",
	}
	for _, item := range location.Removable {
		lines = append(lines, "  - "+item)
	}
	lines = append(lines, "", "It does NOT touch:")
	for _, item := range location.Preserved {
		lines = append(lines, "  - "+item)
	}
	lines = append(lines,
		"",
		"To keep your task history, copy the data directory before removing it, "+
			"or run `codeflux backup --output <path>` while CodeFlux is still installed.")
	return lines
}
