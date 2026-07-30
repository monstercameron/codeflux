package migrations

// Descriptor identifies one immutable migration source by sequence, name, and
// content checksum.
type Descriptor struct {
	Number int
	Name   string
	SHA256 string
}
