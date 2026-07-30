package assets

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestEmbeddedAssetsContainBootstrapStyles(t *testing.T) {
	content, err := Files.ReadFile("static/shell.css")
	if err != nil {
		t.Fatalf("read embedded shell styles: %v", err)
	}
	if len(content) == 0 {
		t.Fatal("embedded shell styles are empty")
	}
	sum := sha256.Sum256(content)
	if len(Manifest) != 1 || Manifest[0].Path != "static/shell.css" ||
		Manifest[0].SHA256 != hex.EncodeToString(sum[:]) {
		t.Fatalf("generated manifest does not identify embedded asset: %#v", Manifest)
	}
}
