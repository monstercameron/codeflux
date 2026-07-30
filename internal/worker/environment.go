package worker

import (
	"errors"
	"strings"
)

// SanitizeEnvironment removes secret values and credential handles from an
// environment. Worker launch applies the narrower minimum allowlist below.
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
		if _, found := explicit[normalized]; found ||
			sensitiveEnvironmentName(normalized) ||
			credentialHandleEnvironmentName(normalized) {
			continue
		}
		sanitized = append(sanitized, entry)
	}
	return sanitized
}

// BuildMinimumWorkerEnvironment admits only platform runtime essentials plus
// coordinator-reviewed names. Secret-like names and credential handles remain
// denied even when explicitly requested.
func BuildMinimumWorkerEnvironment(
	parent []string,
	additionalAllowed []string,
	additionalSensitive []string,
) ([]string, error) {
	allowed := make(map[string]struct{}, len(minimumWorkerEnvironmentNames)+len(additionalAllowed))
	for _, name := range minimumWorkerEnvironmentNames {
		allowed[name] = struct{}{}
	}
	for _, name := range additionalAllowed {
		normalized := strings.ToUpper(strings.TrimSpace(name))
		if !validEnvironmentName(normalized) ||
			sensitiveEnvironmentName(normalized) ||
			credentialHandleEnvironmentName(normalized) {
			return nil, errors.New("additional worker environment name is unsafe")
		}
		allowed[normalized] = struct{}{}
	}
	sensitive := make(map[string]struct{}, len(additionalSensitive))
	for _, name := range additionalSensitive {
		normalized := strings.ToUpper(strings.TrimSpace(name))
		if normalized != "" {
			sensitive[normalized] = struct{}{}
		}
	}
	minimum := make([]string, 0, len(allowed))
	seen := make(map[string]struct{}, len(allowed))
	for _, entry := range parent {
		name, _, found := strings.Cut(entry, "=")
		if !found {
			continue
		}
		normalized := strings.ToUpper(name)
		if _, admitted := allowed[normalized]; !admitted {
			continue
		}
		if _, denied := sensitive[normalized]; denied ||
			sensitiveEnvironmentName(normalized) ||
			credentialHandleEnvironmentName(normalized) {
			continue
		}
		if _, duplicate := seen[normalized]; duplicate {
			continue
		}
		seen[normalized] = struct{}{}
		minimum = append(minimum, entry)
	}
	return minimum, nil
}

var minimumWorkerEnvironmentNames = []string{
	"COMSPEC",
	"LANG",
	"LANGUAGE",
	"LC_ALL",
	"LC_CTYPE",
	"PATH",
	"PATHEXT",
	"SYSTEMROOT",
	"TEMP",
	"TMP",
	"TMPDIR",
	"TZ",
	"WINDIR",
}

func validEnvironmentName(name string) bool {
	if name == "" {
		return false
	}
	for _, character := range name {
		if !((character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') ||
			character == '_') {
			return false
		}
	}
	return true
}

func credentialHandleEnvironmentName(name string) bool {
	switch name {
	case "SSH_AUTH_SOCK",
		"SSH_AGENT_PID",
		"GPG_AGENT_INFO",
		"DOCKER_CONFIG",
		"KUBECONFIG",
		"AWS_CONFIG_FILE",
		"AWS_SHARED_CREDENTIALS_FILE",
		"GOOGLE_APPLICATION_CREDENTIALS",
		"AZURE_CONFIG_DIR",
		"GH_CONFIG_DIR",
		"NETRC":
		return true
	default:
		return false
	}
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
		"_TOKEN",
		"_ACCESS_TOKEN",
		"_AUTH_TOKEN",
		"_BEARER_TOKEN",
		"_CLIENT_SECRET",
		"_PRIVATE_KEY",
		"_SECRET",
		"_PASSWORD",
	} {
		if strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return strings.HasPrefix(name, "CODEFLUX_CREDENTIAL_")
}
