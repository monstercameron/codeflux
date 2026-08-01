package frontendserver

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc"
)

// mapAssets is an in-memory AssetSource, standing in for the assets a released
// executable carries inside itself.
type mapAssets map[string][]byte

func (assets mapAssets) Get(relative string) ([]byte, error) {
	content, present := assets[relative]
	if !present {
		return nil, fmt.Errorf("no such asset: %s", relative)
	}
	return content, nil
}

func completeAssetSource() mapAssets {
	return mapAssets{
		"index.html": []byte(
			`<!DOCTYPE html><html><head><script src="./wasm_exec.js"></script></head>` +
				`<body><script>fetch("./bin/main.wasm")</script></body></html>`),
		"wasm_exec.js":  []byte("// runtime shim"),
		"bin/main.wasm": []byte("\x00asm\x01\x00\x00\x00"),
	}
}

func assetHandlerOptions(source AssetSource, directory string) Options {
	return Options{
		Assets:          source,
		AssetsDirectory: directory,
		GRPCServer:      grpc.NewServer(),
		SessionToken:    strings.Repeat("s", 48),
	}
}

func TestResolvedAssetsAreServedWithoutADirectory(t *testing.T) {
	// This is what lets a released executable serve the interface with nothing
	// beside it on disk.
	handler, err := NewHandler(assetHandlerOptions(completeAssetSource(), ""))
	if err != nil {
		t.Fatalf("resolved assets were refused: %v", err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	for path, wantType := range map[string]string{
		"/":              "text/html",
		"/wasm_exec.js":  "text/javascript",
		"/bin/main.wasm": "application/wasm",
	} {
		t.Run(path, func(t *testing.T) {
			response, err := server.Client().Get(server.URL + path)
			if err != nil {
				t.Fatalf("GET %s: %v", path, err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200", path, response.StatusCode)
			}
			if got := response.Header.Get("Content-Type"); !strings.HasPrefix(got, wantType) {
				t.Errorf("GET %s content type = %q, want %q", path, got, wantType)
			}
		})
	}
}

func TestSupplyingAssetsTwoWaysIsRefused(t *testing.T) {
	// Accepting both would leave the precedence between an embedded release
	// build and somebody's working tree to be discovered at runtime, which is
	// how stale files ship.
	_, err := NewHandler(assetHandlerOptions(completeAssetSource(), "/tmp/assets"))
	if err == nil {
		t.Fatal("assets supplied both resolved and as a directory were accepted")
	}
	if !strings.Contains(err.Error(), "precedence") {
		t.Errorf("the refusal does not say why it matters: %v", err)
	}
}

func TestNoAssetsAtAllIsRefused(t *testing.T) {
	_, err := NewHandler(assetHandlerOptions(nil, ""))
	if err == nil {
		t.Fatal("a handler with no assets was built")
	}
	if !strings.Contains(err.Error(), "assets are required") {
		t.Errorf("the refusal is unclear: %v", err)
	}
}

func TestARelativeAssetsDirectoryIsRefused(t *testing.T) {
	// A relative path resolves against whatever the process's working
	// directory happens to be, which is not something a server should depend
	// on.
	if _, err := NewHandler(assetHandlerOptions(nil, "relative/assets")); err == nil {
		t.Fatal("a relative assets directory was accepted")
	}
}

func TestAnIncompleteAssetSetIsRefusedByName(t *testing.T) {
	for _, missing := range []string{"index.html", "wasm_exec.js", "bin/main.wasm"} {
		t.Run(missing, func(t *testing.T) {
			source := completeAssetSource()
			delete(source, missing)
			_, err := NewHandler(assetHandlerOptions(source, ""))
			if err == nil {
				t.Fatalf("an asset set missing %s was accepted", missing)
			}
			if !strings.Contains(err.Error(), missing) {
				t.Errorf("the error does not name the missing asset: %v", err)
			}
		})
	}
}

func TestResolvedAssetsGetTheSameIndexNormalization(t *testing.T) {
	// GWC scaffolds relative runtime URLs because its own dev server mounts at
	// root. CodeFlux serves the same document from nested routes, so a
	// resolved set must be rewritten exactly like a directory-read one, or
	// deep links break only in release builds.
	handler, err := NewHandler(assetHandlerOptions(completeAssetSource(), ""))
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	response, err := server.Client().Get(server.URL + "/")
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	defer response.Body.Close()
	body := make([]byte, 4096)
	count, _ := response.Body.Read(body)
	document := string(body[:count])
	if strings.Contains(document, `"./bin/main.wasm"`) ||
		strings.Contains(document, `src="./wasm_exec.js"`) {
		t.Errorf("relative runtime references survived normalization:\n%s", document)
	}
	if !strings.Contains(document, "/bin/main.wasm") ||
		!strings.Contains(document, "/wasm_exec.js") {
		t.Errorf("the normalized document lost its runtime references:\n%s", document)
	}
}

func TestAFailingAssetSourceIsReportedNotIgnored(t *testing.T) {
	failing := failingAssets{err: errors.New("disk went away")}
	_, err := NewHandler(assetHandlerOptions(failing, ""))
	if err == nil {
		t.Fatal("a failing asset source produced a working handler")
	}
	if !strings.Contains(err.Error(), "disk went away") {
		t.Errorf("the underlying failure was swallowed: %v", err)
	}
}

type failingAssets struct{ err error }

func (assets failingAssets) Get(string) ([]byte, error) { return nil, assets.err }
