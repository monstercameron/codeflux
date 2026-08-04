package providers

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// DefaultRequestTimeout is the bound one provider request runs under when no
// configuration supplies one.
//
// It is exported because a settings surface reports the timeout a request will
// actually run under, and a second copy of the number written somewhere else
// would eventually report a bound nothing enforces.
const DefaultRequestTimeout = 5 * time.Minute

const (
	defaultProviderRequestTimeout = DefaultRequestTimeout
	defaultMaximumRequestBytes    = 8 << 20
	defaultMaximumJSONBytes       = 8 << 20
	defaultMaximumErrorBytes      = 64 << 10
	defaultMaximumSSEEventBytes   = 1 << 20
	defaultMaximumSSEStreamBytes  = 64 << 20
	maximumProviderRequestTimeout = 30 * time.Minute
	maximumProviderRetryAfter     = 24 * time.Hour
	maximumProviderRequestBytes   = 64 << 20
	maximumProviderResponseBytes  = 256 << 20
	maximumProviderSSEEventBytes  = 8 << 20
)

var (
	ErrEndpointApprovalRequired = errors.New("provider endpoint requires explicit remote approval")
	ErrInsecureRemoteEndpoint   = errors.New("plain HTTP provider endpoint must be loopback")
	ErrProviderRequestTooLarge  = errors.New("provider request exceeded the configured limit")
	ErrProviderResponseTooLarge = errors.New("provider response exceeded the configured limit")
	ErrSSEEventTooLarge         = errors.New("provider stream event exceeded the configured limit")
)

// EndpointPolicy distinguishes local endpoints from explicitly approved remote
// provider endpoints.
type EndpointPolicy struct {
	AllowRemote bool
}

// ValidateProviderEndpoint parses an absolute HTTP provider endpoint and
// enforces the local-first network boundary.
func ValidateProviderEndpoint(raw string, policy EndpointPolicy) (*url.URL, error) {
	if strings.TrimSpace(raw) != raw || raw == "" {
		return nil, errors.New("provider endpoint must be non-empty without surrounding whitespace")
	}
	endpoint, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse provider endpoint: %w", err)
	}
	if !endpoint.IsAbs() || endpoint.Host == "" {
		return nil, errors.New("provider endpoint must be an absolute URL")
	}
	if endpoint.User != nil {
		return nil, errors.New("provider endpoint must not contain user information")
	}
	if endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return nil, errors.New("provider endpoint must not contain a query or fragment")
	}
	if endpoint.Scheme != "http" && endpoint.Scheme != "https" {
		return nil, errors.New("provider endpoint scheme must be HTTP or HTTPS")
	}
	host := endpoint.Hostname()
	if host == "" {
		return nil, errors.New("provider endpoint host is required")
	}
	loopback := false
	if address := net.ParseIP(host); address != nil {
		loopback = address.IsLoopback()
	}
	if endpoint.Scheme == "http" && !loopback {
		return nil, ErrInsecureRemoteEndpoint
	}
	if !loopback && !policy.AllowRemote {
		return nil, ErrEndpointApprovalRequired
	}
	return endpoint, nil
}

// TransportOptions configures bounded provider HTTP I/O.
type TransportOptions struct {
	HTTPClient            *http.Client
	RequestTimeout        time.Duration
	MaximumRequestBytes   int64
	MaximumJSONBytes      int64
	MaximumErrorBytes     int64
	MaximumSSEEventBytes  int
	MaximumSSEStreamBytes int64
	Now                   func() time.Time
}

// HTTPTransport performs bounded provider HTTP exchanges without following
// redirects.
type HTTPTransport struct {
	client                *http.Client
	requestTimeout        time.Duration
	maximumRequestBytes   int64
	maximumJSONBytes      int64
	maximumErrorBytes     int64
	maximumSSEEventBytes  int
	maximumSSEStreamBytes int64
	now                   func() time.Time
}

// NewHTTPTransport validates limits and creates a provider HTTP transport. Its
// default network transport never consults proxy environment variables.
func NewHTTPTransport(options TransportOptions) (*HTTPTransport, error) {
	if options.RequestTimeout == 0 {
		options.RequestTimeout = defaultProviderRequestTimeout
	}
	if options.MaximumRequestBytes == 0 {
		options.MaximumRequestBytes = defaultMaximumRequestBytes
	}
	if options.MaximumJSONBytes == 0 {
		options.MaximumJSONBytes = defaultMaximumJSONBytes
	}
	if options.MaximumErrorBytes == 0 {
		options.MaximumErrorBytes = defaultMaximumErrorBytes
	}
	if options.MaximumSSEEventBytes == 0 {
		options.MaximumSSEEventBytes = defaultMaximumSSEEventBytes
	}
	if options.MaximumSSEStreamBytes == 0 {
		options.MaximumSSEStreamBytes = defaultMaximumSSEStreamBytes
	}
	switch {
	case options.RequestTimeout < time.Millisecond ||
		options.RequestTimeout > maximumProviderRequestTimeout:
		return nil, errors.New("provider request timeout is outside supported bounds")
	case options.MaximumRequestBytes < 1 ||
		options.MaximumRequestBytes > maximumProviderRequestBytes:
		return nil, errors.New("provider request byte limit is outside supported bounds")
	case options.MaximumJSONBytes < 1 ||
		options.MaximumJSONBytes > maximumProviderResponseBytes:
		return nil, errors.New("provider JSON byte limit is outside supported bounds")
	case options.MaximumErrorBytes < 1 ||
		options.MaximumErrorBytes > maximumProviderResponseBytes:
		return nil, errors.New("provider error byte limit is outside supported bounds")
	case options.MaximumSSEEventBytes < 1 ||
		options.MaximumSSEEventBytes > maximumProviderSSEEventBytes:
		return nil, errors.New("provider SSE event byte limit is outside supported bounds")
	case options.MaximumSSEStreamBytes < 1 ||
		options.MaximumSSEStreamBytes > maximumProviderResponseBytes:
		return nil, errors.New("provider SSE stream byte limit is outside supported bounds")
	}

	client := options.HTTPClient
	if client == nil {
		client = &http.Client{
			Transport: &http.Transport{
				Proxy:               nil,
				DialContext:         (&net.Dialer{}).DialContext,
				ForceAttemptHTTP2:   true,
				MaxIdleConns:        20,
				MaxIdleConnsPerHost: 4,
				IdleConnTimeout:     90 * time.Second,
				TLSHandshakeTimeout: 10 * time.Second,
				// One timeout governs a request, and it is the request's own.
				//
				// This was thirty seconds while RequestTimeout was five
				// minutes, which is not a stricter bound but a contradictory
				// one: a reasoning model at high effort thinks for longer than
				// thirty seconds before it emits a single response header, so
				// the client killed its own request every time and reported
				// "http2: timeout awaiting response headers". The adapter then
				// retried three times, each retry thought just as long, and the
				// three failures together looked exactly like a provider that
				// had gone away: about ninety seconds of nothing, no status
				// code, and nothing to tell it apart from an outage.
				//
				// It ended runs whose programs were already correct. Ladder rung
				// 16 on 2026-08-04 built, ran, printed exactly what was asked and
				// survived every hostile input, twice, and was recorded as paused
				// because of this. It is also what the escalation step-down was
				// built to survive, and escalating is what made the model think
				// past the timer in the first place.
				//
				// Kept as a field rather than removed, so a server that accepts
				// a connection and then says nothing at all is still bounded.
				ResponseHeaderTimeout: options.RequestTimeout,
			},
		}
	} else {
		cloned := *client
		client = &cloned
	}
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return errors.New("provider redirects are forbidden")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	return &HTTPTransport{
		client:                client,
		requestTimeout:        options.RequestTimeout,
		maximumRequestBytes:   options.MaximumRequestBytes,
		maximumJSONBytes:      options.MaximumJSONBytes,
		maximumErrorBytes:     options.MaximumErrorBytes,
		maximumSSEEventBytes:  options.MaximumSSEEventBytes,
		maximumSSEStreamBytes: options.MaximumSSEStreamBytes,
		now:                   options.Now,
	}, nil
}

// HTTPRequest is one immutable provider HTTP request body and endpoint.
type HTTPRequest struct {
	Method         string
	Endpoint       string
	Header         http.Header
	Body           []byte
	EndpointPolicy EndpointPolicy
}

// HTTPResult reports safe transport-level evidence for one physical request.
type HTTPResult struct {
	StatusCode      int
	StartedAt       time.Time
	FirstResponseAt time.Time
	FinishedAt      time.Time
	ResponseBytes   int64
	EventsDelivered int
	responseHeader  http.Header
}

// Latency returns the non-negative observed transport duration.
func (result HTTPResult) Latency() time.Duration {
	if result.FinishedAt.Before(result.StartedAt) {
		return 0
	}
	return result.FinishedAt.Sub(result.StartedAt)
}

// ResponseHeader returns one untrusted provider response-header value. Adapter
// callers must select an expected header and redact it before persistence.
func (result HTTPResult) ResponseHeader(name string) string {
	if result.responseHeader == nil {
		return ""
	}
	return result.responseHeader.Get(name)
}

// HTTPStatusError retains a bounded provider error body for adapter-local
// classification without including it in Error or exported metadata.
type HTTPStatusError struct {
	StatusCode    int
	RetryAfter    time.Duration
	BodyTruncated bool
	body          []byte
}

func (failure *HTTPStatusError) Error() string {
	return fmt.Sprintf("provider returned HTTP status %d", failure.StatusCode)
}

// DecodeBody decodes the bounded error body without exposing it through logs.
func (failure *HTTPStatusError) DecodeBody(target any) error {
	return decodeSingleJSON(bytes.NewReader(failure.body), int64(len(failure.body)), target)
}

// RedactedBodySummary returns bounded printable provider text after applying a
// mandatory redaction callback.
func (failure *HTTPStatusError) RedactedBodySummary(
	redact func(string) string,
) (string, error) {
	if redact == nil {
		return "", errors.New("provider error redaction callback is required")
	}
	valid := strings.ToValidUTF8(string(failure.body), "\uFFFD")
	valid = strings.Map(func(character rune) rune {
		if character == '\n' || character == '\r' || character == '\t' ||
			!unicode.IsControl(character) {
			return character
		}
		return '\uFFFD'
	}, valid)
	return strings.TrimSpace(redact(valid)), nil
}

// DoJSON executes a request and strictly decodes one bounded successful JSON
// value. A nil target accepts an empty successful body.
func (transport *HTTPTransport) DoJSON(
	ctx context.Context,
	request HTTPRequest,
	target any,
) (HTTPResult, error) {
	response, result, cancel, err := transport.execute(ctx, request)
	if err != nil {
		return result, err
	}
	defer cancel()
	defer response.Body.Close()
	result.StatusCode = response.StatusCode
	result.responseHeader = response.Header.Clone()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		failure, bytesRead, readErr := transport.readStatusError(response)
		result.ResponseBytes = bytesRead
		result.FinishedAt = transport.now().UTC()
		if readErr != nil {
			return result, readErr
		}
		return result, failure
	}
	if target == nil && response.ContentLength == 0 {
		result.FinishedAt = transport.now().UTC()
		return result, nil
	}
	bytesRead, decodeErr := decodeSingleJSONCounting(
		response.Body, transport.maximumJSONBytes, target,
	)
	result.ResponseBytes = bytesRead
	result.FinishedAt = transport.now().UTC()
	if decodeErr != nil {
		return result, decodeErr
	}
	if closeErr := response.Body.Close(); closeErr != nil {
		return result, closeErr
	}
	return result, nil
}

// SSEEvent is one normalized server-sent event.
type SSEEvent struct {
	Event string
	Data  string
	ID    string
}

// StreamSSE executes a request and delivers bounded server-sent events in
// response order until EOF, callback failure, cancellation, or a size limit.
func (transport *HTTPTransport) StreamSSE(
	ctx context.Context,
	request HTTPRequest,
	deliver func(SSEEvent) error,
) (HTTPResult, error) {
	if deliver == nil {
		return HTTPResult{}, errors.New("provider SSE callback is required")
	}
	response, result, cancel, err := transport.execute(ctx, request)
	if err != nil {
		return result, err
	}
	defer cancel()
	defer response.Body.Close()
	result.StatusCode = response.StatusCode
	result.responseHeader = response.Header.Clone()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		failure, bytesRead, readErr := transport.readStatusError(response)
		result.ResponseBytes = bytesRead
		result.FinishedAt = transport.now().UTC()
		if readErr != nil {
			return result, readErr
		}
		return result, failure
	}

	bounded := &maximumReader{
		reader: response.Body,
		limit:  transport.maximumSSEStreamBytes,
	}
	scanner := bufio.NewScanner(bounded)
	scanner.Buffer(
		make([]byte, 0, min(64<<10, transport.maximumSSEEventBytes)),
		transport.maximumSSEEventBytes+1,
	)
	var event SSEEvent
	dataBytes := 0
	dispatch := func() error {
		if event.Data == "" {
			event = SSEEvent{}
			dataBytes = 0
			return nil
		}
		event.Data = strings.TrimSuffix(event.Data, "\n")
		if err := deliver(event); err != nil {
			return err
		}
		result.EventsDelivered++
		event = SSEEvent{}
		dataBytes = 0
		return nil
	}
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			result.ResponseBytes = bounded.read
			result.FinishedAt = transport.now().UTC()
			return result, err
		}
		line := scanner.Text()
		if line == "" {
			if err := dispatch(); err != nil {
				result.ResponseBytes = bounded.read
				result.FinishedAt = transport.now().UTC()
				return result, err
			}
			continue
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, found := strings.Cut(line, ":")
		if found && strings.HasPrefix(value, " ") {
			value = strings.TrimPrefix(value, " ")
		}
		switch field {
		case "event":
			event.Event = value
		case "data":
			dataBytes += len(value) + 1
			if dataBytes > transport.maximumSSEEventBytes {
				result.ResponseBytes = bounded.read
				result.FinishedAt = transport.now().UTC()
				return result, ErrSSEEventTooLarge
			}
			event.Data += value + "\n"
		case "id":
			if !strings.ContainsRune(value, '\x00') {
				event.ID = value
			}
		}
	}
	result.ResponseBytes = bounded.read
	result.FinishedAt = transport.now().UTC()
	if scanErr := scanner.Err(); scanErr != nil {
		if errors.Is(scanErr, ErrProviderResponseTooLarge) {
			return result, ErrProviderResponseTooLarge
		}
		if strings.Contains(scanErr.Error(), "token too long") {
			return result, ErrSSEEventTooLarge
		}
		return result, scanErr
	}
	if err := dispatch(); err != nil {
		return result, err
	}
	if closeErr := response.Body.Close(); closeErr != nil {
		return result, closeErr
	}
	return result, nil
}

func (transport *HTTPTransport) execute(
	ctx context.Context,
	request HTTPRequest,
) (*http.Response, HTTPResult, context.CancelFunc, error) {
	result := HTTPResult{StartedAt: transport.now().UTC()}
	if len(request.Body) > int(transport.maximumRequestBytes) {
		result.FinishedAt = transport.now().UTC()
		return nil, result, func() {}, ErrProviderRequestTooLarge
	}
	endpoint, err := ValidateProviderEndpoint(request.Endpoint, request.EndpointPolicy)
	if err != nil {
		result.FinishedAt = transport.now().UTC()
		return nil, result, func() {}, err
	}
	if request.Method == "" {
		return nil, result, func() {}, errors.New("provider HTTP method is required")
	}
	requestContext, cancel := context.WithTimeout(ctx, transport.requestTimeout)
	httpRequest, err := http.NewRequestWithContext(
		requestContext,
		request.Method,
		endpoint.String(),
		bytes.NewReader(request.Body),
	)
	if err != nil {
		cancel()
		result.FinishedAt = transport.now().UTC()
		return nil, result, func() {}, err
	}
	httpRequest.Header = request.Header.Clone()
	response, err := transport.client.Do(httpRequest)
	if err != nil {
		cancel()
		result.FinishedAt = transport.now().UTC()
		return nil, result, func() {}, err
	}
	result.FirstResponseAt = transport.now().UTC()
	return response, result, cancel, nil
}

func (transport *HTTPTransport) readStatusError(
	response *http.Response,
) (*HTTPStatusError, int64, error) {
	body, bytesRead, truncated, err := readBounded(
		response.Body,
		transport.maximumErrorBytes,
	)
	if err != nil {
		return nil, bytesRead, err
	}
	return &HTTPStatusError{
		StatusCode:    response.StatusCode,
		RetryAfter:    parseRetryAfter(response.Header.Get("Retry-After"), transport.now()),
		BodyTruncated: truncated,
		body:          body,
	}, bytesRead, nil
}

func parseRetryAfter(raw string, now time.Time) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	allDigits := true
	for _, character := range raw {
		if character < '0' || character > '9' {
			allDigits = false
			break
		}
	}
	if allDigits {
		seconds, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return 0
		}
		if seconds <= 0 {
			return 0
		}
		if seconds > int64(maximumProviderRetryAfter/time.Second) {
			return maximumProviderRetryAfter
		}
		return time.Duration(seconds) * time.Second
	}
	when, err := http.ParseTime(raw)
	if err != nil || !when.After(now) {
		return 0
	}
	delay := when.Sub(now)
	if delay > maximumProviderRetryAfter {
		return maximumProviderRetryAfter
	}
	return delay
}

func decodeSingleJSON(reader io.Reader, maximum int64, target any) error {
	_, err := decodeSingleJSONCounting(reader, maximum, target)
	return err
}

func decodeSingleJSONCounting(
	reader io.Reader,
	maximum int64,
	target any,
) (int64, error) {
	if target == nil {
		return 0, errors.New("provider JSON target is required for a non-empty response")
	}
	body, bytesRead, truncated, err := readBounded(reader, maximum)
	if err != nil {
		return bytesRead, err
	}
	if truncated {
		return bytesRead, ErrProviderResponseTooLarge
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(target); err != nil {
		return bytesRead, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return bytesRead, errors.New("provider JSON response contains trailing values")
		}
		return bytesRead, err
	}
	return bytesRead, nil
}

func readBounded(
	reader io.Reader,
	maximum int64,
) ([]byte, int64, bool, error) {
	limited := io.LimitReader(reader, maximum+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, int64(len(body)), false, err
	}
	if int64(len(body)) > maximum {
		return body[:maximum], int64(len(body)), true, nil
	}
	return body, int64(len(body)), false, nil
}

type maximumReader struct {
	reader io.Reader
	limit  int64
	read   int64
}

func (reader *maximumReader) Read(buffer []byte) (int, error) {
	if reader.read >= reader.limit {
		var probe [1]byte
		count, err := reader.reader.Read(probe[:])
		if count > 0 {
			reader.read += int64(count)
			return 0, ErrProviderResponseTooLarge
		}
		return 0, err
	}
	remaining := reader.limit - reader.read
	if int64(len(buffer)) > remaining {
		buffer = buffer[:remaining]
	}
	count, err := reader.reader.Read(buffer)
	reader.read += int64(count)
	return count, err
}
