package frontendserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"google.golang.org/grpc"
)

// TestM22_059_NonLoopbackConnectionsAreRefused is M22-059.
//
// docs/plan.md requires the browser server bind loopback by default. A
// request arriving with a non-loopback Host is either a misconfiguration or
// an attempt to reach the agent from another machine; both must be refused
// rather than served.
func TestM22_059_NonLoopbackConnectionsAreRefused(t *testing.T) {
	served := false
	handler := loopbackOnly(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		served = true
	}))

	refused := []string{
		"example.com", "example.com:8080",
		"10.0.0.5:63131", "192.168.1.20:63131",
		"0.0.0.0:63131", "[::ffff:203.0.113.9]:63131",
		"attacker.invalid",
		// Names that merely look like the one reserved name. Accepting any of
		// these would reopen DNS rebinding, which is the whole reason only
		// literal loopback addresses and "localhost" itself are allowed.
		"localhost.attacker.invalid", "notlocalhost:63131", "localhosts:63131",
	}
	for _, host := range refused {
		served = false
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "http://"+host+"/", nil)
		request.Host = host
		handler.ServeHTTP(recorder, request)
		if served {
			t.Fatalf("host %q reached the handler", host)
		}
		if recorder.Code == http.StatusOK {
			t.Fatalf("host %q was served with %d", host, recorder.Code)
		}
	}

	allowed := []string{"127.0.0.1:63131", "localhost:63131", "[::1]:63131"}
	for _, host := range allowed {
		served = false
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "http://"+host+"/", nil)
		request.Host = host
		handler.ServeHTTP(recorder, request)
		if !served {
			t.Fatalf("loopback host %q was refused with %d", host, recorder.Code)
		}
	}
}

// TestM22_060_CrossOriginSessionUseIsRefused is M22-060.
//
// The bridge carries authenticated session traffic, so an origin belonging
// to any other page must not be able to drive it. This exercises the real
// sameOrigin predicate the tunnel is configured with.
func TestM22_060_CrossOriginSessionUseIsRefused(t *testing.T) {
	cases := []struct {
		name   string
		host   string
		origin string
		want   bool
	}{
		{"matching loopback origin", "127.0.0.1:63131", "http://127.0.0.1:63131", true},
		{"attacker origin", "127.0.0.1:63131", "https://attacker.invalid", false},
		{"different loopback port", "127.0.0.1:63131", "http://127.0.0.1:9999", false},
		{"null origin", "127.0.0.1:63131", "null", false},
		{"non-loopback host", "example.com", "http://example.com", false},
		{"scheme upgrade to attacker", "127.0.0.1:63131", "http://127.0.0.1.attacker.invalid", false},
		{"empty origin on loopback", "127.0.0.1:63131", "", false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "http://"+testCase.host+"/bridge", nil)
			request.Host = testCase.host
			if testCase.origin != "" {
				request.Header.Set("Origin", testCase.origin)
			}
			if got := sameOrigin(request); got != testCase.want {
				t.Fatalf("sameOrigin(host=%q, origin=%q) = %v, want %v",
					testCase.host, testCase.origin, got, testCase.want)
			}
		})
	}
}

// TestM22_061_OldSessionSecretIsRefused is M22-061.
//
// The session secret is per-launch, so a cookie left in the browser by an
// earlier run of the process must not authorize this one. That is the whole
// point of regenerating it: a stale tab, a restored session, or a copied
// cookie confers nothing.
func TestM22_061_OldSessionSecretIsRefused(t *testing.T) {
	const currentToken = "current-launch-secret-0000000000000000000000"
	previousLaunchTokens := []string{
		"previous-launch-secret-000000000000000000000",
		// Same length as the current one, differing in a single byte: the
		// comparison must be exact, not a prefix or length check.
		"current-launch-secret-0000000000000000000001",
		// A prefix of the current secret.
		currentToken[:len(currentToken)-1],
		// The current secret with one byte appended.
		currentToken + "0",
	}

	authorize := authorizeCookie(currentToken)
	for _, stale := range previousLaunchTokens {
		request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:63131/bridge", nil)
		request.Host = "127.0.0.1:63131"
		request.AddCookie(&http.Cookie{Name: sessionCookieName, Value: stale})
		if err := authorize(request); err == nil {
			t.Fatalf("a session cookie holding %q was accepted", stale)
		}
	}

	// No cookie at all is refused, and so is a cookie under another name: a
	// tunnel that fell back to "any cookie present" would pass the cases above.
	bare := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:63131/bridge", nil)
	if err := authorize(bare); err == nil {
		t.Fatal("a request with no session cookie was accepted")
	}
	misnamed := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:63131/bridge", nil)
	misnamed.AddCookie(&http.Cookie{Name: "not_the_session_cookie", Value: currentToken})
	if err := authorize(misnamed); err == nil {
		t.Fatal("a correctly-valued cookie under the wrong name was accepted")
	}

	// Not vacuous: the current launch's own cookie works.
	valid := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:63131/bridge", nil)
	valid.AddCookie(&http.Cookie{Name: sessionCookieName, Value: currentToken})
	if err := authorize(valid); err != nil {
		t.Fatalf("the current launch cookie was refused: %v", err)
	}
}

// TestM22_061_SessionCookieCannotLeaveTheOriginOrReachScript pins the cookie
// attributes an old-secret refusal depends on: a cookie readable by script or
// sent cross-site would be copyable in the first place.
func TestM22_061_SessionCookieCannotLeaveTheOriginOrReachScript(t *testing.T) {
	recorder := httptest.NewRecorder()
	setSessionCookie(recorder, "current-launch-secret-0000000000000000000000")

	cookies := recorder.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("expected exactly one session cookie, got %d", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != sessionCookieName {
		t.Fatalf("session cookie name = %q", cookie.Name)
	}
	if !cookie.HttpOnly {
		t.Fatal("the session cookie is readable by page script")
	}
	if cookie.SameSite != http.SameSiteStrictMode {
		t.Fatalf("session cookie SameSite = %v, want Strict", cookie.SameSite)
	}
	// MaxAge 0 with no Expires makes it a session cookie, so it does not
	// outlive the browser session and cannot be replayed against a later
	// launch from disk.
	if cookie.MaxAge != 0 || !cookie.Expires.IsZero() {
		t.Fatalf("the session cookie persists to disk: MaxAge=%d Expires=%v",
			cookie.MaxAge, cookie.Expires)
	}
}

// TestM22_061_ShortSessionTokensAreRejectedAtConstruction proves the handler
// will not start with a guessable secret in the first place.
func TestM22_061_ShortSessionTokensAreRejectedAtConstruction(t *testing.T) {
	for _, token := range []string{"", "short", strings.Repeat("a", 31)} {
		_, err := NewHandler(Options{
			AssetsDirectory: t.TempDir(),
			GRPCServer:      grpc.NewServer(),
			SessionToken:    token,
		})
		if err == nil {
			t.Fatalf("a %d byte session token was accepted", len(token))
		}
		if !strings.Contains(err.Error(), "session token") {
			t.Fatalf("a %d byte token failed for an unrelated reason: %v", len(token), err)
		}
	}
}

// TestM22_059_SecurityHeadersArePresentOnEveryResponse checks the headers
// that make a same-origin claim meaningful in the browser are actually set.
func TestM22_059_SecurityHeadersArePresentOnEveryResponse(t *testing.T) {
	handler := securityHeaders(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
	}), "")

	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:63131/", nil)
	request.Host = "127.0.0.1:63131"
	handler.ServeHTTP(recorder, request)

	required := map[string]string{
		"Cross-Origin-Opener-Policy": "same-origin",
	}
	for header, expected := range required {
		if got := recorder.Header().Get(header); !strings.Contains(got, expected) {
			t.Fatalf("%s = %q, want it to contain %q", header, got, expected)
		}
	}
	if policy := recorder.Header().Get("Content-Security-Policy"); policy == "" {
		t.Fatal("a Content-Security-Policy is required: without it an injected script has no boundary")
	}
}
