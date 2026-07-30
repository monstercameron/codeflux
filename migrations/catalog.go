package migrations

// Descriptor identifies one immutable migration source by sequence, name, and
// content checksum.
type Descriptor struct {
	Number int
	Name   string
	SHA256 string
}

// Source binds one generated descriptor to its immutable embedded SQL.
type Source struct {
	Descriptor Descriptor
	SQL        string
}
