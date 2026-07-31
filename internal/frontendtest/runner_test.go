package frontendtest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/mxschmitt/playwright-go"
)

func TestValidateBaseURLRequiresExactLoopbackOrigin(t *testing.T) {
	for _, raw := range []string{
		"http://127.0.0.1:8080",
		"http://localhost:9911/",
		"https://[::1]:7443",
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := ValidateBaseURL(raw); err != nil {
				t.Fatalf("ValidateBaseURL(%q): %v", raw, err)
			}
		})
	}
}

func TestValidateBaseURLRejectsHostConfusion(t *testing.T) {
	for _, raw := range []string{
		"https://example.com:443",
		"http://localhost.example.com:8080",
		"http://127.0.0.1.example.com:8080",
		"http://user@localhost:8080",
		"http://localhost",
		"http://localhost:8080/path",
		"ws://localhost:8080",
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := ValidateBaseURL(raw); err == nil {
				t.Fatalf("ValidateBaseURL(%q) succeeded", raw)
			}
		})
	}
}

func TestOriginGuardRejectsExternalSchemesHostsAndPorts(t *testing.T) {
	base, err := url.Parse("http://127.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	guard := newOriginGuard(base)
	for _, allowed := range []string{
		"http://127.0.0.1:8080/",
		"ws://127.0.0.1:8080/bridge",
		"blob:http://127.0.0.1:8080/fixture",
		"data:image/png;base64,AA==",
		"about:blank",
	} {
		if !guard.allowed(allowed) {
			t.Errorf("allowed URL rejected: %s", allowed)
		}
	}
	for _, rejected := range []string{
		"http://127.0.0.1:8081/",
		"http://localhost:8080/",
		"https://127.0.0.1:8080/",
		"wss://127.0.0.1:8080/bridge",
		"blob:https://example.com/id",
		"file:///tmp/leak",
		"https://example.com/",
	} {
		if guard.allowed(rejected) {
			t.Errorf("external URL accepted: %s", rejected)
		}
	}
}

func TestAllPassedRequiresAtLeastOnePassingCheck(t *testing.T) {
	if allPassed(nil) {
		t.Fatal("empty check set passed")
	}
	if !allPassed([]CheckResult{{Passed: true}}) {
		t.Fatal("passing check set failed")
	}
	if allPassed([]CheckResult{{Passed: true}, {Passed: false}}) {
		t.Fatal("failing check set passed")
	}
}

func TestNetworkGateDoesNotClaimEvidenceWithoutApplicationLoad(t *testing.T) {
	if detail := networkGateDetail(false, nil); detail == "" {
		t.Fatal("unevaluated network gate has no explanation")
	}
	if detail := networkGateDetail(true, nil); detail != "" {
		t.Fatalf("successful network gate detail = %q", detail)
	}
}

func TestBrowserDiagnosticsReturnsACopy(t *testing.T) {
	diagnostics := &browserDiagnostics{}
	diagnostics.append("first")
	messages := diagnostics.errors()
	messages[0] = "mutated"
	if got := diagnostics.errors(); len(got) != 1 || got[0] != "first" {
		t.Fatalf("diagnostics = %#v", got)
	}
}

func TestBroadWebSocketCSPDetectionIsDirectiveScoped(t *testing.T) {
	if !hasBroadWebSocketSource("default-src 'self'; connect-src 'self' ws: wss:") {
		t.Fatal("broad websocket sources were accepted")
	}
	if hasBroadWebSocketSource("default-src 'self'; connect-src 'self'; img-src data:") {
		t.Fatal("same-origin-only connect-src was rejected")
	}
	if hasBroadWebSocketSource("default-src 'self'; report-uri /contains-ws:-text") {
		t.Fatal("unrelated directive triggered websocket detection")
	}
}

func TestExactWebSocketCSPRequiresOneLaunchOrigin(t *testing.T) {
	base, err := url.Parse("http://127.0.0.1:8080")
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name string
		csp  string
		want bool
	}{
		{name: "exact", csp: "default-src 'self'; connect-src 'self' ws://127.0.0.1:8080", want: true},
		{name: "missing", csp: "default-src 'self'; connect-src 'self'", want: false},
		{name: "wrong port", csp: "connect-src 'self' ws://127.0.0.1:8081", want: false},
		{name: "extra websocket", csp: "connect-src 'self' ws://127.0.0.1:8080 wss://example.com", want: false},
		{name: "scheme wildcard", csp: "connect-src 'self' ws:", want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, _ := hasExactWebSocketSource(test.csp, base)
			if got != test.want {
				t.Fatalf("hasExactWebSocketSource(%q) = %t, want %t", test.csp, got, test.want)
			}
		})
	}
}

func TestLoopbackHostProbeUsesForgedHostAndOrigin(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Host != "attacker.invalid" || request.Header.Get("Origin") != "http://attacker.invalid" {
			response.WriteHeader(http.StatusOK)
			return
		}
		response.WriteHeader(http.StatusMisdirectedRequest)
	}))
	t.Cleanup(server.Close)
	base, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	result := Result{}
	appendLoopbackHostProtectionCheck(context.Background(), base, time.Second, &result)
	if len(result.Checks) != 1 || !result.Checks[0].Passed {
		t.Fatalf("host probe checks = %#v", result.Checks)
	}
}

func TestThemeCycleReturnsToInitialTheme(t *testing.T) {
	for _, initial := range []string{"dark", "light", "high-contrast"} {
		current := initial
		for step := 0; step < 3; step++ {
			next, ok := nextTheme(current)
			if !ok {
				t.Fatalf("nextTheme(%q) is unsupported", current)
			}
			current = next
		}
		if current != initial {
			t.Fatalf("three steps from %q ended at %q", initial, current)
		}
	}
	if _, ok := nextTheme("unknown"); ok {
		t.Fatal("unknown theme was accepted")
	}
}

func TestInteractionThreadRouteUsesDeclaredThreadContract(t *testing.T) {
	route := interactionTaskRoute()
	if route != "/tasks" && !strings.Contains(route, "/thread/") {
		t.Fatalf("interaction thread route = %q", route)
	}
}

func TestExpandedFocusClipIncludesRingAndStaysInsideViewport(t *testing.T) {
	clip, err := expandedFocusClip(
		playwright.Rect{X: 2, Y: 3, Width: 40, Height: 44},
		100,
		80,
	)
	if err != nil {
		t.Fatal(err)
	}
	if clip.X != 0 || clip.Y != 0 || clip.Width != 48 || clip.Height != 53 {
		t.Fatalf("clip = %+v", clip)
	}
	if _, err := expandedFocusClip(
		playwright.Rect{X: 120, Y: 90, Width: 10, Height: 10},
		100,
		80,
	); err == nil {
		t.Fatal("outside focus clip succeeded")
	}
}
