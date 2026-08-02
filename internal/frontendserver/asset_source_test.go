package frontendserver

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"codeflux.dev/codeflux/internal/domain"
	"codeflux.dev/codeflux/web/frontend/routes"
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

// TestTheServerServesExactlyWhatTheClientRoutes binds the two decisions.
//
// They were two hand-maintained lists and they disagreed: this server allowed
// /tasks and /memory, which the client does not route, and refused /graphs,
// which it does. Clicking Graphs in the navigation rail produced a server 404.
func TestTheServerServesExactlyWhatTheClientRoutes(t *testing.T) {
	handler, err := NewHandler(assetHandlerOptions(completeAssetSource(), ""))
	if err != nil {
		t.Fatalf("build handler: %v", err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	repository, err := domain.NewRepositoryID()
	if err != nil {
		t.Fatal(err)
	}
	thread, err := domain.NewThreadID()
	if err != nil {
		t.Fatal(err)
	}

	served := []string{
		"/",
		"/settings",
		"/graphs",
		"/diagnostics",
		"/first-run",
		"/workspace/" + repository.String() + "/memory",
		"/workspace/" + repository.String() + "/thread/" + thread.String(),
	}
	for _, path := range served {
		t.Run("serves "+path, func(t *testing.T) {
			response, err := server.Client().Get(server.URL + path)
			if err != nil {
				t.Fatalf("GET %s: %v", path, err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("GET %s = %d; the client routes this path but the server refused it",
					path, response.StatusCode)
			}
		})
	}

	// The client's router registers "/tasks" and "/memory" as entry points and
	// resolves each into a scoped route once a repository is open, so both are
	// served: refusing them answered a reload or a deep link with a plain-text
	// 404 and the interface never loaded at all.
	for _, path := range []string{"/tasks", "/memory"} {
		t.Run("serves entry path "+path, func(t *testing.T) {
			response, err := server.Client().Get(server.URL + path)
			if err != nil {
				t.Fatalf("GET %s: %v", path, err)
			}
			defer response.Body.Close()
			if response.StatusCode != http.StatusOK {
				t.Fatalf("GET %s = %d; the client routes this entry path", path, response.StatusCode)
			}
		})
	}

	// A path the client cannot route must not receive the document either:
	// serving it produces a page that mounts and then renders not-found,
	// which is a worse answer than the honest one.
	for _, path := range []string{"/workspace", "/nope"} {
		t.Run("refuses "+path, func(t *testing.T) {
			response, err := server.Client().Get(server.URL + path)
			if err != nil {
				t.Fatalf("GET %s: %v", path, err)
			}
			defer response.Body.Close()
			if response.StatusCode == http.StatusOK {
				t.Fatalf("GET %s = 200; the client cannot route this path", path)
			}
		})
	}

	// Every path the route table can produce must be served. This is the
	// binding that keeps the two from drifting again.
	for _, route := range []routes.Route{
		{Name: routes.RepositoryChooser},
		{Name: routes.Settings},
		{Name: routes.Graphs},
		{Name: routes.Diagnostics},
		{Name: routes.FirstRun},
		{Name: routes.Memory, RepositoryID: repository},
		{Name: routes.ThreadWorkspace, RepositoryID: repository, ThreadID: thread},
	} {
		path, err := routes.Path(route)
		if err != nil {
			t.Fatalf("route %s has no path: %v", route.Name, err)
		}
		if !isApplicationRoute(path) {
			t.Errorf("the route table produces %q, which this server refuses", path)
		}
	}
}
