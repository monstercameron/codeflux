package coordinator

import (
	"context"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"codeflux.dev/codeflux/internal/transport"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/types/dynamicpb"
)

// TestAUDIT008_EveryDeclaredProductMethodReachesARegisteredService covers
// AUDIT-008, reconciling M07-057.
//
// M07-057 recorded in-process API tests for every method. The test behind it
// registers no service at all: it installs grpc.UnknownServiceHandler with a
// handler that echoes an empty response for any declared method. Every method
// therefore "passed" without a single production implementation being reached,
// which proves the descriptors exist and the interceptors run and nothing
// about the API.
//
// This drives the same enumeration against the real application's task server,
// where no unknown-service handler is installed. A method that is not
// registered comes back Unimplemented, and Unimplemented is the one answer
// this test refuses.
func TestAUDIT008_EveryDeclaredProductMethodReachesARegisteredService(t *testing.T) {
	root := t.TempDir()
	application, err := StartApplication(t.Context(), ApplicationOptions{
		DatabasePath:    filepath.Join(root, "codeflux.sqlite3"),
		BackupDirectory: filepath.Join(root, "backups"),
		ListenAddress:   "127.0.0.1:0", TaskListenAddress: "127.0.0.1:0",
		TaskControls: &applicationTaskControlStub{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Shutdown(context.Background()) })

	connection, err := grpc.NewClient(
		application.TaskControlAddress(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	ctx := metadata.AppendToOutgoingContext(
		t.Context(), transport.SessionMetadataKey, application.BrowserSessionSecret(),
	)

	product := codefluxv1.File_codeflux_v1_product_api_proto
	var unimplemented []string
	checked := 0

	for serviceIndex := 0; serviceIndex < product.Services().Len(); serviceIndex++ {
		service := product.Services().Get(serviceIndex)
		for methodIndex := 0; methodIndex < service.Methods().Len(); methodIndex++ {
			method := service.Methods().Get(methodIndex)
			fullMethod := "/" + string(service.FullName()) + "/" + string(method.Name())
			checked++

			err := invokeDeclaredMethod(ctx, connection, fullMethod, method)
			// Any answer other than Unimplemented means a registered service
			// handled the call. An empty request will usually be refused as
			// InvalidArgument or NotFound, and that refusal is the evidence:
			// it came from a real implementation applying real validation.
			if status.Code(err) == codes.Unimplemented {
				unimplemented = append(unimplemented, fullMethod)
			}
		}
	}

	// A floor rather than an exact count: the API is expected to grow, but a
	// sudden collapse in what was enumerated would otherwise pass silently and
	// is indistinguishable from the vacuous test this one replaces.
	const declaredMethodFloor = 50
	if checked < declaredMethodFloor {
		t.Fatalf("only %d product methods were enumerated, want at least %d; "+
			"the enumeration has broken rather than the API having shrunk this far",
			checked, declaredMethodFloor)
	}
	sort.Strings(unimplemented)
	if len(unimplemented) > 0 {
		t.Fatalf("%d of %d declared methods reach no registered service:\n  %s",
			len(unimplemented), checked, strings.Join(unimplemented, "\n  "))
	}
}

// TestAUDIT008_AnUnauthenticatedCallIsRefusedBeforeAnyService proves the
// boundary is in front of the registered services rather than beside them.
func TestAUDIT008_AnUnauthenticatedCallIsRefusedBeforeAnyService(t *testing.T) {
	root := t.TempDir()
	application, err := StartApplication(t.Context(), ApplicationOptions{
		DatabasePath:    filepath.Join(root, "codeflux.sqlite3"),
		BackupDirectory: filepath.Join(root, "backups"),
		ListenAddress:   "127.0.0.1:0", TaskListenAddress: "127.0.0.1:0",
		TaskControls: &applicationTaskControlStub{},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = application.Shutdown(context.Background()) })

	connection, err := grpc.NewClient(
		application.TaskControlAddress(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })

	product := codefluxv1.File_codeflux_v1_product_api_proto
	refused := 0
	for serviceIndex := 0; serviceIndex < product.Services().Len(); serviceIndex++ {
		service := product.Services().Get(serviceIndex)
		for methodIndex := 0; methodIndex < service.Methods().Len(); methodIndex++ {
			method := service.Methods().Get(methodIndex)
			fullMethod := "/" + string(service.FullName()) + "/" + string(method.Name())
			err := invokeDeclaredMethod(t.Context(), connection, fullMethod, method)
			if status.Code(err) != codes.Unauthenticated {
				t.Errorf("%s answered %v without a session, want Unauthenticated",
					fullMethod, status.Code(err))
				continue
			}
			refused++
		}
	}
	if refused == 0 {
		t.Fatal("no method was exercised; the refusal assertion is vacuous")
	}
}

// invokeDeclaredMethod calls one declared method with an empty request of its
// own type, handling the streaming and unary shapes alike.
func invokeDeclaredMethod(
	ctx context.Context,
	connection *grpc.ClientConn,
	fullMethod string,
	method protoreflect.MethodDescriptor,
) error {
	request := dynamicpb.NewMessage(method.Input())
	response := dynamicpb.NewMessage(method.Output())

	if method.IsStreamingServer() {
		stream, err := connection.NewStream(
			ctx, &grpc.StreamDesc{ServerStreams: true}, fullMethod)
		if err != nil {
			return err
		}
		if err := stream.SendMsg(request); err != nil {
			return err
		}
		if err := stream.CloseSend(); err != nil {
			return err
		}
		return stream.RecvMsg(response)
	}
	return connection.Invoke(ctx, fullMethod, request, response)
}
