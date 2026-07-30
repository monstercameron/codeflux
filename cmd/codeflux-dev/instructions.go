package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const pinnedKarpathyReference = "https://github.com/multica-ai/andrej-karpathy-skills/tree/2c606141936f1eeef17fa3043a72095b4765b9c2"

var markdownLinkPattern = regexp.MustCompile(`\[[^\]]+\]\(([^)]+)\)`)

func checkInstructionFiles(root string, tracked []string) error {
	trackedSet := make(map[string]struct{}, len(tracked))
	for _, path := range tracked {
		trackedSet[filepath.ToSlash(path)] = struct{}{}
	}
	for _, required := range []string{"AGENTS.md", "CLAUDE.md"} {
		if _, ok := trackedSet[required]; !ok {
			return fmt.Errorf("required instruction file is not tracked: %s", required)
		}
	}

	agent, err := os.ReadFile(filepath.Join(root, "AGENTS.md"))
	if err != nil {
		return fmt.Errorf("read AGENTS.md: %w", err)
	}
	claude, err := os.ReadFile(filepath.Join(root, "CLAUDE.md"))
	if err != nil {
		return fmt.Errorf("read CLAUDE.md: %w", err)
	}
	for _, target := range []string{"AGENTS.md", "docs/plan.md", "TODOS.md"} {
		if !bytes.Contains(claude, []byte(target)) {
			return fmt.Errorf("CLAUDE.md must point to %s", target)
		}
	}
	if bytes.Count(claude, []byte{'\n'}) > 30 {
		return fmt.Errorf("CLAUDE.md must remain a thin compatibility entry point")
	}
	for _, duplicatedHeader := range []string{
		"## Non-Negotiable Product Boundaries",
		"## Go Engineering Rules",
		"## Security and Authority Rules",
		"## Frontend Rules",
	} {
		if bytes.Contains(claude, []byte(duplicatedHeader)) {
			return fmt.Errorf("CLAUDE.md duplicates authoritative section %q", duplicatedHeader)
		}
	}
	for _, document := range []struct {
		path    string
		content []byte
	}{
		{path: "AGENTS.md", content: agent},
		{path: "CLAUDE.md", content: claude},
	} {
		if !bytes.Contains(document.content, []byte(pinnedKarpathyReference)) {
			return fmt.Errorf("%s must contain the pinned Karpathy-inspired reference", document.path)
		}
		if !bytes.Contains(document.content, []byte("community-maintained")) ||
			!bytes.Contains(document.content, []byte("not an official Andrej Karpathy repository")) {
			return fmt.Errorf("%s must label the reference as community-maintained and unofficial", document.path)
		}
		if err := checkRepositoryRelativeMarkdownLinks(root, document.path, document.content); err != nil {
			return err
		}
	}
	if !bytes.Contains(agent, []byte("Never create a new Markdown file unless the user explicitly requests that specific file.")) {
		return fmt.Errorf("AGENTS.md must retain the explicit Markdown-creation prohibition")
	}
	return checkInstructionSmokeScenario(root)
}

func checkRepositoryRelativeMarkdownLinks(root, sourcePath string, content []byte) error {
	matches := markdownLinkPattern.FindAllSubmatch(content, -1)
	for _, match := range matches {
		target := string(match[1])
		if strings.HasPrefix(target, "https://") || strings.HasPrefix(target, "http://") ||
			strings.HasPrefix(target, "#") {
			continue
		}
		pathPart, _, _ := strings.Cut(target, "#")
		if pathPart == "" {
			continue
		}
		resolved := filepath.Clean(filepath.Join(root, filepath.Dir(sourcePath), filepath.FromSlash(pathPart)))
		relative, err := filepath.Rel(root, resolved)
		if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("%s link escapes repository: %s", sourcePath, target)
		}
		if info, err := os.Stat(resolved); err != nil || info.IsDir() {
			return fmt.Errorf("%s has unresolved repository link: %s", sourcePath, target)
		}
	}
	return nil
}

func checkInstructionSmokeScenario(root string) error {
	path := filepath.Join(root, "testdata", "instructions", "smoke-valid.txt")
	content, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read instruction smoke scenario: %w", err)
	}
	return validateInstructionSmokeScenario(root, string(content))
}

func validateInstructionSmokeScenario(root, content string) error {
	lines := strings.Split(content, "\n")
	positions := map[string]int{}
	values := map[string]string{}
	for index, line := range lines {
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		switch key {
		case "PLAN", "TODO", "EDIT":
			if _, exists := positions[key]; exists {
				return fmt.Errorf("instruction smoke scenario repeats %s", key)
			}
			positions[key] = index
			values[key] = strings.TrimSpace(value)
		}
	}
	for _, key := range []string{"PLAN", "TODO", "EDIT"} {
		if values[key] == "" {
			return fmt.Errorf("instruction smoke scenario requires %s", key)
		}
	}
	if positions["PLAN"] >= positions["EDIT"] || positions["TODO"] >= positions["EDIT"] {
		return fmt.Errorf("instruction smoke scenario must identify PLAN and TODO before EDIT")
	}
	planPath, _, hasAnchor := strings.Cut(values["PLAN"], "#")
	if planPath != "docs/plan.md" || !hasAnchor {
		return fmt.Errorf("instruction smoke PLAN must identify an anchored docs/plan.md section")
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(planPath))); err != nil {
		return fmt.Errorf("instruction smoke PLAN target: %w", err)
	}
	todoContent, err := os.ReadFile(filepath.Join(root, "TODOS.md"))
	if err != nil {
		return fmt.Errorf("read TODOS.md for smoke scenario: %w", err)
	}
	if !regexp.MustCompile(`^M[0-9]{2}-[0-9]{3}$`).MatchString(values["TODO"]) ||
		!bytes.Contains(todoContent, []byte("`"+values["TODO"]+" ")) {
		return fmt.Errorf("instruction smoke TODO is not a declared atomic task: %s", values["TODO"])
	}
	editPath := filepath.Clean(filepath.Join(root, filepath.FromSlash(values["EDIT"])))
	relative, err := filepath.Rel(root, editPath)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return fmt.Errorf("instruction smoke EDIT escapes repository")
	}
	return nil
}
