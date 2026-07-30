// Package assets describes deterministic packaging for browser artifacts
// generated from the GoWebComponents client.
package assets

// Descriptor identifies one packaged GoWebComponents-generated browser asset
// by path and content checksum.
type Descriptor struct {
	Path   string
	SHA256 string
}
