package providers

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"codeflux.dev/codeflux/internal/domain"
)

func TestValidateModelRequestBindsIdentityIdempotencyAndCapabilities(
	t *testing.T,
) {
	request, model, capabilities := validNormalizedRequest(t)
	if err := ValidateModelRequest(request, model, capabilities, true); err != nil {
		t.Fatal(err)
	}

	mismatch := request
	mismatch.Idempotency.Key = "different"
	if err := ValidateModelRequest(
		mismatch, model, capabilities, true,
	); !errors.Is(err, ErrInvalidModelRequest) {
		t.Fatalf("idempotency mismatch error = %v", err)
	}

	unsupported := request
	if err := ValidateModelRequest(
		unsupported, model, capabilities, false,
	); !errors.Is(err, ErrInvalidModelRequest) {
		t.Fatalf("unsupported idempotency error = %v", err)
	}

	switched := model
	switched.Revision = "different"
	if err := ValidateModelRequest(
		request, switched, capabilities, true,
	); !errors.Is(err, ErrInvalidModelRequest) {
		t.Fatalf("identity switch error = %v", err)
	}
}

func TestValidateModelRequestRejectsUnboundedAndUnauthorizedInput(t *testing.T) {
	request, model, capabilities := validNormalizedRequest(t)
	request.Messages[0].Content = []ContentPart{{
		Kind: ContentKindImage,
		Image: &ImageInput{
			URL: "https://private.example.test/image.png",
		},
	}}
	if err := ValidateModelRequest(
		request, model, capabilities, true,
	); !errors.Is(err, ErrRemoteImageAuthorityRequired) {
		t.Fatalf("remote image error = %v", err)
	}

	request, model, capabilities = validNormalizedRequest(t)
	request.Temperature = new(float64)
	*request.Temperature = math.NaN()
	if err := ValidateModelRequest(
		request, model, capabilities, true,
	); !errors.Is(err, ErrInvalidModelRequest) {
		t.Fatalf("NaN temperature error = %v", err)
	}

	request, model, capabilities = validNormalizedRequest(t)
	request.Messages[0].Content[0].Text = strings.Repeat(
		"x",
		maximumProviderTextBytes+1,
	)
	if err := ValidateModelRequest(
		request, model, capabilities, true,
	); !errors.Is(err, ErrInvalidModelRequest) {
		t.Fatalf("unbounded text error = %v", err)
	}
}

func TestValidateRequestIdentityRequiresStorageCompatibleHashAndLogicalKey(
	t *testing.T,
) {
	request, _, _ := validNormalizedRequest(t)
	identity := request.Identity
	identity.RequestHash = "sha256:not-a-digest"
	if err := ValidateRequestIdentity(identity); !errors.Is(
		err,
		ErrInvalidRequestIdentity,
	) {
		t.Fatalf("invalid hash error = %v", err)
	}
	identity = request.Identity
	identity.IdempotencyKey = ""
	if err := ValidateRequestIdentity(identity); !errors.Is(
		err,
		ErrInvalidRequestIdentity,
	) {
		t.Fatalf("missing logical idempotency error = %v", err)
	}
}

func validNormalizedRequest(
	t *testing.T,
) (ModelRequest, ModelIdentity, ModelCapabilities) {
	t.Helper()
	requestID, err := domain.NewModelRequestID()
	if err != nil {
		t.Fatal(err)
	}
	provider := ProviderIdentity{
		Adapter: "fixture", AdapterVersion: "1",
		Provider: "fixture", ProviderVersion: "2026-07-30",
	}
	model := ModelIdentity{
		Provider: provider, Model: "fixture-model", Revision: "revision-1",
	}
	idempotency := RequestIdempotency{
		ProviderSupported: true,
		Key:               "logical-request-key",
		ProviderScope:     "fixture-scope",
	}
	capabilities := ModelCapabilities{
		Tools: true, StructuredOutput: true, ImageInput: true,
		MaximumOutputTokens: 4096, ReasoningControls: []string{"low"},
	}
	return ModelRequest{
		Identity: RequestIdentity{
			ModelRequestID: requestID,
			Provider:       provider,
			Model:          model,
			Idempotency:    idempotency,
			IdempotencyKey: idempotency.Key,
			RequestHash:    strings.Repeat("c", 64),
		},
		Messages: []Message{{
			Role: MessageRoleUser,
			Content: []ContentPart{{
				Kind: ContentKindText, Text: "hello",
			}},
		}},
		Tools: []ToolDeclaration{{
			Name: "lookup",
			InputSchema: json.RawMessage(
				`{"type":"object","properties":{"id":{"type":"integer"}}}`,
			),
		}},
		StructuredOutput: &StructuredOutputRequirement{
			Name: "answer", Strict: true,
			Schema: json.RawMessage(`{"type":"object"}`),
		},
		MaximumTokens:   256,
		ReasoningEffort: "low",
		Idempotency:     idempotency,
		Deadline:        time.Now().Add(time.Minute),
	}, model, capabilities
}
