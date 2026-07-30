package coordinator

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	codefluxv1 "codeflux.dev/codeflux/api/gen/codeflux/v1"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestRPCContractCatalogMatchesProductDescriptor(t *testing.T) {
	t.Parallel()

	descriptorMethods := productDescriptorMethods(t)
	descriptorTypes := productDescriptorTypes()
	contracts := RPCContracts()
	if len(contracts) != len(descriptorMethods) {
		t.Fatalf("catalog has %d RPCs; descriptor has %d", len(contracts), len(descriptorMethods))
	}

	catalogMethods := make([]string, 0, len(contracts))
	seen := make(map[string]struct{}, len(contracts))
	for _, contract := range contracts {
		if err := contract.Validate(); err != nil {
			t.Errorf("invalid %s: %v", contract.FullMethod, err)
		}
		if _, exists := seen[contract.FullMethod]; exists {
			t.Errorf("duplicate RPC catalog row %s", contract.FullMethod)
		}
		seen[contract.FullMethod] = struct{}{}
		catalogMethods = append(catalogMethods, contract.FullMethod)
		wantTypes := descriptorTypes[contract.FullMethod]
		if contract.CommandOrQueryType != wantTypes[0] || contract.ResultType != wantTypes[1] {
			t.Errorf(
				"%s types = %s -> %s; want %s -> %s",
				contract.FullMethod,
				contract.CommandOrQueryType,
				contract.ResultType,
				wantTypes[0],
				wantTypes[1],
			)
		}
	}
	sort.Strings(catalogMethods)
	if !reflect.DeepEqual(catalogMethods, descriptorMethods) {
		t.Fatalf("catalog methods do not match descriptor\ncatalog: %v\ndescriptor: %v", catalogMethods, descriptorMethods)
	}
}

func TestEveryRPCMutationHasRetryAndRevisionSemantics(t *testing.T) {
	t.Parallel()

	byMethod := make(map[string]RPCContract, len(rpcContracts))
	for _, contract := range RPCContracts() {
		byMethod[contract.FullMethod] = contract
	}

	services := codefluxv1.File_codeflux_v1_product_api_proto.Services()
	for serviceIndex := 0; serviceIndex < services.Len(); serviceIndex++ {
		service := services.Get(serviceIndex)
		methods := service.Methods()
		for methodIndex := 0; methodIndex < methods.Len(); methodIndex++ {
			method := methods.Get(methodIndex)
			if method.Input().Fields().ByName("control") == nil {
				continue
			}
			fullMethod := "/" + string(service.FullName()) + "/" + string(method.Name())
			contract, exists := byMethod[fullMethod]
			if !exists {
				t.Errorf("%s has MutationControl but no catalog row", fullMethod)
				continue
			}
			if contract.Function.Kind != OperationMutation && contract.Function.Kind != OperationEffect {
				t.Errorf("%s has MutationControl but kind %q", fullMethod, contract.Function.Kind)
			}
			for name, value := range map[string]string{
				"idempotency": contract.Function.IdempotencyScope,
				"duplicate":   contract.Function.DuplicateBehavior,
				"revision":    contract.Function.ExpectedRevision,
				"transaction": contract.Function.Transaction,
			} {
				if value == "" || strings.Contains(value, "not-required") {
					t.Errorf("%s lacks explicit %s semantics: %q", fullMethod, name, value)
				}
			}
		}
	}
}

func TestBackendFunctionCatalogCoversPlanSurface(t *testing.T) {
	t.Parallel()

	requiredCategories := []string{
		"budget", "credentials", "diagnostics", "events", "graph", "lifecycle",
		"memory", "provider", "review", "settings", "task", "thread", "tool",
		"worker", "workspace", "worktree",
	}
	requiredFunctions := []string{
		"Application.Start",
		"EventJournal.Append",
		"WorkspaceService.SelectContext",
		"ThreadService.AppendUserMessage",
		"TaskService.BuildPlan",
		"WorktreeService.ApplyEditBatch",
		"ProviderService.ExecuteModelRequest",
		"ResolveModelPrice",
		"BudgetService.Reserve",
		"ToolService.RequestToolExecution",
		"BoundToolOutput",
		"WorkerManager.Spawn",
		"ReviewService.AcceptChange",
		"CommitGraphRevision",
		"MemoryService.RetrieveBeforeWork",
		"CredentialStore.Put",
		"SettingsService.UpdateUserSettings",
		"DiagnosticsService.BuildDiagnosticExport",
	}

	categories := make(map[string]struct{})
	functions := make(map[string]FunctionContract)
	for _, contract := range BackendFunctionContracts() {
		if err := contract.Validate(); err != nil {
			t.Errorf("invalid %s: %v", contract.Name, err)
		}
		if _, exists := functions[contract.Name]; exists {
			t.Errorf("duplicate backend function %s", contract.Name)
		}
		functions[contract.Name] = contract
		categories[contract.Category] = struct{}{}

		if contract.Kind == OperationMutation || contract.Kind == OperationEffect {
			assertExplicitMutationContract(t, contract)
		}
	}

	for _, category := range requiredCategories {
		if _, exists := categories[category]; !exists {
			t.Errorf("missing plan category %s", category)
		}
	}
	for _, name := range requiredFunctions {
		if _, exists := functions[name]; !exists {
			t.Errorf("missing plan function %s", name)
		}
	}
}

func TestCatalogUsesStableSafeErrorVocabulary(t *testing.T) {
	t.Parallel()

	expected := append([]string(nil), stableErrors...)
	sort.Strings(expected)
	for _, contract := range BackendFunctionContracts() {
		actual := append([]string(nil), contract.Errors...)
		sort.Strings(actual)
		if !reflect.DeepEqual(actual, expected) {
			t.Errorf("%s error mapping = %v; want %v", contract.Name, actual, expected)
		}
	}
}

func TestCatalogReturnsDefensiveCopies(t *testing.T) {
	t.Parallel()

	rpcs := RPCContracts()
	functions := BackendFunctionContracts()
	rpcs[0].HandlerSteps[0] = "domain-policy"
	rpcs[0].Function.Errors[0] = "secret"
	functions[0].Repositories = append(functions[0].Repositories, "secret-store")
	functions[0].Errors[0] = "secret"

	freshRPCs := RPCContracts()
	freshFunctions := BackendFunctionContracts()
	if freshRPCs[0].HandlerSteps[0] != thinHandlerSteps[0] ||
		freshRPCs[0].Function.Errors[0] != stableErrors[0] ||
		freshFunctions[0].Errors[0] != stableErrors[0] {
		t.Fatal("catalog mutation escaped a defensive copy")
	}
	for _, repository := range freshFunctions[0].Repositories {
		if repository == "secret-store" {
			t.Fatal("repository slice mutation escaped a defensive copy")
		}
	}
}

func assertExplicitMutationContract(t *testing.T, contract FunctionContract) {
	t.Helper()

	for name, value := range map[string]string{
		"authority":          contract.Authority,
		"idempotency":        contract.IdempotencyScope,
		"duplicate behavior": contract.DuplicateBehavior,
		"revision":           contract.ExpectedRevision,
		"transaction":        contract.Transaction,
		"external effect":    contract.ExternalEffect,
		"cancellation":       contract.Cancellation,
	} {
		if strings.TrimSpace(value) == "" {
			t.Errorf("%s lacks %s", contract.Name, name)
		}
	}
	if contract.Kind == OperationEffect {
		if contract.ExternalEffect == "none" {
			t.Errorf("%s is an effect without an effect identity", contract.Name)
		}
		if !strings.Contains(contract.Transaction, "intent") ||
			!strings.Contains(contract.DuplicateBehavior, "never repeat an ambiguous effect") {
			t.Errorf("%s lacks durable intent/ambiguity behavior", contract.Name)
		}
		for name, value := range map[string]string{
			"intent":    contract.EffectIntent,
			"outcome":   contract.EffectOutcome,
			"ambiguity": contract.Ambiguity,
			"retry":     contract.Retry,
		} {
			if value == "" || value == "none" {
				t.Errorf("%s lacks external-effect %s semantics", contract.Name, name)
			}
		}
	}
	if len(contract.Repositories) == 0 || len(contract.DurableEvents) == 0 {
		t.Errorf("%s does not explicitly record repositories/events", contract.Name)
	}
}

func productDescriptorMethods(t *testing.T) []string {
	t.Helper()

	var methods []string
	services := codefluxv1.File_codeflux_v1_product_api_proto.Services()
	for serviceIndex := 0; serviceIndex < services.Len(); serviceIndex++ {
		service := services.Get(serviceIndex)
		for methodIndex := 0; methodIndex < service.Methods().Len(); methodIndex++ {
			method := service.Methods().Get(methodIndex)
			methods = append(methods, fullMethodName(service, method))
		}
	}
	sort.Strings(methods)
	return methods
}

func fullMethodName(service protoreflect.ServiceDescriptor, method protoreflect.MethodDescriptor) string {
	return "/" + string(service.FullName()) + "/" + string(method.Name())
}

func productDescriptorTypes() map[string][2]string {
	result := make(map[string][2]string)
	services := codefluxv1.File_codeflux_v1_product_api_proto.Services()
	for serviceIndex := 0; serviceIndex < services.Len(); serviceIndex++ {
		service := services.Get(serviceIndex)
		for methodIndex := 0; methodIndex < service.Methods().Len(); methodIndex++ {
			method := service.Methods().Get(methodIndex)
			result[fullMethodName(service, method)] = [2]string{
				string(method.Input().FullName()),
				string(method.Output().FullName()),
			}
		}
	}
	return result
}
