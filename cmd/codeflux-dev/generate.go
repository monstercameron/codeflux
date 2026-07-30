package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"go/format"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var eventDirectivePattern = regexp.MustCompile(`(?m)^//codeflux:event ([a-z][a-z0-9]*(?:\.[a-z0-9]+)*)$`)

var generatedRepositoryPaths = []string{
	"api/gen",
	"internal/buildinfo/versions_gen.go",
	"internal/events/registry_gen.go",
	"migrations/catalog_gen.go",
	"web/assets/manifest_gen.go",
}

func generateRepositorySource(sourceRoot, outputRoot string) error {
	migrations, err := migrationDescriptors(sourceRoot)
	if err != nil {
		return err
	}
	assets, frontendVersion, err := frontendAssetDescriptors(sourceRoot)
	if err != nil {
		return err
	}
	events, err := eventKindNames(sourceRoot)
	if err != nil {
		return err
	}
	outputs := map[string]string{
		"migrations/catalog_gen.go":          renderMigrationCatalog(migrations),
		"web/assets/manifest_gen.go":         renderFrontendManifest(assets),
		"internal/events/registry_gen.go":    renderEventRegistry(events),
		"internal/buildinfo/versions_gen.go": renderGeneratedVersions(len(migrations)-1, frontendVersion),
	}
	for relative, source := range outputs {
		if err := writeFormattedGeneratedGo(outputRoot, relative, source); err != nil {
			return err
		}
	}
	return nil
}

func migrationDescriptors(root string) ([]migrationDescriptor, error) {
	entries, err := os.ReadDir(filepath.Join(root, "migrations"))
	if err != nil {
		return nil, fmt.Errorf("read migration inputs: %w", err)
	}
	var descriptors []migrationDescriptor
	for _, entry := range entries {
		match := migrationNamePattern.FindStringSubmatch(entry.Name())
		if entry.IsDir() || match == nil {
			continue
		}
		number, err := strconv.Atoi(match[1])
		if err != nil {
			return nil, err
		}
		content, err := os.ReadFile(filepath.Join(root, "migrations", entry.Name()))
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(content)
		descriptors = append(descriptors, migrationDescriptor{
			Number: number,
			Name:   entry.Name(),
			SHA256: hex.EncodeToString(sum[:]),
		})
	}
	sort.Slice(descriptors, func(i, j int) bool {
		return descriptors[i].Number < descriptors[j].Number
	})
	return descriptors, nil
}

type migrationDescriptor struct {
	Number int
	Name   string
	SHA256 string
}

type assetDescriptor struct {
	Path   string
	SHA256 string
}

func frontendAssetDescriptors(root string) ([]assetDescriptor, string, error) {
	assetRoot := filepath.Join(root, "web", "assets", "static")
	var descriptors []assetDescriptor
	err := filepath.WalkDir(assetRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(filepath.Join(root, "web", "assets"), path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(content)
		descriptors = append(descriptors, assetDescriptor{
			Path:   filepath.ToSlash(relative),
			SHA256: hex.EncodeToString(sum[:]),
		})
		return nil
	})
	if err != nil {
		return nil, "", fmt.Errorf("read frontend asset inputs: %w", err)
	}
	sort.Slice(descriptors, func(i, j int) bool {
		return descriptors[i].Path < descriptors[j].Path
	})
	hasher := sha256.New()
	for _, descriptor := range descriptors {
		fmt.Fprintf(hasher, "%s\x00%s\x00", descriptor.Path, descriptor.SHA256)
	}
	version := "assets-" + hex.EncodeToString(hasher.Sum(nil))[:12]
	return descriptors, version, nil
}

func eventKindNames(root string) ([]string, error) {
	eventRoot := filepath.Join(root, "internal", "events")
	var names []string
	err := filepath.WalkDir(eventRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".go" || strings.HasSuffix(path, "_gen.go") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, match := range eventDirectivePattern.FindAllSubmatch(content, -1) {
			names = append(names, string(match[1]))
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("read event registry inputs: %w", err)
	}
	sort.Strings(names)
	for index := 1; index < len(names); index++ {
		if names[index] == names[index-1] {
			return nil, fmt.Errorf("event kind %q is duplicated", names[index])
		}
	}
	return names, nil
}

func renderMigrationCatalog(descriptors []migrationDescriptor) string {
	var output strings.Builder
	writeGeneratedHeader(&output, "migrations/*.sql")
	output.WriteString("package migrations\n\n")
	output.WriteString("var Catalog = []Descriptor{\n")
	for _, descriptor := range descriptors {
		fmt.Fprintf(&output, "\t{Number: %d, Name: %q, SHA256: %q},\n", descriptor.Number, descriptor.Name, descriptor.SHA256)
	}
	output.WriteString("}\n")
	return output.String()
}

func renderFrontendManifest(descriptors []assetDescriptor) string {
	var output strings.Builder
	writeGeneratedHeader(&output, "web/assets/static")
	output.WriteString("package assets\n\n")
	output.WriteString("var Manifest = []Descriptor{\n")
	for _, descriptor := range descriptors {
		fmt.Fprintf(&output, "\t{Path: %q, SHA256: %q},\n", descriptor.Path, descriptor.SHA256)
	}
	output.WriteString("}\n")
	return output.String()
}

func renderEventRegistry(names []string) string {
	var output strings.Builder
	writeGeneratedHeader(&output, "internal/events //codeflux:event directives")
	output.WriteString("package events\n\n")
	output.WriteString("var Registry = []KindDescriptor{\n")
	for _, name := range names {
		fmt.Fprintf(&output, "\t{Name: %q},\n", name)
	}
	output.WriteString("}\n")
	return output.String()
}

func renderGeneratedVersions(schemaVersion int, frontendVersion string) string {
	var output strings.Builder
	writeGeneratedHeader(&output, "migration catalog and frontend asset manifest")
	output.WriteString("package buildinfo\n\n")
	fmt.Fprintf(&output, "const generatedSchemaVersion uint32 = %d\n", schemaVersion)
	fmt.Fprintf(&output, "const generatedFrontendVersion = %q\n", frontendVersion)
	return output.String()
}

func writeGeneratedHeader(output *strings.Builder, source string) {
	output.WriteString("// Code generated by codeflux-dev generate. DO NOT EDIT.\n")
	fmt.Fprintf(output, "// Source: %s; generator schema: 1.\n\n", source)
}

func writeFormattedGeneratedGo(root, relative, source string) error {
	formatted, err := format.Source([]byte(source))
	if err != nil {
		return fmt.Errorf("format generated %s: %w", relative, err)
	}
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create generated directory for %s: %w", relative, err)
	}
	if err := os.WriteFile(path, formatted, 0o644); err != nil {
		return fmt.Errorf("write generated %s: %w", relative, err)
	}
	return nil
}
