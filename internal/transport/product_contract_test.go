package transport

import (
	"strings"
	"testing"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
)

func TestProductServiceMethodSurface(t *testing.T) {
	expected := map[protoreflect.FullName][]protoreflect.Name{
		"codeflux.v1.WorkspaceService": {
			"OpenWorkspace", "GetWorkspaceState", "ListRepositories", "InspectRepository",
		},
		"codeflux.v1.ThreadService": {
			"CreateThread", "ListThreads", "GetThreadPage", "SendMessage", "RenameThread", "ArchiveThread",
		},
		"codeflux.v1.TaskService": {
			"CreateTask", "GetTask", "StartTask", "PauseTask", "ResumeTask", "CancelTask",
			"ApproveAction", "SetBudget", "RequestRepair", "RollbackTask",
		},
		"codeflux.v1.GraphService": {
			"GetGraphSlice", "ExpandGraph", "GetNode", "ExplainNode", "CompareGraphRevisions",
		},
		"codeflux.v1.ReviewService": {
			"GetDiffSummary", "GetValidationReport", "AcceptChange", "RejectChange", "OpenInEditor",
		},
		"codeflux.v1.SettingsService": {
			"GetModels", "GetPolicy", "SetPolicy", "SetBudgetDefaults", "ConfigureProvider", "TestProvider",
		},
		"codeflux.v1.SessionService": {"SubscribeSession"},
	}

	for serviceName, methodNames := range expected {
		descriptor, err := protoregistry.GlobalFiles.FindDescriptorByName(serviceName)
		if err != nil {
			t.Fatalf("find %s: %v", serviceName, err)
		}
		service, ok := descriptor.(protoreflect.ServiceDescriptor)
		if !ok {
			t.Fatalf("%s descriptor type = %T", serviceName, descriptor)
		}
		if service.Methods().Len() != len(methodNames) {
			t.Fatalf("%s method count = %d, want %d", serviceName, service.Methods().Len(), len(methodNames))
		}
		for _, methodName := range methodNames {
			if service.Methods().ByName(methodName) == nil {
				t.Errorf("%s lacks %s", serviceName, methodName)
			}
		}
	}
}

func TestEveryMutationCarriesStandardControl(t *testing.T) {
	mutations := []protoreflect.FullName{
		"codeflux.v1.OpenWorkspaceRequest",
		"codeflux.v1.CreateThreadRequest",
		"codeflux.v1.SendMessageRequest",
		"codeflux.v1.RenameThreadRequest",
		"codeflux.v1.ArchiveThreadRequest",
		"codeflux.v1.CreateTaskRequest",
		"codeflux.v1.StartTaskRequest",
		"codeflux.v1.PauseTaskRequest",
		"codeflux.v1.ResumeTaskRequest",
		"codeflux.v1.CancelTaskRequest",
		"codeflux.v1.ApproveActionRequest",
		"codeflux.v1.SetBudgetRequest",
		"codeflux.v1.RequestRepairRequest",
		"codeflux.v1.RollbackTaskRequest",
		"codeflux.v1.AcceptChangeRequest",
		"codeflux.v1.RejectChangeRequest",
		"codeflux.v1.OpenInEditorRequest",
		"codeflux.v1.SetPolicyRequest",
		"codeflux.v1.SetBudgetDefaultsRequest",
		"codeflux.v1.ConfigureProviderRequest",
	}

	for _, messageName := range mutations {
		descriptor, err := protoregistry.GlobalFiles.FindDescriptorByName(messageName)
		if err != nil {
			t.Fatalf("find %s: %v", messageName, err)
		}
		message := descriptor.(protoreflect.MessageDescriptor)
		control := message.Fields().ByName("control")
		if control == nil || control.Message() == nil ||
			control.Message().FullName() != "codeflux.v1.MutationControl" {
			t.Errorf("%s control = %v, want MutationControl", messageName, control)
		}
	}

	control := (&codefluxv1.MutationControl{}).ProtoReflect().Descriptor()
	if control.Fields().ByName("idempotency_key") == nil {
		t.Fatal("MutationControl lacks idempotency_key")
	}
	revision := control.Fields().ByName("expected_revision")
	if revision == nil || !revision.HasOptionalKeyword() {
		t.Fatal("MutationControl expected_revision must be explicitly optional")
	}
}

func TestProductMessagesExposeNoStorageOrCredentialValues(t *testing.T) {
	forbidden := []string{
		"api_key",
		"password",
		"raw_credential",
		"secret_value",
		"sqlite",
		"table_name",
		"row_id",
		"sql_query",
	}
	files := []protoreflect.FileDescriptor{
		codefluxv1.File_codeflux_v1_conventions_proto,
		codefluxv1.File_codeflux_v1_resources_proto,
		codefluxv1.File_codeflux_v1_product_api_proto,
	}
	for _, file := range files {
		inspectMessages(t, file.Messages(), forbidden)
	}

	provider := (&codefluxv1.ConfigureProviderRequest{}).ProtoReflect().Descriptor()
	if provider.Fields().ByName("credential_reference") == nil {
		t.Fatal("ConfigureProvider must carry only an opaque credential_reference")
	}
}

func TestGeneratedClientBindingsExistForEveryProductService(t *testing.T) {
	if codefluxv1.NewWorkspaceServiceClient(nil) == nil ||
		codefluxv1.NewThreadServiceClient(nil) == nil ||
		codefluxv1.NewTaskServiceClient(nil) == nil ||
		codefluxv1.NewGraphServiceClient(nil) == nil ||
		codefluxv1.NewReviewServiceClient(nil) == nil ||
		codefluxv1.NewSettingsServiceClient(nil) == nil ||
		codefluxv1.NewSessionServiceClient(nil) == nil {
		t.Fatal("generated client constructor returned nil")
	}
}

func TestSessionServiceIsTheOnlyProductStreamingMethod(t *testing.T) {
	product := codefluxv1.File_codeflux_v1_product_api_proto
	for serviceIndex := 0; serviceIndex < product.Services().Len(); serviceIndex++ {
		service := product.Services().Get(serviceIndex)
		for methodIndex := 0; methodIndex < service.Methods().Len(); methodIndex++ {
			method := service.Methods().Get(methodIndex)
			wantStream := service.Name() == "SessionService" && method.Name() == "SubscribeSession"
			if method.IsStreamingClient() {
				t.Errorf("%s.%s unexpectedly enables client streaming", service.Name(), method.Name())
			}
			if method.IsStreamingServer() != wantStream {
				t.Errorf("%s.%s server streaming = %t, want %t", service.Name(), method.Name(), method.IsStreamingServer(), wantStream)
			}
		}
	}
}

func inspectMessages(
	t *testing.T,
	messages protoreflect.MessageDescriptors,
	forbidden []string,
) {
	t.Helper()
	for messageIndex := 0; messageIndex < messages.Len(); messageIndex++ {
		message := messages.Get(messageIndex)
		for fieldIndex := 0; fieldIndex < message.Fields().Len(); fieldIndex++ {
			field := message.Fields().Get(fieldIndex)
			name := strings.ToLower(string(field.Name()))
			for _, fragment := range forbidden {
				if strings.Contains(name, fragment) {
					t.Errorf("%s exposes forbidden field %q", message.FullName(), field.Name())
				}
			}
		}
		inspectMessages(t, message.Messages(), forbidden)
	}
}
