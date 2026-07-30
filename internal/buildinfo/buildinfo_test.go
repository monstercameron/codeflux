package buildinfo

import (
	"strings"
	"testing"
)

func TestCurrentIsCompleteAndHonest(t *testing.T) {
	info := Current()
	for name, value := range map[string]string{
		"version":          info.Version,
		"commit":           info.Commit,
		"build date":       info.BuildDate,
		"Go version":       info.GoVersion,
		"frontend version": info.FrontendVersion,
	} {
		if strings.TrimSpace(value) == "" {
			t.Errorf("%s is empty", name)
		}
	}
	if !strings.HasPrefix(info.GoVersion, "go1.") {
		t.Errorf("GoVersion = %q, want Go runtime version", info.GoVersion)
	}
}
