package frontendserver

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestAUDIT021_ThePolicyForbidsEveryExternalOrigin covers the enforceable half
// of AUDIT-021 (M16-G01).
//
// M16-G01 claims the embedded shell, WASM client, and bridge load with zero
// external requests. Observing that a page made none is weaker than the page
// being unable to: an asset added later could reach out and no existing test
// would notice. The Content-Security-Policy is what makes the property hold
// rather than merely happen to be true, so it is asserted directly.
func TestAUDIT021_ThePolicyForbidsEveryExternalOrigin(t *testing.T) {
	terminal := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})
	handler := securityHeaders(terminal, "")

	for _, testCase := range []struct {
		name            string
		host            string
		wantWebsocket   string
		forbidWebsocket bool
	}{
		{name: "a loopback host may reach its own bridge",
			host: "127.0.0.1:47861", wantWebsocket: "ws://127.0.0.1:47861"},
		{name: "a non-loopback host gets no websocket source",
			host: "codeflux.example.com", forbidWebsocket: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/", nil)
			request.Host = testCase.host
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)

			policy := recorder.Header().Get("Content-Security-Policy")
			if policy == "" {
				t.Fatal("no Content-Security-Policy was set")
			}
			for directive, expected := range map[string]string{
				"default-src":     "'self'",
				"base-uri":        "'none'",
				"form-action":     "'self'",
				"frame-ancestors": "'none'",
				"object-src":      "'none'",
			} {
				if !strings.Contains(policy, directive+" "+expected) {
					t.Errorf("the policy does not set %s to %s, so an asset could "+
						"reach outside the executable", directive, expected)
				}
			}
			if testCase.forbidWebsocket {
				if strings.Contains(policy, "ws://") || strings.Contains(policy, "wss://") {
					t.Errorf("a non-loopback host was granted a websocket source: %s", policy)
				}
				return
			}
			if !strings.Contains(policy, testCase.wantWebsocket) {
				t.Errorf("the loopback bridge is not permitted: %s", policy)
			}
		})
	}
}

// TestAUDIT021_TheServedShellNamesNoExternalHost checks the asset the server
// actually produces.
//
// The policy above is the enforcement; this is the belt to its braces, and it
// catches the case where a shell is generated with an absolute URL that the
// policy would then block at runtime — a broken page rather than a leaking
// one, but still broken.
func TestAUDIT021_TheServedShellNamesNoExternalHost(t *testing.T) {
	root := repositoryRootForFrontendTest(t)
	shell := filepath.Join(root, ".artifacts", "frontend", "index.html")
	content, err := os.ReadFile(shell)
	if err != nil {
		t.Skipf("no built shell to inspect; run `codeflux-dev build-frontend` first: %v", err)
	}

	absolute := regexp.MustCompile(`(?:src|href)\s*=\s*"(https?://[^"]+)"`)
	for _, match := range absolute.FindAllStringSubmatch(string(content), -1) {
		t.Errorf("the shell loads %s from outside the executable", match[1])
	}
}

// TestAUDIT021_TheEmbedContractNamesEveryRequiredAsset keeps the required set
// and the staged set from drifting apart.
//
// A release that embedded two of the three files would start, serve a page,
// and fail at the first import.
func TestAUDIT021_TheEmbedContractNamesEveryRequiredAsset(t *testing.T) {
	root := repositoryRootForFrontendTest(t)
	embed, err := os.ReadFile(filepath.Join(root, "web", "assets", "embed.go"))
	if err != nil {
		t.Fatalf("read the embed contract: %v", err)
	}
	body := string(embed)
	for _, required := range []string{"index.html", "wasm_exec.js", "bin/main.wasm"} {
		if !strings.Contains(body, required) {
			t.Errorf("the embed contract does not name %s as required", required)
		}
	}
	if !strings.Contains(body, "go:embed all:static") {
		t.Error("the embed directive no longer pulls in the whole asset tree")
	}
}

func repositoryRootForFrontendTest(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("repository root %q has no go.mod: %v", root, err)
	}
	return root
}
