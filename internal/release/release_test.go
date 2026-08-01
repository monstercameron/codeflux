package release

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"codeflux.dev/codeflux/migrations"
	"codeflux.dev/codeflux/web/assets"
)

func completeArtifact(platform Platform) Artifact {
	return Artifact{
		Name:      platform.ExecutableName(),
		Platform:  platform,
		SizeBytes: 33 << 20,
		SHA256:    ComputeSHA256([]byte(platform.String())),
		Signature: "fixture-detached-signature",
		Contains:  AllRequiredContent(),
	}
}

func completeManifest() Manifest {
	manifest := Manifest{Version: "0.1.0", Commit: "abc1234"}
	for _, platform := range SupportedPlatforms() {
		manifest.Artifacts = append(manifest.Artifacts, completeArtifact(platform))
	}
	return manifest
}

// TestM23_052_EveryDeclaredPlatformIsBuiltReproducibly covers M23-052.
func TestM23_052_EveryDeclaredPlatformIsBuiltReproducibly(t *testing.T) {
	if len(SupportedPlatforms()) < 3 {
		t.Fatalf("only %d platforms are declared", len(SupportedPlatforms()))
	}
	for _, platform := range SupportedPlatforms() {
		if !platform.Supported() {
			t.Fatalf("declared platform %s does not validate", platform)
		}
		name := platform.ExecutableName()
		if platform.OS == "windows" && !strings.HasSuffix(name, ".exe") {
			t.Fatalf("windows artifact %q has no .exe suffix", name)
		}
		if platform.OS != "windows" && strings.HasSuffix(name, ".exe") {
			t.Fatalf("non-windows artifact %q has an .exe suffix", name)
		}
	}
	if (Platform{OS: "plan9", Arch: "amd64"}).Supported() {
		t.Fatal("an undeclared platform validated")
	}

	// Reproducibility is not a wish: the flags that make the same source
	// produce the same bytes must be declared, or a checksum proves nothing.
	flags := ReproducibilityFlags()
	for _, required := range []string{"-trimpath", "-buildvcs=false"} {
		found := false
		for _, flag := range flags {
			if strings.Contains(flag, required) {
				found = true
			}
		}
		if !found {
			t.Fatalf("the reproducible build does not pass %q", required)
		}
	}

	// A release missing any platform must be refused.
	manifest := completeManifest()
	manifest.Artifacts = manifest.Artifacts[1:]
	if err := manifest.Validate(); err == nil {
		t.Fatal("a release missing a platform validated")
	}
}

// TestM23_053_054_AssetsAndMigrationsAreEmbedded covers M23-053 and M23-054.
func TestM23_053_054_AssetsAndMigrationsAreEmbedded(t *testing.T) {
	// M23-054: migrations are embedded, and there are real ones.
	sources, err := migrations.Sources()
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}
	if len(sources) == 0 {
		t.Fatal("no migrations are embedded")
	}
	for _, source := range sources {
		if strings.TrimSpace(source.SQL) == "" {
			t.Fatalf("embedded migration %d is empty", source.Descriptor.Number)
		}
	}

	// M23-053: the asset resolver prefers embedded content and falls back to a
	// build directory in development. Both paths must work, and a partial set
	// must be refused rather than half-served.
	if assets.Embedded() {
		resolved, err := assets.Resolve("")
		if err != nil {
			t.Fatalf("resolve embedded assets: %v", err)
		}
		if resolved.Source != assets.SourceEmbedded {
			t.Fatalf("resolved source = %q", resolved.Source)
		}
		for _, required := range assets.RequiredAssets() {
			if _, err := resolved.Get(required); err != nil {
				t.Fatalf("embedded assets omit %q", required)
			}
		}
		return
	}

	// In a development checkout nothing is embedded, so Resolve must say so
	// rather than serving an empty page.
	if _, err := assets.Resolve(""); !errors.Is(err, assets.ErrAssetsUnavailable) {
		t.Fatalf("resolving with nothing available returned %v", err)
	}

	directory := t.TempDir()
	if err := os.MkdirAll(filepath.Join(directory, "bin"), 0o755); err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	// A partial set is refused: a server that started and then failed in the
	// browser gives the user an error they cannot act on.
	if err := os.WriteFile(filepath.Join(directory, "index.html"),
		[]byte("<html></html>"), 0o600); err != nil {
		t.Fatalf("write index: %v", err)
	}
	if _, err := assets.Resolve(directory); !errors.Is(err, assets.ErrAssetsUnavailable) {
		t.Fatalf("a partial asset set resolved: %v", err)
	}

	for _, file := range map[string]string{
		"wasm_exec.js":  "// shim",
		"bin/main.wasm": "\x00asm",
	} {
		_ = file
	}
	if err := os.WriteFile(filepath.Join(directory, "wasm_exec.js"),
		[]byte("// shim"), 0o600); err != nil {
		t.Fatalf("write shim: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, "bin", "main.wasm"),
		[]byte("\x00asm\x01\x00\x00\x00"), 0o600); err != nil {
		t.Fatalf("write wasm: %v", err)
	}
	resolved, err := assets.Resolve(directory)
	if err != nil {
		t.Fatalf("resolve directory assets: %v", err)
	}
	if resolved.Source != assets.SourceDirectory {
		t.Fatalf("resolved source = %q", resolved.Source)
	}
	if len(resolved.Paths()) != len(assets.RequiredAssets()) {
		t.Fatalf("resolved %v", resolved.Paths())
	}
	if _, err := resolved.Get("absent.js"); err == nil {
		t.Fatal("an absent asset resolved")
	}
}

// TestM23_055_056_ArtifactsCarryLicenceAndVersion covers M23-055 and M23-056.
func TestM23_055_056_ArtifactsCarryLicenceAndVersion(t *testing.T) {
	// Each required content must be declared and explained.
	for _, content := range AllRequiredContent() {
		if strings.TrimSpace(content.Describe()) == "" {
			t.Fatalf("required content %q has no explanation", content)
		}
	}
	for _, required := range []RequiredContent{
		ContentLicense, ContentNotices, ContentVersion,
	} {
		found := false
		for _, content := range AllRequiredContent() {
			if content == required {
				found = true
			}
		}
		if !found {
			t.Fatalf("%q is not required content", required)
		}
	}

	// An artifact missing any of it must be refused.
	for _, omitted := range AllRequiredContent() {
		artifact := completeArtifact(SupportedPlatforms()[0])
		artifact.Contains = nil
		for _, content := range AllRequiredContent() {
			if content != omitted {
				artifact.Contains = append(artifact.Contains, content)
			}
		}
		if err := artifact.Validate(); err == nil {
			t.Fatalf("an artifact missing %q validated", omitted)
		}
	}

	// The repository must actually carry a licence file, or the requirement is
	// a declaration with nothing behind it.
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}
	licence, err := os.ReadFile(filepath.Join(root, "LICENSE"))
	if err != nil {
		t.Fatalf("the repository has no LICENSE: %v", err)
	}
	if len(strings.TrimSpace(string(licence))) < 100 {
		t.Fatal("the LICENSE file is too short to be a licence")
	}
}

// TestM23_057_ChecksumsAreComputedAndRendered covers M23-057.
func TestM23_057_ChecksumsAreComputedAndRendered(t *testing.T) {
	sum := ComputeSHA256([]byte("codeflux"))
	if len(sum) != 64 {
		t.Fatalf("checksum length = %d", len(sum))
	}
	if strings.ToLower(sum) != sum {
		t.Fatal("checksum is not lower case")
	}
	if ComputeSHA256([]byte("codeflux")) != sum {
		t.Fatal("checksums are not deterministic")
	}
	if ComputeSHA256([]byte("codefluy")) == sum {
		t.Fatal("different content produced the same checksum")
	}

	for _, bad := range []string{
		"", "short", strings.Repeat("z", 64), strings.ToUpper(sum),
	} {
		artifact := completeArtifact(SupportedPlatforms()[0])
		artifact.SHA256 = bad
		if err := artifact.Validate(); err == nil {
			t.Fatalf("checksum %q validated", bad)
		}
	}

	// The checksum file must be in the format the tool people already have
	// expects, and must be deterministically ordered.
	manifest := completeManifest()
	file := manifest.ChecksumFile()
	lines := strings.Split(strings.TrimSpace(file), "\n")
	if len(lines) != len(manifest.Artifacts) {
		t.Fatalf("the checksum file has %d lines for %d artifacts",
			len(lines), len(manifest.Artifacts))
	}
	previous := ""
	for _, line := range lines {
		parts := strings.SplitN(line, "  ", 2)
		if len(parts) != 2 || len(parts[0]) != 64 {
			t.Fatalf("malformed checksum line: %q", line)
		}
		if parts[1] <= previous {
			t.Fatalf("the checksum file is not sorted by name: %q after %q", parts[1], previous)
		}
		previous = parts[1]
	}
	if manifest.ChecksumFile() != file {
		t.Fatal("the checksum file is not deterministic")
	}
}

// TestM23_058_059_ArtifactsAreSignedAndVerified covers M23-058 and M23-059.
func TestM23_058_059_ArtifactsAreSignedAndVerified(t *testing.T) {
	// M23-058: an unsigned artifact is refused.
	artifact := completeArtifact(SupportedPlatforms()[0])
	artifact.Signature = ""
	if err := artifact.Validate(); err == nil {
		t.Fatal("an unsigned artifact validated")
	}

	manifest := completeManifest()
	verified := 0
	if err := VerifyRelease(manifest, func(Artifact) error {
		verified++
		return nil
	}); err != nil {
		t.Fatalf("verify release: %v", err)
	}
	// Every artifact must be verified, not just the first.
	if verified != len(manifest.Artifacts) {
		t.Fatalf("verified %d of %d artifacts", verified, len(manifest.Artifacts))
	}

	// A failure must name every bad artifact, so a release is fixed once.
	err := VerifyRelease(manifest, func(candidate Artifact) error {
		if strings.HasPrefix(candidate.Name, "codeflux-linux") {
			return errors.New("bad signature")
		}
		return nil
	})
	if !errors.Is(err, ErrSignatureInvalid) {
		t.Fatalf("verification returned %v", err)
	}
	for _, platform := range []string{"linux-amd64", "linux-arm64"} {
		if !strings.Contains(err.Error(), platform) {
			t.Fatalf("the failure does not name %s: %v", platform, err)
		}
	}

	// Publishing with no verifier must be refused, or signing is decorative.
	if err := VerifyRelease(manifest, nil); err == nil {
		t.Fatal("a release was verified with no verifier")
	}
	// An invalid manifest must be refused before any signature is checked.
	if err := VerifyRelease(Manifest{}, func(Artifact) error { return nil }); err == nil {
		t.Fatal("an empty manifest verified")
	}
}

// TestM23_060_062_InstallationIsPerUserAndNeedsNoAdministrator covers M23-060
// and M23-062.
func TestM23_060_062_InstallationIsPerUserAndNeedsNoAdministrator(t *testing.T) {
	locations := InstallLocations()
	if len(locations) != len(SupportedPlatforms()) {
		t.Fatalf("%d locations for %d platforms", len(locations), len(SupportedPlatforms()))
	}
	for _, location := range locations {
		if err := location.Validate(); err != nil {
			t.Fatalf("install location for %s is invalid: %v", location.Platform, err)
		}
		// M23-062: a coding agent asking for administrator rights to install
		// is asking for far more trust than it needs.
		if RequiresAdministrator(location) {
			t.Fatalf("%s installs somewhere needing administrator rights: %s",
				location.Platform, location.DataDirectory)
		}
	}

	// The check must be capable of saying yes, or it proves nothing.
	for _, directory := range []string{
		`C:\Program Files\codeflux`, "/usr/local/share/codeflux", "/opt/codeflux",
	} {
		if !RequiresAdministrator(InstallLocation{
			Platform: SupportedPlatforms()[0], DataDirectory: directory,
		}) {
			t.Fatalf("%q was not recognised as needing administrator rights", directory)
		}
	}

	// A layout that says nothing about what it removes or preserves is unsafe.
	for name, corrupt := range map[string]func(InstallLocation) InstallLocation{
		"no data directory": func(location InstallLocation) InstallLocation {
			location.DataDirectory = ""
			return location
		},
		"machine-wide": func(location InstallLocation) InstallLocation {
			location.DataDirectory = "/etc/codeflux"
			return location
		},
		"no removable list": func(location InstallLocation) InstallLocation {
			location.Removable = nil
			return location
		},
		"no preserved list": func(location InstallLocation) InstallLocation {
			location.Preserved = nil
			return location
		},
		"silent about repositories": func(location InstallLocation) InstallLocation {
			location.Preserved = []string{"something else"}
			return location
		},
		"undeclared platform": func(location InstallLocation) InstallLocation {
			location.Platform = Platform{OS: "plan9", Arch: "amd64"}
			return location
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := corrupt(locations[0]).Validate(); err == nil {
				t.Fatalf("an unsafe layout validated: %s", name)
			}
		})
	}
}

// TestM23_061_PathsWithSpacesAndNonASCIIAreHandled covers M23-061.
func TestM23_061_PathsWithSpacesAndNonASCIIAreHandled(t *testing.T) {
	for _, hazard := range PathHazards() {
		normalized, err := NormalizeInstallPath(hazard)
		if err != nil {
			t.Fatalf("a real-world path was refused: %q: %v", hazard, err)
		}
		if strings.TrimSpace(normalized) == "" {
			t.Fatalf("normalizing %q produced nothing", hazard)
		}
	}
	// At least one hazard must actually contain each shape, or the list is
	// not exercising what it claims.
	joined := strings.Join(PathHazards(), " ")
	if !strings.Contains(joined, " ") {
		t.Fatal("no path hazard contains a space")
	}
	hasNonASCII := false
	for _, character := range joined {
		if character > 127 {
			hasNonASCII = true
			break
		}
	}
	if !hasNonASCII {
		t.Fatal("no path hazard contains a non-ASCII character")
	}

	// The shapes that cannot work are refused rather than repaired.
	for _, bad := range []string{
		"", "   ", "with\x00null", "/home/user /codeflux", "/home/ user/codeflux",
	} {
		if _, err := NormalizeInstallPath(bad); err == nil {
			t.Fatalf("an unusable path was accepted: %q", bad)
		}
	}

	// Real filesystem behaviour: a directory with a space and a non-ASCII name
	// must actually be creatable and writable, since that is what an install
	// does.
	root := t.TempDir()
	for _, name := range []string{"my projects", "Ünïcödé", "中文 目录"} {
		directory := filepath.Join(root, name)
		if err := os.MkdirAll(directory, 0o755); err != nil {
			t.Fatalf("create %q: %v", name, err)
		}
		file := filepath.Join(directory, "codeflux.sqlite3")
		if err := os.WriteFile(file, []byte("fixture"), 0o600); err != nil {
			t.Fatalf("write into %q: %v", name, err)
		}
		content, err := os.ReadFile(file)
		if err != nil || string(content) != "fixture" {
			t.Fatalf("read back from %q: %q %v", name, content, err)
		}
	}
}

// TestM23_063_UninstallInstructionsAreExplicitAboutUserData covers M23-063.
func TestM23_063_UninstallInstructionsAreExplicitAboutUserData(t *testing.T) {
	for _, location := range InstallLocations() {
		instructions := strings.Join(location.UninstallInstructions(), "\n")
		lowered := strings.ToLower(instructions)

		// Both halves must be stated: what goes, and what stays.
		if !strings.Contains(instructions, "deletes:") {
			t.Fatalf("%s does not say what is deleted:\n%s", location.Platform, instructions)
		}
		if !strings.Contains(instructions, "NOT touch") {
			t.Fatalf("%s does not say what is preserved:\n%s", location.Platform, instructions)
		}
		// The repository is the thing a user is actually worried about.
		if !strings.Contains(lowered, "repositor") {
			t.Fatalf("%s does not mention repositories:\n%s", location.Platform, instructions)
		}
		// And there must be a way to keep the history before removing it.
		if !strings.Contains(lowered, "backup") {
			t.Fatalf("%s offers no way to keep task history:\n%s",
				location.Platform, instructions)
		}
		if !strings.Contains(instructions, location.DataDirectory) {
			t.Fatalf("%s does not name the data directory:\n%s",
				location.Platform, instructions)
		}
	}
}

// TestM23_052_ArtifactValidationIsLoadBearing proves a malformed artifact
// cannot be released.
func TestM23_052_ArtifactValidationIsLoadBearing(t *testing.T) {
	valid := completeArtifact(SupportedPlatforms()[0])
	if err := valid.Validate(); err != nil {
		t.Fatalf("a complete artifact was rejected: %v", err)
	}
	corruptions := map[string]func(Artifact) Artifact{
		"no name": func(artifact Artifact) Artifact {
			artifact.Name = ""
			return artifact
		},
		"undeclared platform": func(artifact Artifact) Artifact {
			artifact.Platform = Platform{OS: "plan9", Arch: "amd64"}
			return artifact
		},
		"mismatched name": func(artifact Artifact) Artifact {
			artifact.Name = "something-else"
			return artifact
		},
		"empty artifact": func(artifact Artifact) Artifact {
			artifact.SizeBytes = 0
			return artifact
		},
	}
	for name, corrupt := range corruptions {
		t.Run(name, func(t *testing.T) {
			if err := corrupt(valid).Validate(); err == nil {
				t.Fatalf("an unreleasable artifact validated: %s", name)
			}
		})
	}

	manifest := completeManifest()
	manifest.Artifacts = append(manifest.Artifacts, manifest.Artifacts[0])
	if err := manifest.Validate(); err == nil {
		t.Fatal("a manifest with a duplicate artifact validated")
	}
	for _, bad := range []Manifest{
		{Commit: "abc"}, {Version: "0.1.0"}, {Version: "0.1.0", Commit: "abc"},
	} {
		if err := bad.Validate(); err == nil {
			t.Fatalf("an incomplete manifest validated: %+v", bad)
		}
	}
}
