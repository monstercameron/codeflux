package assets

import "testing"

func TestManifestContainsNoPreSpikeBrowserAssets(t *testing.T) {
	if len(Manifest) != 0 {
		t.Fatalf("generated manifest contains pre-spike browser assets: %#v", Manifest)
	}
}
