package frontendserver

import (
	"codeflux.dev/codeflux/internal/domain"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"google.golang.org/grpc"
)

const testSessionToken = "0123456789abcdef0123456789abcdef"

func TestHandlerServesGeneratedShellAndSecretFreeBootstrap(t *testing.T) {
	assets := writeGeneratedAssets(t)
	handler, err := NewHandler(Options{
		AssetsDirectory: assets,
		GRPCServer:      grpc.NewServer(),
		SessionToken:    testSessionToken,
		Bootstrap: Bootstrap{
			ApplicationVersion: "0.16.0",
			APIVersion:         "v1",
			SchemaVersion:      15,
			FrontendVersion:    "m16",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	// These are the paths the client actually routes. /tasks, /memory, and a
	// bare /workspace were in this list and are not routes: the server used to
	// serve a document for them, which produced a page that mounted and then
	// rendered not-found.
	repository, err := domain.NewRepositoryID()
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		"/", "/graphs", "/settings", "/diagnostics", "/first-run",
		"/workspace/" + repository.String() + "/memory",
	} {
		request := httptest.NewRequest(http.MethodGet, path, nil)
		request.Host = "127.0.0.1:8080"
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK || response.Body.String() != "<generated-shell>" {
			t.Fatalf("%s returned %d %q", path, response.Code, response.Body.String())
		}
		cookies := response.Result().Cookies()
		if len(cookies) != 1 || !cookies[0].HttpOnly ||
			cookies[0].SameSite != http.SameSiteStrictMode {
			t.Fatalf("%s session cookies = %#v", path, cookies)
		}
		if csp := response.Header().Get("Content-Security-Policy"); !strings.Contains(csp, "default-src 'self'") {
			t.Fatalf("%s CSP = %q", path, csp)
		}
	}

	request := httptest.NewRequest(http.MethodGet, "/bootstrap", nil)
	request.Host = "127.0.0.1:8080"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK ||
		strings.Contains(response.Body.String(), testSessionToken) ||
		!strings.Contains(response.Body.String(), `"bridge_path":"/grpc"`) {
		t.Fatalf("bootstrap returned %d %q", response.Code, response.Body.String())
	}
}

func TestBootstrapIncludesOnlyAnExplicitValidSelectedSession(t *testing.T) {
	selected := &codefluxv1.StableIdentity{
		Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_SESSION,
		Value: "ses_018f0123-4567-789a-8bcd-ef0123456789",
	}
	handler, err := NewHandler(Options{
		AssetsDirectory: writeGeneratedAssets(t),
		GRPCServer:      grpc.NewServer(),
		SessionToken:    testSessionToken,
		Bootstrap:       Bootstrap{SelectedSessionID: selected},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/bootstrap", nil)
	request.Host = "127.0.0.1:8080"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), `"selected_session_id"`) ||
		!strings.Contains(response.Body.String(), selected.GetValue()) ||
		strings.Contains(response.Body.String(), testSessionToken) {
		t.Fatalf("bootstrap returned %d %q", response.Code, response.Body.String())
	}

	_, err = NewHandler(Options{
		AssetsDirectory: writeGeneratedAssets(t),
		GRPCServer:      grpc.NewServer(),
		SessionToken:    testSessionToken,
		Bootstrap: Bootstrap{SelectedSessionID: &codefluxv1.StableIdentity{
			Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK,
			Value: "tsk-not-a-session",
		}},
	})
	if err == nil {
		t.Fatal("NewHandler accepted a non-session selected identity")
	}
}

func TestBootstrapIncludesOnlyAnExplicitValidSelectedWorkspace(t *testing.T) {
	selected := &codefluxv1.StableIdentity{
		Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_WORKSPACE,
		Value: "wsp_018f0123-4567-789a-8bcd-ef0123456789",
	}
	handler, err := NewHandler(Options{
		AssetsDirectory: writeGeneratedAssets(t),
		GRPCServer:      grpc.NewServer(),
		SessionToken:    testSessionToken,
		Bootstrap:       Bootstrap{SelectedWorkspaceID: selected},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/bootstrap", nil)
	request.Host = "127.0.0.1:8080"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK ||
		!strings.Contains(response.Body.String(), `"selected_workspace_id"`) ||
		!strings.Contains(response.Body.String(), selected.GetValue()) ||
		strings.Contains(response.Body.String(), testSessionToken) {
		t.Fatalf("bootstrap returned %d %q", response.Code, response.Body.String())
	}

	_, err = NewHandler(Options{
		AssetsDirectory: writeGeneratedAssets(t),
		GRPCServer:      grpc.NewServer(),
		SessionToken:    testSessionToken,
		Bootstrap: Bootstrap{SelectedWorkspaceID: &codefluxv1.StableIdentity{
			Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_REPOSITORY,
			Value: "repo-not-a-workspace",
		}},
	})
	if err == nil {
		t.Fatal("NewHandler accepted a non-workspace selected identity")
	}
}

func TestBootstrapRouteAccessContainsOnlyValidatedStableIdentities(t *testing.T) {
	repository := &codefluxv1.StableIdentity{
		Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_REPOSITORY,
		Value: "repo_018f0123-4567-789a-8bcd-ef0123456789",
	}
	thread := &codefluxv1.StableIdentity{
		Kind:  codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD,
		Value: "thr_018f0123-4567-789a-8bcd-ef0123456789",
	}
	handler, err := NewHandler(Options{
		AssetsDirectory: writeGeneratedAssets(t),
		GRPCServer:      grpc.NewServer(),
		SessionToken:    testSessionToken,
		Bootstrap: Bootstrap{RouteAccess: RouteAccess{
			FirstRunComplete:       true,
			AccessibleRepositories: []*codefluxv1.StableIdentity{repository},
			AccessibleThreads:      []*codefluxv1.StableIdentity{thread},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/bootstrap", nil)
	request.Host = "127.0.0.1:8080"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	body := response.Body.String()
	if response.Code != http.StatusOK ||
		!strings.Contains(body, `"first_run_complete":true`) ||
		!strings.Contains(body, repository.GetValue()) ||
		!strings.Contains(body, thread.GetValue()) ||
		strings.Contains(body, testSessionToken) {
		t.Fatalf("bootstrap route access = %d %q", response.Code, body)
	}

	_, err = NewHandler(Options{
		AssetsDirectory: writeGeneratedAssets(t),
		GRPCServer:      grpc.NewServer(),
		SessionToken:    testSessionToken,
		Bootstrap: Bootstrap{RouteAccess: RouteAccess{
			AccessibleRepositories: []*codefluxv1.StableIdentity{thread},
		}},
	})
	if err == nil {
		t.Fatal("NewHandler accepted a thread identity as repository access")
	}
}

func TestInlineLoaderIsAllowedOnlyByExactContentHash(t *testing.T) {
	index := []byte("<script src=\"/wasm_exec.js\"></script><script>boot()</script>")
	sources := inlineScriptSources(index)
	if strings.Count(sources, "'sha256-") != 1 ||
		sources != "'sha256-MeZS89WlF0u+o0hCvHTBt4q1WHU+U+sJKgbdRUc36mY='" {
		t.Fatalf("inline sources = %q", sources)
	}
	changed := inlineScriptSources([]byte("<script>other()</script>"))
	if changed == sources {
		t.Fatal("changed inline loader retained the old source hash")
	}
}

func TestHandlerRejectsUnknownRoutesAndUnauthenticatedBridge(t *testing.T) {
	handler, err := NewHandler(Options{
		AssetsDirectory: writeGeneratedAssets(t),
		GRPCServer:      grpc.NewServer(),
		SessionToken:    testSessionToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	unknown := httptest.NewRecorder()
	unknownRequest := httptest.NewRequest(http.MethodGet, "/private.txt", nil)
	unknownRequest.Host = "127.0.0.1:8080"
	handler.ServeHTTP(unknown, unknownRequest)
	if unknown.Code != http.StatusNotFound {
		t.Fatalf("unknown route status = %d", unknown.Code)
	}

	bridgeRequest := httptest.NewRequest(http.MethodGet, "/grpc", nil)
	bridgeRequest.Host = "127.0.0.1:8080"
	bridgeRequest.Header.Set("Origin", "http://127.0.0.1:8080")
	bridge := httptest.NewRecorder()
	handler.ServeHTTP(bridge, bridgeRequest)
	if bridge.Code != http.StatusForbidden {
		t.Fatalf("unauthenticated bridge status = %d", bridge.Code)
	}
}

func TestBridgeRequestIdentityIsServerGenerated(t *testing.T) {
	seen := ""
	handler := withBridgeRequestIdentity(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		seen = request.Header.Get("X-Request-Id")
		writer.WriteHeader(http.StatusNoContent)
	}))
	request := httptest.NewRequest(http.MethodGet, "/grpc", nil)
	request.Header.Set("X-Request-Id", "client-controlled")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent {
		t.Fatalf("bridge identity response = %d", response.Code)
	}
	if !strings.HasPrefix(seen, "bridge-") || len(seen) != len("bridge-")+32 ||
		seen == "client-controlled" {
		t.Fatalf("server bridge request identity = %q", seen)
	}
}

func TestSameOriginIsExact(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:8080/grpc", nil)
	request.Host = "127.0.0.1:8080"
	request.Header.Set("Origin", "http://127.0.0.1:8080")
	if !sameOrigin(request) {
		t.Fatal("exact local origin was rejected")
	}
	request.Header.Set("Origin", "http://evil.example")
	if sameOrigin(request) {
		t.Fatal("cross origin request was accepted")
	}
	request.Host = "codeflux.attacker.example:8080"
	request.Header.Set("Origin", "http://codeflux.attacker.example:8080")
	if sameOrigin(request) {
		t.Fatal("matching non-loopback origin was accepted")
	}
}

func TestGeneratedIndexUsesRootRelativeRuntimeAssets(t *testing.T) {
	for _, test := range []struct {
		name string
		html string
		want string
	}{
		{
			name: "double quoted fetch",
			html: `<script src="./wasm_exec.js"></script><script>fetch("./bin/main.wasm")</script>`,
			want: `fetch("/bin/main.wasm")`,
		},
		{
			name: "single quoted fetch",
			html: `<script src="./wasm_exec.js"></script><script>fetch('./bin/main.wasm')</script>`,
			want: `fetch('/bin/main.wasm')`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			index := normalizeGeneratedIndex([]byte(test.html))
			if !strings.Contains(string(index), `src="/wasm_exec.js"`) ||
				!strings.Contains(string(index), test.want) {
				t.Fatalf("normalized index = %q", index)
			}
		})
	}
}

func TestHandlerRejectsNonLoopbackHostBeforeMintingSession(t *testing.T) {
	handler, err := NewHandler(Options{
		AssetsDirectory: writeGeneratedAssets(t),
		GRPCServer:      grpc.NewServer(),
		SessionToken:    testSessionToken,
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/bootstrap", nil)
	request.Host = "codeflux.attacker.example:8080"
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusMisdirectedRequest ||
		len(response.Result().Cookies()) != 0 {
		t.Fatalf("non-loopback response = %d cookies=%#v", response.Code, response.Result().Cookies())
	}
}

func writeGeneratedAssets(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	for relative, content := range map[string]string{
		"index.html":    "<generated-shell>",
		"wasm_exec.js":  "generated shim",
		"bin/main.wasm": "generated wasm",
	} {
		if err := os.WriteFile(filepath.Join(root, relative), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}
