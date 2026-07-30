package worker

import "strings"

// SanitizeEnvironment removes provider credentials before launching a task
// worker. Non-secret process settings remain available to tools.
func SanitizeEnvironment(parent []string, additionalSensitive []string) []string {
	explicit := make(map[string]struct{}, len(additionalSensitive))
	for _, name := range additionalSensitive {
		explicit[strings.ToUpper(strings.TrimSpace(name))] = struct{}{}
	}
	sanitized := make([]string, 0, len(parent))
	for _, entry := range parent {
		name, _, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		normalized := strings.ToUpper(name)
		if _, found := explicit[normalized]; found || sensitiveEnvironmentName(normalized) {
			continue
		}
		sanitized = append(sanitized, entry)
	}
	return sanitized
}

func sensitiveEnvironmentName(name string) bool {
	switch name {
	case "OPENAI_API_KEY",
		"ANTHROPIC_API_KEY",
		"AZURE_OPENAI_API_KEY",
		"GITHUB_TOKEN",
		"GITLAB_TOKEN",
		"BITBUCKET_TOKEN",
		"AWS_SECRET_ACCESS_KEY",
		"GOOGLE_API_KEY":
		return true
	}
	for _, suffix := range []string{
		"_API_KEY",
		"_ACCESS_TOKEN",
		"_AUTH_TOKEN",
		"_BEARER_TOKEN",
		"_SECRET",
		"_PASSWORD",
	} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return strings.HasPrefix(name, "CODEFLUX_CREDENTIAL_")
}
