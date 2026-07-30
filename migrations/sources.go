package migrations

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// Sources returns the ordered embedded migrations after verifying every
// generated name and checksum.
func Sources() ([]Source, error) {
	sources := make([]Source, 0, len(Catalog))
	for index, descriptor := range Catalog {
		if descriptor.Number != index {
			return nil, fmt.Errorf(
				"migration catalog position %d has number %d",
				index,
				descriptor.Number,
			)
		}
		content, err := Files.ReadFile(descriptor.Name)
		if err != nil {
			return nil, fmt.Errorf("read embedded migration %s: %w", descriptor.Name, err)
		}
		sum := sha256.Sum256(content)
		checksum := hex.EncodeToString(sum[:])
		if checksum != descriptor.SHA256 {
			return nil, fmt.Errorf(
				"embedded migration %s checksum is %s, want %s",
				descriptor.Name,
				checksum,
				descriptor.SHA256,
			)
		}
		sources = append(sources, Source{Descriptor: descriptor, SQL: string(content)})
	}
	return sources, nil
}

// LatestVersion returns the greatest migration number or -1 for no migrations.
func LatestVersion() int {
	return len(Catalog) - 1
}
