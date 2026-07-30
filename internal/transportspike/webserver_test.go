package transportspike

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplicationHandlerServesGeneratedAssetsAndHistoryFallback(t *testing.T) {
	assets := t.TempDir()
	writeGeneratedAsset(t, assets, "index.html", "framework-generated-shell")
	writeGeneratedAsset(t, assets, "wasm_exec.js", "framework-generated-runtime")
	writeGeneratedAsset(t, assets, filepath.Join("bin", "main.wasm"), "\x00asm")

	handler, _, err := NewApplicationHandler(assets, testLaunchSecret)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(handler)
	defer server.Close()

	for _, path := range []string{"/", "/details", "/wasm_exec.js", "/bin/main.wasm"} {
		response, getErr := http.Get(server.URL + path)
		if getErr != nil {
			t.Fatalf("get %s: %v", path, getErr)
		}
		body, readErr := copyResponseBody(response)
		if readErr != nil {
			t.Fatalf("read %s: %v", path, readErr)
		}
		if len(body) == 0 {
			t.Fatalf("%s returned an empty body", path)
		}
		if response.Header.Get("Content-Security-Policy") == "" {
			t.Fatalf("%s lacks Content-Security-Policy", path)
		}
		if !strings.Contains(response.Header.Get("Content-Security-Policy"), "'wasm-unsafe-eval'") {
			t.Fatalf("%s CSP does not authorize WebAssembly compilation", path)
		}
	}

	response, err := http.Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var found bool
	for _, cookie := range response.Cookies() {
		if cookie.Name == SessionCookieName {
			found = true
			if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
				t.Fatalf("session cookie = %#v", cookie)
			}
		}
	}
	if !found {
		t.Fatal("session cookie not set")
	}
}

func TestApplicationHandlerRejectsMissingAssetsAndUnknownFiles(t *testing.T) {
	assets := t.TempDir()
	if _, _, err := NewApplicationHandler(assets, testLaunchSecret); err == nil {
		t.Fatal("missing generated assets accepted")
	}

	writeGeneratedAsset(t, assets, "index.html", "shell")
	writeGeneratedAsset(t, assets, "wasm_exec.js", "runtime")
	writeGeneratedAsset(t, assets, filepath.Join("bin", "main.wasm"), "\x00asm")
	handler, _, err := NewApplicationHandler(assets, testLaunchSecret)
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1/secret.txt", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatalf("unknown asset status = %d, want 404", response.Code)
	}
}

func TestNewHTTPServerSetsTimeouts(t *testing.T) {
	server := NewHTTPServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	if server.ReadHeaderTimeout == 0 || server.ReadTimeout == 0 || server.IdleTimeout == 0 {
		t.Fatalf("server timeouts = header %s read %s idle %s", server.ReadHeaderTimeout, server.ReadTimeout, server.IdleTimeout)
	}
	_ = server.Shutdown(context.Background())
}

func writeGeneratedAsset(t *testing.T, root string, relative string, contents string) {
	t.Helper()
	path := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func copyResponseBody(response *http.Response) ([]byte, error) {
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode != http.StatusOK {
		return nil, errors.New(response.Status)
	}
	return body, nil
}
