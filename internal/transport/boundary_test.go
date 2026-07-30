package transport

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/dynamicpb"
)

const (
	boundaryTestToken = "boundary-test-session-token-32-bytes-minimum"
	boundaryTestUUID  = "01890f3c-4a00-7abc-8def-0123456789ab"
)

func TestEveryProductMethodThroughAuthenticatedInProcessAPI(t *testing.T) {
	connection, cleanup := startBoundaryTestServer(t, nil)
	defer cleanup()
	ctx := authenticatedTestContext(t.Context())

	product := codefluxv1.File_codeflux_v1_product_api_proto
	for serviceIndex := 0; serviceIndex < product.Services().Len(); serviceIndex++ {
		service := product.Services().Get(serviceIndex)
		for methodIndex := 0; methodIndex < service.Methods().Len(); methodIndex++ {
			method := service.Methods().Get(methodIndex)
			t.Run(string(service.Name())+"/"+string(method.Name()), func(t *testing.T) {
				request := dynamicpb.NewMessage(method.Input())
				populateDynamicMessage(request.ProtoReflect(), string(method.Input().Name()))
				response := dynamicpb.NewMessage(method.Output())
				fullMethod := "/" + string(service.FullName()) + "/" + string(method.Name())
				if method.IsStreamingServer() {
					stream, err := connection.NewStream(
						ctx,
						&grpc.StreamDesc{ServerStreams: true},
						fullMethod,
					)
					if err != nil {
						t.Fatal(err)
					}
					if err := stream.SendMsg(request); err != nil {
						t.Fatal(err)
					}
					if err := stream.CloseSend(); err != nil {
						t.Fatal(err)
					}
					if err := stream.RecvMsg(response); err != nil {
						t.Fatal(err)
					}
					if err := stream.RecvMsg(response); !errors.Is(err, io.EOF) {
						t.Fatalf("terminal receive = %v, want EOF", err)
					}
					return
				}
				if err := connection.Invoke(ctx, fullMethod, request, response); err != nil {
					t.Fatal(err)
				}
			})
		}
	}
}

func TestGeneratedClientsExecuteSyntheticJourney(t *testing.T) {
	connection, cleanup := startBoundaryTestServer(t, nil)
	defer cleanup()
	ctx := authenticatedTestContext(t.Context())
	control := &codefluxv1.MutationControl{
		IdempotencyKey:   "journey-1",
		ExpectedRevision: uint64Pointer(1),
	}
	workspaceID := fixtureIdentity(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_WORKSPACE, "wsp")
	threadID := fixtureIdentity(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD, "thr")
	messageID := fixtureIdentity(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_MESSAGE, "msg")
	taskID := fixtureIdentity(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK, "tsk")
	sessionID := fixtureIdentity(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_SESSION, "ses")

	workspace := codefluxv1.NewWorkspaceServiceClient(connection)
	if _, err := workspace.OpenWorkspace(ctx, &codefluxv1.OpenWorkspaceRequest{
		Control: control, SelectedPath: "C:/synthetic/repository",
	}); err != nil {
		t.Fatal(err)
	}
	thread := codefluxv1.NewThreadServiceClient(connection)
	if _, err := thread.CreateThread(ctx, &codefluxv1.CreateThreadRequest{
		Control: control, WorkspaceId: workspaceID, Title: "Synthetic journey",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := thread.SendMessage(ctx, &codefluxv1.SendMessageRequest{
		Control: control, ThreadId: threadID, Body: "Implement the bounded change.",
	}); err != nil {
		t.Fatal(err)
	}
	task := codefluxv1.NewTaskServiceClient(connection)
	if _, err := task.CreateTask(ctx, &codefluxv1.CreateTaskRequest{
		Control: control, ThreadId: threadID, SourceMessageId: messageID, Requirement: "Implement the bounded change.",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := task.StartTask(ctx, &codefluxv1.StartTaskRequest{
		Control: control, TaskId: taskID, ApprovedPlanRevision: 1,
	}); err != nil {
		t.Fatal(err)
	}
	graph := codefluxv1.NewGraphServiceClient(connection)
	if _, err := graph.GetGraphSlice(ctx, &codefluxv1.GetGraphSliceRequest{
		TaskId: taskID, Mode: "execution", MaxNodes: 300, MaxEdges: 600,
	}); err != nil {
		t.Fatal(err)
	}
	review := codefluxv1.NewReviewServiceClient(connection)
	if _, err := review.GetDiffSummary(ctx, &codefluxv1.GetDiffSummaryRequest{TaskId: taskID}); err != nil {
		t.Fatal(err)
	}
	if _, err := review.GetValidationReport(ctx, &codefluxv1.GetValidationReportRequest{TaskId: taskID}); err != nil {
		t.Fatal(err)
	}
	if _, err := review.AcceptChange(ctx, &codefluxv1.AcceptChangeRequest{
		Control: control, TaskId: taskID, ExpectedDiffIdentity: "diff-fixture", ExpectedValidationRevision: 1,
	}); err != nil {
		t.Fatal(err)
	}
	session := codefluxv1.NewSessionServiceClient(connection)
	stream, err := session.SubscribeSession(ctx, &codefluxv1.SubscribeSessionRequest{
		SessionId: sessionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatal(err)
	}
}

func TestBoundaryRejectsUnauthorizedAndMalformedRequests(t *testing.T) {
	boundary, err := NewBoundary(BoundaryOptions{SessionToken: boundaryTestToken})
	if err != nil {
		t.Fatal(err)
	}
	interceptor := boundary.UnaryInterceptor()
	info := &grpc.UnaryServerInfo{FullMethod: "/codeflux.v1.ThreadService/CreateThread"}
	valid := &codefluxv1.CreateThreadRequest{
		Control:     &codefluxv1.MutationControl{IdempotencyKey: "valid-key"},
		WorkspaceId: fixtureIdentity(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_WORKSPACE, "wsp"),
		Title:       "Title",
	}
	handler := func(ctx context.Context, _ any) (any, error) {
		if !IsAuthenticated(ctx) {
			t.Fatal("handler did not receive authenticated context")
		}
		return &codefluxv1.CreateThreadResponse{}, nil
	}

	if _, err := interceptor(t.Context(), valid, info, handler); status.Code(err) != codes.Unauthenticated {
		t.Fatalf("unauthorized code = %s, want Unauthenticated", status.Code(err))
	}
	ctx := authenticatedTestContext(t.Context())
	malformed := &codefluxv1.CreateThreadRequest{Title: "Title"}
	if _, err := interceptor(ctx, malformed, info, handler); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("malformed code = %s, want InvalidArgument", status.Code(err))
	}
	valid.Control.IdempotencyKey = "contains space"
	if _, err := interceptor(ctx, valid, info, handler); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("invalid idempotency code = %s, want InvalidArgument", status.Code(err))
	}
}

func TestBoundaryMapsStaleDuplicateAndDeadlineErrors(t *testing.T) {
	boundary, err := NewBoundary(BoundaryOptions{SessionToken: boundaryTestToken})
	if err != nil {
		t.Fatal(err)
	}
	interceptor := boundary.UnaryInterceptor()
	info := &grpc.UnaryServerInfo{FullMethod: "/fixture/Call"}
	request := &codefluxv1.TestProviderRequest{
		ProviderId: fixtureIdentity(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_PROVIDER, "prv"),
	}
	ctx := authenticatedTestContext(t.Context())
	testCases := []struct {
		name     string
		error    error
		wantCode codes.Code
		wantAPI  codefluxv1.ErrorCode
	}{
		{
			name: "stale",
			error: &ApplicationError{
				Code:        codefluxv1.ErrorCode_ERROR_CODE_STALE_REVISION,
				SafeMessage: "The entity changed; refresh and retry.",
			},
			wantCode: codes.Aborted,
			wantAPI:  codefluxv1.ErrorCode_ERROR_CODE_STALE_REVISION,
		},
		{
			name: "duplicate",
			error: &ApplicationError{
				Code:        codefluxv1.ErrorCode_ERROR_CODE_DUPLICATE,
				SafeMessage: "The original result already exists.",
			},
			wantCode: codes.Aborted,
			wantAPI:  codefluxv1.ErrorCode_ERROR_CODE_DUPLICATE,
		},
		{
			name:     "deadline",
			error:    context.DeadlineExceeded,
			wantCode: codes.DeadlineExceeded,
			wantAPI:  codefluxv1.ErrorCode_ERROR_CODE_DEADLINE_EXCEEDED,
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, callErr := interceptor(ctx, request, info, func(context.Context, any) (any, error) {
				return nil, testCase.error
			})
			if status.Code(callErr) != testCase.wantCode {
				t.Fatalf("code = %s, want %s", status.Code(callErr), testCase.wantCode)
			}
			if got := apiErrorDetail(t, callErr).GetCode(); got != testCase.wantAPI {
				t.Fatalf("API code = %s, want %s", got, testCase.wantAPI)
			}
		})
	}
}

func TestBoundaryPropagatesDeadlineCorrelationAndSafeLog(t *testing.T) {
	var mu sync.Mutex
	var logs []RequestLog
	boundary, err := NewBoundary(BoundaryOptions{
		SessionToken: boundaryTestToken,
		Log: func(entry RequestLog) {
			mu.Lock()
			defer mu.Unlock()
			logs = append(logs, entry)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs(
		SessionMetadataKey, boundaryTestToken,
		CorrelationMetadataKey, "corr-fixture",
	))
	request := &codefluxv1.TestProviderRequest{
		ProviderId: fixtureIdentity(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_PROVIDER, "prv"),
	}
	_, err = boundary.UnaryInterceptor()(ctx, request, &grpc.UnaryServerInfo{FullMethod: "/fixture/Call"},
		func(handlerContext context.Context, _ any) (any, error) {
			if _, ok := handlerContext.Deadline(); !ok {
				t.Fatal("handler deadline was not propagated")
			}
			if got := CorrelationID(handlerContext); got != "corr-fixture" {
				t.Fatalf("correlation = %q", got)
			}
			return &codefluxv1.TestProviderResponse{}, nil
		})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(logs) != 1 || logs[0].CorrelationID != "corr-fixture" || logs[0].Code != codes.OK {
		t.Fatalf("logs = %+v", logs)
	}
	if strings.Contains(strings.Join([]string{logs[0].Method, logs[0].CorrelationID}, " "), boundaryTestToken) {
		t.Fatal("diagnostic log exposed the session token")
	}
}

func TestBoundaryTrustsOnlyExplicitBridgeAttestation(t *testing.T) {
	boundary, err := NewBoundary(BoundaryOptions{
		SessionToken:         boundaryTestToken,
		TrustBridgeRequestID: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := metadata.NewIncomingContext(t.Context(), metadata.Pairs(RequestIDMetadataKey, "bridge-request-1"))
	request := &codefluxv1.TestProviderRequest{
		ProviderId: fixtureIdentity(codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_PROVIDER, "prv"),
	}
	_, err = boundary.UnaryInterceptor()(ctx, request, &grpc.UnaryServerInfo{FullMethod: "/fixture/Call"},
		func(context.Context, any) (any, error) {
			return &codefluxv1.TestProviderResponse{}, nil
		})
	if err != nil {
		t.Fatal(err)
	}
}

func TestStopGRPCServerHonorsCompletedAndExpiredContexts(t *testing.T) {
	server := grpc.NewServer()
	if err := StopGRPCServer(t.Context(), server); err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	server = grpc.NewServer()
	err := StopGRPCServer(cancelled, server)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Fatalf("expired shutdown = %v", err)
	}
}

func startBoundaryTestServer(
	t *testing.T,
	log func(RequestLog),
) (*grpc.ClientConn, func()) {
	t.Helper()
	boundary, err := NewBoundary(BoundaryOptions{
		SessionToken: boundaryTestToken,
		Log:          log,
	})
	if err != nil {
		t.Fatal(err)
	}
	listener := bufconn.Listen(4 << 20)
	server := grpc.NewServer(
		grpc.ChainUnaryInterceptor(boundary.UnaryInterceptor()),
		grpc.ChainStreamInterceptor(boundary.StreamInterceptor()),
		grpc.UnknownServiceHandler(dynamicProductHandler),
	)
	go func() { _ = server.Serve(listener) }()
	connection, err := grpc.NewClient(
		"passthrough:///bufnet",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
	)
	if err != nil {
		server.Stop()
		t.Fatal(err)
	}
	return connection, func() {
		_ = connection.Close()
		server.Stop()
		_ = listener.Close()
	}
}

func dynamicProductHandler(_ any, stream grpc.ServerStream) error {
	fullMethod, ok := grpc.MethodFromServerStream(stream)
	if !ok {
		return status.Error(codes.Internal, "method identity unavailable")
	}
	method, err := productMethodDescriptor(fullMethod)
	if err != nil {
		return status.Error(codes.Unimplemented, "method is not declared")
	}
	request := dynamicpb.NewMessage(method.Input())
	if err := stream.RecvMsg(request); err != nil {
		return err
	}
	response := dynamicpb.NewMessage(method.Output())
	return stream.SendMsg(response)
}

func productMethodDescriptor(fullMethod string) (protoreflect.MethodDescriptor, error) {
	trimmed := strings.TrimPrefix(fullMethod, "/")
	serviceName, methodName, ok := strings.Cut(trimmed, "/")
	if !ok {
		return nil, errors.New("invalid method path")
	}
	descriptor, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(serviceName))
	if err != nil {
		return nil, err
	}
	service, ok := descriptor.(protoreflect.ServiceDescriptor)
	if !ok {
		return nil, errors.New("descriptor is not a service")
	}
	method := service.Methods().ByName(protoreflect.Name(methodName))
	if method == nil {
		return nil, errors.New("method is not declared")
	}
	return method, nil
}

func populateDynamicMessage(message protoreflect.Message, hint string) {
	fields := message.Descriptor().Fields()
	for index := 0; index < fields.Len(); index++ {
		field := fields.Get(index)
		if field.IsList() {
			if field.Message() != nil &&
				field.Message().FullName() == "codeflux.v1.StableIdentity" {
				child := dynamicpb.NewMessage(field.Message())
				populateIdentity(child.ProtoReflect(), string(field.Name()))
				message.Mutable(field).List().Append(protoreflect.ValueOfMessage(child.ProtoReflect()))
			} else if field.Kind() == protoreflect.StringKind &&
				field.Name() == "attachment_paths" {
				message.Mutable(field).List().Append(protoreflect.ValueOfString("file.go"))
			}
			continue
		}
		if field.Message() != nil {
			child := dynamicpb.NewMessage(field.Message())
			if field.Message().FullName() == "codeflux.v1.StableIdentity" {
				populateIdentity(child.ProtoReflect(), string(field.Name()))
			} else {
				populateDynamicMessage(child.ProtoReflect(), string(field.Name()))
			}
			message.Set(field, protoreflect.ValueOfMessage(child.ProtoReflect()))
			continue
		}
		switch field.Kind() {
		case protoreflect.StringKind:
			message.Set(field, protoreflect.ValueOfString(dynamicStringValue(field.Name(), hint)))
		case protoreflect.BoolKind:
			message.Set(field, protoreflect.ValueOfBool(false))
		case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
			message.Set(field, protoreflect.ValueOfUint32(dynamicUint32Value(field.Name())))
		case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
			message.Set(field, protoreflect.ValueOfUint64(1))
		case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
			message.Set(field, protoreflect.ValueOfInt32(1))
		case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
			message.Set(field, protoreflect.ValueOfInt64(1))
		case protoreflect.EnumKind:
			if field.Enum().Values().Len() > 1 {
				message.Set(field, protoreflect.ValueOfEnum(field.Enum().Values().Get(1).Number()))
			}
		case protoreflect.BytesKind:
			message.Set(field, protoreflect.ValueOfBytes([]byte("fixture")))
		}
	}
}

func populateIdentity(message protoreflect.Message, hint string) {
	kind, prefix := identityKindForHint(hint)
	message.Set(
		message.Descriptor().Fields().ByName("kind"),
		protoreflect.ValueOfEnum(protoreflect.EnumNumber(kind)),
	)
	message.Set(
		message.Descriptor().Fields().ByName("value"),
		protoreflect.ValueOfString(prefix+"_"+boundaryTestUUID),
	)
}

func identityKindForHint(hint string) (codefluxv1.StableIdentityKind, string) {
	switch {
	case strings.Contains(hint, "workspace"):
		return codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_WORKSPACE, "wsp"
	case strings.Contains(hint, "repository"):
		return codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_REPOSITORY, "repo"
	case strings.Contains(hint, "thread"):
		return codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_THREAD, "thr"
	case strings.Contains(hint, "message"):
		return codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_MESSAGE, "msg"
	case strings.Contains(hint, "session"):
		return codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_SESSION, "ses"
	case strings.Contains(hint, "task"):
		return codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_TASK, "tsk"
	case strings.Contains(hint, "approval"):
		return codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_APPROVAL, "apr"
	case strings.Contains(hint, "checkpoint"):
		return codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_CHECKPOINT, "ckp"
	case strings.Contains(hint, "graph_revision"):
		return codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_GRAPH_REVISION, "grv"
	case strings.Contains(hint, "graph"):
		return codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_GRAPH, "grf"
	case strings.Contains(hint, "node"):
		return codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_NODE, "nod"
	case strings.Contains(hint, "edge"):
		return codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_EDGE, "edg"
	case strings.Contains(hint, "validation"):
		return codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_VALIDATION, "val"
	case strings.Contains(hint, "evidence"):
		return codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_EVIDENCE, "evd"
	case strings.Contains(hint, "provider"):
		return codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_PROVIDER, "prv"
	case strings.Contains(hint, "budget"):
		return codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_BUDGET, "bdg"
	default:
		return codefluxv1.StableIdentityKind_STABLE_IDENTITY_KIND_ARTIFACT, "art"
	}
}

func dynamicStringValue(name protoreflect.Name, hint string) string {
	switch name {
	case "idempotency_key":
		return "idem-" + strings.ToLower(hint)
	case "currency_code":
		return "USD"
	case "selected_path":
		return "C:/synthetic/repository"
	case "workspace_relative_path":
		return "file.go"
	case "expected_diff_identity":
		return "diff-fixture"
	case "mode":
		return "program"
	case "continuation_cursor", "cursor":
		return ""
	default:
		return "fixture"
	}
}

func dynamicUint32Value(name protoreflect.Name) uint32 {
	switch name {
	case "max_nodes", "max_changes":
		return 300
	case "max_edges":
		return 600
	case "decimal_places":
		return 2
	default:
		return 1
	}
}

func authenticatedTestContext(ctx context.Context) context.Context {
	values := metadata.Pairs(
		SessionMetadataKey, boundaryTestToken,
		CorrelationMetadataKey, "corr-test",
	)
	ctx = metadata.NewOutgoingContext(ctx, values)
	return metadata.NewIncomingContext(ctx, values)
}

func fixtureIdentity(
	kind codefluxv1.StableIdentityKind,
	prefix string,
) *codefluxv1.StableIdentity {
	return &codefluxv1.StableIdentity{Kind: kind, Value: prefix + "_" + boundaryTestUUID}
}

func uint64Pointer(value uint64) *uint64 {
	return &value
}

func apiErrorDetail(t *testing.T, err error) *codefluxv1.ApiErrorDetail {
	t.Helper()
	statusValue, ok := status.FromError(err)
	if !ok {
		t.Fatalf("error is not a status: %v", err)
	}
	for _, detail := range statusValue.Details() {
		if typed, ok := detail.(*codefluxv1.ApiErrorDetail); ok {
			return typed
		}
	}
	t.Fatalf("status lacks ApiErrorDetail: %v", err)
	return nil
}
