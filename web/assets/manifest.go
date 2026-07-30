package assets

// Descriptor identifies one embedded frontend source asset by path and content
// checksum.
type Descriptor struct {
	Path   string
	SHA256 string
}
