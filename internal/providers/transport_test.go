package providers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestValidateProviderEndpointRequiresSecureRemoteApproval(t *testing.T) {
	for _, test := range []struct {
		name     string
		endpoint string
		policy   EndpointPolicy
		wantErr  error
	}{
		{
			name:     "loopback HTTP",
			endpoint: "http://127.0.0.1:8080/v1",
		},
		{
			name:     "remote approval required",
			endpoint: "https://models.example.test/v1",
			wantErr:  ErrEndpointApprovalRequired,
		},
		{
			name:     "approved remote HTTPS",
			endpoint: "https://models.example.test/v1",
			policy:   EndpointPolicy{AllowRemote: true},
		},
		{
			name:     "remote HTTP remains forbidden",
			endpoint: "http://models.example.test/v1",
			policy:   EndpointPolicy{AllowRemote: true},
			wantErr:  ErrInsecureRemoteEndpoint,
		},
		{
			name:     "hostname is not assumed loopback",
			endpoint: "http://localhost:8080/v1",
			wantErr:  ErrInsecureRemoteEndpoint,
		},
		{
			name:     "embedded credentials forbidden",
			endpoint: "https://token@models.example.test/v1",
			policy:   EndpointPolicy{AllowRemote: true},
			wantErr:  errors.New("sentinel"),
		},
		{
			name:     "query forbidden",
			endpoint: "https://models.example.test/v1?key=secret",
			policy:   EndpointPolicy{AllowRemote: true},
			wantErr:  errors.New("sentinel"),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			endpoint, err := ValidateProviderEndpoint(test.endpoint, test.policy)
			if test.wantErr == nil {
				if err != nil {
					t.Fatal(err)
				}
				if endpoint.String() != test.endpoint {
					t.Fatalf("endpoint = %q", endpoint)
				}
				return
			}
			if err == nil {
				t.Fatal("unsafe endpoint was accepted")
			}
			if test.wantErr != nil &&
				test.wantErr.Error() != "sentinel" &&
				!errors.Is(err, test.wantErr) {
				t.Fatalf("endpoint error = %v, want %v", err, test.wantErr)
			}
		})
	}
}

func TestHTTPTransportStrictJSONAndRedirectBoundary(t *testing.T) {
	var targetURL string
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		switch request.URL.Path {
		case "/redirect":
			http.Redirect(writer, request, targetURL, http.StatusTemporaryRedirect)
		case "/trailing":
			_, _ = writer.Write([]byte(`{"value":"first"} {"value":"second"}`))
		default:
			writer.Header().Set("X-Request-Id", "synthetic-request-id")
			_ = json.NewEncoder(writer).Encode(struct {
				Value string `json:"value"`
			}{Value: "accepted"})
		}
	}))
	defer server.Close()
	targetURL = server.URL + "/accepted"
	transport, err := NewHTTPTransport(TransportOptions{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	var response struct {
		Value string `json:"value"`
	}
	result, err := transport.DoJSON(t.Context(), HTTPRequest{
		Method: http.MethodPost, Endpoint: server.URL + "/accepted",
		Body: []byte(`{}`),
	}, &response)
	if err != nil {
		t.Fatal(err)
	}
	if response.Value != "accepted" || result.StatusCode != http.StatusOK ||
		result.Latency() < 0 ||
		result.ResponseHeader("X-Request-Id") != "synthetic-request-id" {
		t.Fatalf("response=%#v result=%#v", response, result)
	}
	_, err = transport.DoJSON(t.Context(), HTTPRequest{
		Method: http.MethodPost, Endpoint: server.URL + "/trailing",
	}, &response)
	if err == nil || !strings.Contains(err.Error(), "trailing") {
		t.Fatalf("trailing JSON error = %v", err)
	}
	_, err = transport.DoJSON(t.Context(), HTTPRequest{
		Method: http.MethodPost, Endpoint: server.URL + "/redirect",
	}, &response)
	if err == nil || !strings.Contains(err.Error(), "redirect") {
		t.Fatalf("redirect error = %v", err)
	}
}

func TestHTTPTransportBoundsResponsesAndHidesErrorBodyFromError(t *testing.T) {
	const secret = "synthetic-provider-secret"
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		if request.URL.Path == "/error" {
			writer.Header().Set("Retry-After", "2")
			writer.WriteHeader(http.StatusTooManyRequests)
			_, _ = writer.Write([]byte(`{"message":"` + secret + `"}`))
			return
		}
		_, _ = writer.Write([]byte(`{"value":"` + strings.Repeat("x", 256) + `"}`))
	}))
	defer server.Close()
	transport, err := NewHTTPTransport(TransportOptions{
		HTTPClient: server.Client(), MaximumJSONBytes: 32,
		MaximumErrorBytes: 12,
	})
	if err != nil {
		t.Fatal(err)
	}
	var response map[string]string
	_, err = transport.DoJSON(t.Context(), HTTPRequest{
		Method: http.MethodPost, Endpoint: server.URL + "/large",
	}, &response)
	if !errors.Is(err, ErrProviderResponseTooLarge) {
		t.Fatalf("large response error = %v", err)
	}
	_, err = transport.DoJSON(t.Context(), HTTPRequest{
		Method: http.MethodPost, Endpoint: server.URL + "/error",
	}, &response)
	var statusError *HTTPStatusError
	if !errors.As(err, &statusError) {
		t.Fatalf("status error = %T %v", err, err)
	}
	if strings.Contains(statusError.Error(), secret) {
		t.Fatal("provider body leaked through Error")
	}
	if !statusError.BodyTruncated || statusError.RetryAfter != 2*time.Second {
		t.Fatalf("status error = %#v", statusError)
	}
	if _, err := statusError.RedactedBodySummary(nil); err == nil {
		t.Fatal("unredacted provider error summary was allowed")
	}
	summary, err := statusError.RedactedBodySummary(func(string) string {
		return "[REDACTED]"
	})
	if err != nil || summary != "[REDACTED]" {
		t.Fatalf("redacted summary=%q err=%v", summary, err)
	}
}

func TestHTTPTransportDefaultDisablesEnvironmentProxy(t *testing.T) {
	transport, err := NewHTTPTransport(TransportOptions{})
	if err != nil {
		t.Fatal(err)
	}
	networkTransport, ok := transport.client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("default transport = %T", transport.client.Transport)
	}
	if networkTransport.Proxy != nil {
		t.Fatal("default provider transport can consult a proxy")
	}
}

func TestParseRetryAfterBoundsProviderGuidance(t *testing.T) {
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	if delay := parseRetryAfter("3", now); delay != 3*time.Second {
		t.Fatalf("numeric retry delay = %v", delay)
	}
	if delay := parseRetryAfter(
		now.Add(5*time.Second).Format(http.TimeFormat),
		now,
	); delay != 5*time.Second {
		t.Fatalf("date retry delay = %v", delay)
	}
	if delay := parseRetryAfter("99999999999999999999", now); delay != 0 {
		t.Fatalf("unparseable retry delay = %v", delay)
	}
	if delay := parseRetryAfter("999999", now); delay != maximumProviderRetryAfter {
		t.Fatalf("bounded retry delay = %v", delay)
	}
}

func TestHTTPTransportStreamsBoundedOrderedSSE(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		_ *http.Request,
	) {
		writer.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(writer, ": keepalive\n")
		_, _ = fmt.Fprint(writer, "event: response.output_text.delta\n")
		_, _ = fmt.Fprint(writer, "id: one\n")
		_, _ = fmt.Fprint(writer, "data: {\"delta\":\"hel\"}\n")
		_, _ = fmt.Fprint(writer, "data: {\"delta\":\"lo\"}\n\n")
		_, _ = fmt.Fprint(writer, "data: [DONE]\n\n")
	}))
	defer server.Close()
	transport, err := NewHTTPTransport(TransportOptions{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	var events []SSEEvent
	result, err := transport.StreamSSE(t.Context(), HTTPRequest{
		Method: http.MethodPost, Endpoint: server.URL,
	}, func(event SSEEvent) error {
		events = append(events, event)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.EventsDelivered != 2 || len(events) != 2 {
		t.Fatalf("events=%#v result=%#v", events, result)
	}
	if events[0].Event != "response.output_text.delta" ||
		events[0].ID != "one" ||
		events[0].Data != "{\"delta\":\"hel\"}\n{\"delta\":\"lo\"}" ||
		events[1].Data != "[DONE]" {
		t.Fatalf("events = %#v", events)
	}
}

func TestHTTPTransportCancellationStopsSSEDelivery(t *testing.T) {
	firstDelivered := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(
		writer http.ResponseWriter,
		request *http.Request,
	) {
		flusher := writer.(http.Flusher)
		_, _ = fmt.Fprint(writer, "data: first\n\n")
		flusher.Flush()
		<-request.Context().Done()
	}))
	defer server.Close()
	transport, err := NewHTTPTransport(TransportOptions{HTTPClient: server.Client()})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	var delivered atomic.Int32
	done := make(chan error, 1)
	go func() {
		_, streamErr := transport.StreamSSE(ctx, HTTPRequest{
			Method: http.MethodPost, Endpoint: server.URL,
		}, func(SSEEvent) error {
			delivered.Add(1)
			close(firstDelivered)
			return nil
		})
		done <- streamErr
	}()
	<-firstDelivered
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not stop after cancellation")
	}
	time.Sleep(20 * time.Millisecond)
	if delivered.Load() != 1 {
		t.Fatalf("delivered events after cancellation = %d", delivered.Load())
	}
}

func TestHTTPTransportBoundsSingleAndAggregateSSEData(t *testing.T) {
	for _, test := range []struct {
		name string
		body string
		err  error
	}{
		{
			name: "single line",
			body: "data: " + strings.Repeat("x", 64) + "\n\n",
			err:  ErrSSEEventTooLarge,
		},
		{
			name: "aggregate event",
			body: strings.Repeat("data: 1234567890\n", 8) + "\n",
			err:  ErrSSEEventTooLarge,
		},
		{
			name: "aggregate stream",
			body: strings.Repeat("data: x\n\n", 64),
			err:  ErrProviderResponseTooLarge,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(
				writer http.ResponseWriter,
				_ *http.Request,
			) {
				_, _ = writer.Write([]byte(test.body))
			}))
			defer server.Close()
			transport, err := NewHTTPTransport(TransportOptions{
				HTTPClient: server.Client(), MaximumSSEEventBytes: 32,
				MaximumSSEStreamBytes: 128,
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = transport.StreamSSE(t.Context(), HTTPRequest{
				Method: http.MethodPost, Endpoint: server.URL,
			}, func(SSEEvent) error { return nil })
			if !errors.Is(err, test.err) {
				t.Fatalf("stream error = %v, want %v", err, test.err)
			}
		})
	}
}
