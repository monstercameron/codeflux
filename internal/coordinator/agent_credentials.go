package coordinator

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// ProviderKeyNames are the environment names a coordinator will read a key from.
//
// The first one found wins, and the order is the conventional one: an explicit
// OPENAI_API_KEY beats the shorter alias, because a machine with both set has
// almost certainly set the specific one deliberately.
var ProviderKeyNames = []string{"OPENAI_API_KEY", "OPENAI_KEY"}

// ReadProviderKey finds an API key in the environment or in a .env file.
//
// The file is read because that is where people actually keep this, and a
// coordinator that only read the environment would appear to have no key on a
// machine that plainly has one. Nothing here logs the value, returns it in an
// error, or writes it anywhere.
func ReadProviderKey(directory string) string {
	for _, name := range ProviderKeyNames {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" {
			return value
		}
	}
	file, err := os.Open(filepath.Join(directory, ".env"))
	if err != nil {
		return ""
	}
	defer func() { _ = file.Close() }()

	values := map[string]string{}
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, value, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		values[strings.TrimSpace(name)] = strings.Trim(strings.TrimSpace(value), `"'`)
	}
	for _, name := range ProviderKeyNames {
		if value := values[name]; value != "" {
			return value
		}
	}
	return ""
}
