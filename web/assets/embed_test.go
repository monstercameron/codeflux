package assets

import "testing"

func TestEmbeddedAssetsContainBootstrapStyles(t *testing.T) {
	content, err := Files.ReadFile("static/shell.css")
	if err != nil {
		t.Fatalf("read embedded shell styles: %v", err)
	}
	if len(content) == 0 {
		t.Fatal("embedded shell styles are empty")
	}
}
