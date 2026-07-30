package transport

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/domain"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	SessionMetadataKey     = "x-codeflux-session-auth"
	RequestIDMetadataKey   = "x-request-id"
	CorrelationMetadataKey = "x-correlation-id"

	maxSafeStringBytes = 64 << 10
	maxPageSize        = 200
	maxGraphNodes      = 1_200
	maxGraphEdges      = 2_400
)

type boundaryContextKey uint8

const (
	correlationContextKey boundaryContextKey = iota
	authenticatedContextKey
)

// RequestLog is a credential-free diagnostic projection of one RPC.
type RequestLog struct {
	Method        string
	CorrelationID string
	Code          codes.Code
	Duration      time.Duration
}

// BoundaryOptions configures the product gRPC boundary.
type BoundaryOptions struct {
	SessionToken         string
	TrustBridgeRequestID bool
	Log                  func(RequestLog)
	Now                  func() time.Time
}

// Boundary owns authentication, validation, safe error mapping, correlation,
// deadline propagation, and diagnostic logging around thin handlers.
type Boundary struct {
	sessionToken         string
	trustBridgeRequestID bool
	log                  func(RequestLog)
	now                  func() time.Time
}

// NewBoundary validates and constructs the product transport boundary.
func NewBoundary(options BoundaryOptions) (*Boundary, error) {
	if len(options.SessionToken) < 32 {
		return nil, errors.New("transport session token must contain at least 32 characters")
	}
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Boundary{
		sessionToken:         options.SessionToken,
		trustBridgeRequestID: options.TrustBridgeRequestID,
		log:                  options.Log,
		now:                  now,
	}, nil
}

// UnaryInterceptor returns the complete unary product boundary.
func (boundary *Boundary) UnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		request any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (response any, returnedErr error) {
		started := boundary.now()
		ctx, correlationID := withCorrelation(ctx)
		defer func() {
			boundary.writeLog(info.FullMethod, correlationID, returnedErr, boundary.now().Sub(started))
		}()

		authenticated, err := boundary.authenticate(ctx)
		if err != nil {
			return nil, MapError(err)
		}
		ctx = context.WithValue(ctx, authenticatedContextKey, authenticated)
		if err := ValidateRequest(request); err != nil {
			return nil, MapError(err)
		}
		response, err = handler(ctx, request)
		return response, MapError(err)
	}
}

// StreamInterceptor returns the complete streaming product boundary.
func (boundary *Boundary) StreamInterceptor() grpc.StreamServerInterceptor {
	return func(
		server any,
		stream grpc.ServerStream,
		info *grpc.StreamServerInfo,
		handler grpc.StreamHandler,
	) (returnedErr error) {
		started := boundary.now()
		ctx, correlationID := withCorrelation(stream.Context())
		defer func() {
			boundary.writeLog(info.FullMethod, correlationID, returnedErr, boundary.now().Sub(started))
		}()

		authenticated, err := boundary.authenticate(ctx)
		if err != nil {
			return MapError(err)
		}
		ctx = context.WithValue(ctx, authenticatedContextKey, authenticated)
		wrapped := &validatingServerStream{
			ServerStream: stream,
			ctx:          ctx,
		}
		return MapError(handler(server, wrapped))
	}
}

func (boundary *Boundary) authenticate(ctx context.Context) (bool, error) {
	incoming, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return false, &ApplicationError{
			Code:        codefluxv1.ErrorCode_ERROR_CODE_UNAUTHENTICATED,
			SafeMessage: "A valid local session is required.",
		}
	}
	for _, candidate := range incoming.Get(SessionMetadataKey) {
		if secureEqual(candidate, boundary.sessionToken) {
			return true, nil
		}
	}
	if boundary.trustBridgeRequestID {
		for _, requestID := range incoming.Get(RequestIDMetadataKey) {
			if validCorrelationID(requestID) {
				return true, nil
			}
		}
	}
	return false, &ApplicationError{
		Code:        codefluxv1.ErrorCode_ERROR_CODE_UNAUTHENTICATED,
		SafeMessage: "A valid local session is required.",
	}
}

func secureEqual(left, right string) bool {
	if len(left) != len(right) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func (boundary *Boundary) writeLog(
	method string,
	correlationID string,
	err error,
	duration time.Duration,
) {
	if boundary.log == nil {
		return
	}
	boundary.log(RequestLog{
		Method:        method,
		CorrelationID: correlationID,
		Code:          status.Code(err),
		Duration:      duration,
	})
}

type validatingServerStream struct {
	grpc.ServerStream
	ctx context.Context
}

func (stream *validatingServerStream) Context() context.Context {
	return stream.ctx
}

func (stream *validatingServerStream) RecvMsg(message any) error {
	if err := stream.ServerStream.RecvMsg(message); err != nil {
		return err
	}
	return ValidateRequest(message)
}

// CorrelationID returns the validated request correlation identity.
func CorrelationID(ctx context.Context) string {
	value, _ := ctx.Value(correlationContextKey).(string)
	return value
}

// IsAuthenticated reports whether the boundary authenticated the local request.
func IsAuthenticated(ctx context.Context) bool {
	value, _ := ctx.Value(authenticatedContextKey).(bool)
	return value
}

func withCorrelation(ctx context.Context) (context.Context, string) {
	incoming, _ := metadata.FromIncomingContext(ctx)
	for _, candidate := range incoming.Get(CorrelationMetadataKey) {
		if validCorrelationID(candidate) {
			return context.WithValue(ctx, correlationContextKey, candidate), candidate
		}
	}
	correlationID := newCorrelationID()
	return context.WithValue(ctx, correlationContextKey, correlationID), correlationID
}

func validCorrelationID(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			strings.ContainsRune("-_.:", character) {
			continue
		}
		return false
	}
	return true
}

func newCorrelationID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "corr-unavailable"
	}
	return "corr-" + hex.EncodeToString(value[:])
}

// RequestValidationError is safe to map to InvalidArgument.
type RequestValidationError struct {
	Field  string
	Reason string
}

func (err *RequestValidationError) Error() string {
	return fmt.Sprintf("%s %s", err.Field, err.Reason)
}

// ValidateRequest enforces transport-shape bounds before application
// conversion or delegation.
func ValidateRequest(request any) error {
	message, ok := request.(proto.Message)
	if !ok || message == nil || !message.ProtoReflect().IsValid() {
		return &RequestValidationError{Field: "request", Reason: "must be a valid protobuf message"}
	}
	if err := validateMessage(message.ProtoReflect(), "request"); err != nil {
		return err
	}
	switch typed := request.(type) {
	case *codefluxv1.GetGraphSliceRequest:
		return validateGraphBounds(typed.GetMaxNodes(), typed.GetMaxEdges())
	case *codefluxv1.ExpandGraphRequest:
		return validateGraphBounds(typed.GetMaxNodes(), typed.GetMaxEdges())
	case *codefluxv1.CompareGraphRevisionsRequest:
		if typed.GetMaxChanges() == 0 || typed.GetMaxChanges() > maxGraphNodes {
			return &RequestValidationError{Field: "max_changes", Reason: "must be between 1 and 1200"}
		}
	}
	return validateRequiredRequestFields(message.ProtoReflect())
}

func validateMessage(message protoreflect.Message, path string) error {
	descriptor := message.Descriptor()
	switch descriptor.FullName() {
	case "codeflux.v1.StableIdentity":
		return validateStableIdentity(&codefluxv1.StableIdentity{
			Kind: codefluxv1.StableIdentityKind(
				message.Get(descriptor.Fields().ByName("kind")).Enum(),
			),
			Value: message.Get(descriptor.Fields().ByName("value")).String(),
		}, path)
	case "codeflux.v1.MutationControl":
		idempotencyKey := message.Get(
			descriptor.Fields().ByName("idempotency_key"),
		).String()
		if !validCorrelationID(idempotencyKey) {
			return &RequestValidationError{Field: path + ".idempotency_key", Reason: "has an invalid format"}
		}
	case "codeflux.v1.PageRequest":
		limit := message.Get(descriptor.Fields().ByName("limit")).Uint()
		if limit > maxPageSize {
			return &RequestValidationError{Field: path + ".limit", Reason: "must not exceed 200"}
		}
		cursor := message.Get(descriptor.Fields().ByName("cursor")).String()
		if len(cursor) > 1024 {
			return &RequestValidationError{Field: path + ".cursor", Reason: "is too long"}
		}
	case "codeflux.v1.Money":
		money := &codefluxv1.Money{
			CurrencyCode: message.Get(descriptor.Fields().ByName("currency_code")).String(),
			MinorUnits:   message.Get(descriptor.Fields().ByName("minor_units")).Int(),
			DecimalPlaces: uint32(
				message.Get(descriptor.Fields().ByName("decimal_places")).Uint(),
			),
		}
		if err := validateMoney(money, path); err != nil {
			return err
		}
	case "google.protobuf.Timestamp":
		timestamp, ok := message.Interface().(*timestamppb.Timestamp)
		if ok {
			if err := timestamp.CheckValid(); err != nil {
				return &RequestValidationError{Field: path, Reason: "is not a normalized UTC timestamp"}
			}
		}
	case "google.protobuf.Duration":
		duration, ok := message.Interface().(*durationpb.Duration)
		if ok {
			if err := duration.CheckValid(); err != nil || duration.AsDuration() < 0 {
				return &RequestValidationError{Field: path, Reason: "must be a valid nonnegative duration"}
			}
		}
	}

	var scalarErr error
	message.Range(func(field protoreflect.FieldDescriptor, value protoreflect.Value) bool {
		if field.Kind() == protoreflect.StringKind {
			text := value.String()
			if len(text) > maxSafeStringBytes || strings.ContainsRune(text, '\x00') {
				scalarErr = &RequestValidationError{
					Field:  path + "." + string(field.Name()),
					Reason: "is too long or contains a null byte",
				}
				return false
			}
		}
		return true
	})
	if scalarErr != nil {
		return scalarErr
	}
	for fieldIndex := 0; fieldIndex < descriptor.Fields().Len(); fieldIndex++ {
		field := descriptor.Fields().Get(fieldIndex)
		if !message.Has(field) {
			continue
		}
		fieldPath := path + "." + string(field.Name())
		if field.IsList() {
			list := message.Get(field).List()
			if list.Len() > 5_000 {
				return &RequestValidationError{Field: fieldPath, Reason: "contains too many values"}
			}
			if field.Message() != nil {
				for index := 0; index < list.Len(); index++ {
					if err := validateMessage(list.Get(index).Message(), fieldPath); err != nil {
						return err
					}
				}
			}
			continue
		}
		if field.Message() != nil {
			if err := validateMessage(message.Get(field).Message(), fieldPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func validateRequiredRequestFields(message protoreflect.Message) error {
	fields := message.Descriptor().Fields()
	if control := fields.ByName("control"); control != nil && !message.Has(control) {
		return &RequestValidationError{Field: "control", Reason: "must not be omitted"}
	}
	for fieldIndex := 0; fieldIndex < fields.Len(); fieldIndex++ {
		field := fields.Get(fieldIndex)
		if field.IsList() || field.Message() == nil ||
			field.Message().FullName() != "codeflux.v1.StableIdentity" ||
			!strings.HasSuffix(string(field.Name()), "_id") {
			continue
		}
		if !message.Has(field) {
			return &RequestValidationError{
				Field:  string(field.Name()),
				Reason: "must not be omitted",
			}
		}
	}
	requiredStrings := map[protoreflect.FullName][]protoreflect.Name{
		"codeflux.v1.OpenWorkspaceRequest":     {"selected_path"},
		"codeflux.v1.CreateThreadRequest":      {"title"},
		"codeflux.v1.SendMessageRequest":       {"body"},
		"codeflux.v1.RenameThreadRequest":      {"title"},
		"codeflux.v1.CreateTaskRequest":        {"requirement"},
		"codeflux.v1.ApproveActionRequest":     {"decision", "scope"},
		"codeflux.v1.RequestRepairRequest":     {"feedback"},
		"codeflux.v1.GetGraphSliceRequest":     {"mode"},
		"codeflux.v1.AcceptChangeRequest":      {"expected_diff_identity"},
		"codeflux.v1.OpenInEditorRequest":      {"workspace_relative_path"},
		"codeflux.v1.ConfigureProviderRequest": {"credential_reference", "model_id"},
	}
	for _, fieldName := range requiredStrings[message.Descriptor().FullName()] {
		field := message.Descriptor().Fields().ByName(fieldName)
		if field == nil || strings.TrimSpace(message.Get(field).String()) == "" {
			return &RequestValidationError{Field: string(fieldName), Reason: "must not be empty"}
		}
	}
	return nil
}

func validateStableIdentity(identity *codefluxv1.StableIdentity, path string) error {
	if identity == nil {
		return &RequestValidationError{Field: path, Reason: "must not be nil"}
	}
	var err error
	switch identity.GetKind() {
	case codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_PROJECT:
		_, err = domain.ParseProjectID(identity.GetValue())
	case codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_REPOSITORY:
		_, err = domain.ParseRepositoryID(identity.GetValue())
	case codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_WORKSPACE:
		_, err = domain.ParseWorkspaceID(identity.GetValue())
	case codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_SESSION:
		_, err = domain.ParseSessionID(identity.GetValue())
	case codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD:
		_, err = domain.ParseThreadID(identity.GetValue())
	case codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_MESSAGE:
		_, err = domain.ParseMessageID(identity.GetValue())
	case codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK:
		_, err = domain.ParseTaskID(identity.GetValue())
	case codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_RUN:
		_, err = domain.ParseRunID(identity.GetValue())
	case codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_EVENT:
		_, err = domain.ParseEventID(identity.GetValue())
	case codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_CHECKPOINT:
		_, err = domain.ParseCheckpointID(identity.GetValue())
	case codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_APPROVAL:
		_, err = domain.ParseApprovalID(identity.GetValue())
	case codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_GRAPH:
		_, err = domain.ParseGraphID(identity.GetValue())
	case codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_GRAPH_REVISION:
		_, err = domain.ParseGraphRevisionID(identity.GetValue())
	case codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_NODE:
		_, err = domain.ParseNodeID(identity.GetValue())
	case codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_EDGE:
		_, err = domain.ParseEdgeID(identity.GetValue())
	case codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_VALIDATION:
		_, err = domain.ParseValidationID(identity.GetValue())
	case codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_EVIDENCE:
		_, err = domain.ParseEvidenceID(identity.GetValue())
	case codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_ARTIFACT:
		_, err = domain.ParseArtifactID(identity.GetValue())
	case codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_ATOM:
		_, err = domain.ParseAtomID(identity.GetValue())
	case codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_MODEL_REQUEST:
		_, err = domain.ParseModelRequestID(identity.GetValue())
	case codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_PROVIDER:
		_, err = domain.ParseProviderID(identity.GetValue())
	case codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_BUDGET:
		_, err = domain.ParseBudgetID(identity.GetValue())
	default:
		err = domain.ErrInvalidID
	}
	if err != nil {
		return &RequestValidationError{Field: path, Reason: "contains an invalid stable identity"}
	}
	return nil
}

func validateMoney(money *codefluxv1.Money, path string) error {
	currency := money.GetCurrencyCode()
	if len(currency) != 3 {
		return &RequestValidationError{Field: path + ".currency_code", Reason: "must contain three uppercase letters"}
	}
	for _, character := range currency {
		if character < 'A' || character > 'Z' {
			return &RequestValidationError{Field: path + ".currency_code", Reason: "must contain three uppercase letters"}
		}
	}
	if money.GetDecimalPlaces() > 9 {
		return &RequestValidationError{Field: path + ".decimal_places", Reason: "must not exceed 9"}
	}
	if money.GetMinorUnits() < 0 {
		return &RequestValidationError{Field: path + ".minor_units", Reason: "must not be negative"}
	}
	return nil
}

func validateGraphBounds(nodes, edges uint32) error {
	if nodes == 0 || nodes > maxGraphNodes {
		return &RequestValidationError{Field: "max_nodes", Reason: "must be between 1 and 1200"}
	}
	if edges == 0 || edges > maxGraphEdges {
		return &RequestValidationError{Field: "max_edges", Reason: "must be between 1 and 2400"}
	}
	return nil
}

// ApplicationError carries a stable safe transport classification.
type ApplicationError struct {
	Code        codefluxv1.ErrorCode
	SafeMessage string
	Retryable   bool
	EntityID    *codefluxv1.StableIdentity
}

func (err *ApplicationError) Error() string {
	return err.SafeMessage
}

// MapError maps internal failures to a safe status with ApiErrorDetail.
func MapError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := status.FromError(err); ok {
		if status.Code(err) != codes.Unknown {
			return err
		}
	}

	detail := &codefluxv1.ApiErrorDetail{
		Code:        codefluxv1.ErrorCode_ERROR_CODE_INTERNAL,
		SafeMessage: "The local request could not be completed.",
	}
	grpcCode := codes.Internal
	var applicationErr *ApplicationError
	var validationErr *RequestValidationError
	switch {
	case errors.As(err, &applicationErr):
		detail.Code = applicationErr.Code
		detail.SafeMessage = safeMessage(applicationErr.SafeMessage)
		detail.Retryable = applicationErr.Retryable
		detail.EntityId = applicationErr.EntityID
		grpcCode = grpcCodeForError(applicationErr.Code)
	case errors.As(err, &validationErr),
		errors.Is(err, domain.ErrInvalidID),
		errors.Is(err, domain.ErrUnknownState):
		detail.Code = codefluxv1.ErrorCode_ERROR_CODE_INVALID_ARGUMENT
		detail.SafeMessage = "The request contains an invalid value."
		grpcCode = codes.InvalidArgument
	case errors.Is(err, domain.ErrInvalidTransition):
		detail.Code = codefluxv1.ErrorCode_ERROR_CODE_INVALID_TRANSITION
		detail.SafeMessage = "The requested state transition is not allowed."
		grpcCode = codes.FailedPrecondition
	case errors.Is(err, context.Canceled):
		detail.Code = codefluxv1.ErrorCode_ERROR_CODE_CANCELLED
		detail.SafeMessage = "The request was cancelled."
		grpcCode = codes.Canceled
	case errors.Is(err, context.DeadlineExceeded):
		detail.Code = codefluxv1.ErrorCode_ERROR_CODE_DEADLINE_EXCEEDED
		detail.SafeMessage = "The request deadline expired."
		detail.Retryable = true
		grpcCode = codes.DeadlineExceeded
	}
	statusValue := status.New(grpcCode, detail.GetSafeMessage())
	withDetails, detailsErr := statusValue.WithDetails(detail)
	if detailsErr != nil {
		return statusValue.Err()
	}
	return withDetails.Err()
}

func safeMessage(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(value, "\r", " "), "\n", " "))
	if value == "" {
		return "The local request could not be completed."
	}
	if len(value) > 512 {
		return value[:512]
	}
	return value
}

func grpcCodeForError(code codefluxv1.ErrorCode) codes.Code {
	switch code {
	case codefluxv1.ErrorCode_ERROR_CODE_NOT_FOUND:
		return codes.NotFound
	case codefluxv1.ErrorCode_ERROR_CODE_INVALID_ARGUMENT:
		return codes.InvalidArgument
	case codefluxv1.ErrorCode_ERROR_CODE_INVALID_TRANSITION,
		codefluxv1.ErrorCode_ERROR_CODE_BUDGET_EXHAUSTED,
		codefluxv1.ErrorCode_ERROR_CODE_RECOVERY_REQUIRED:
		return codes.FailedPrecondition
	case codefluxv1.ErrorCode_ERROR_CODE_STALE_REVISION,
		codefluxv1.ErrorCode_ERROR_CODE_DUPLICATE:
		return codes.Aborted
	case codefluxv1.ErrorCode_ERROR_CODE_DENIED:
		return codes.PermissionDenied
	case codefluxv1.ErrorCode_ERROR_CODE_CANCELLED:
		return codes.Canceled
	case codefluxv1.ErrorCode_ERROR_CODE_PROVIDER_RETRYABLE:
		return codes.Unavailable
	case codefluxv1.ErrorCode_ERROR_CODE_CORRUPTION,
		codefluxv1.ErrorCode_ERROR_CODE_INTERNAL:
		return codes.Internal
	case codefluxv1.ErrorCode_ERROR_CODE_UNAUTHENTICATED:
		return codes.Unauthenticated
	case codefluxv1.ErrorCode_ERROR_CODE_DEADLINE_EXCEEDED:
		return codes.DeadlineExceeded
	default:
		return codes.Unknown
	}
}

// StopGRPCServer gracefully drains until ctx expires, then force-stops.
func StopGRPCServer(ctx context.Context, server *grpc.Server) error {
	if server == nil {
		return errors.New("gRPC server is nil")
	}
	stopped := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(stopped)
	}()
	select {
	case <-stopped:
		return nil
	case <-ctx.Done():
		server.Stop()
		<-stopped
		return ctx.Err()
	}
}
