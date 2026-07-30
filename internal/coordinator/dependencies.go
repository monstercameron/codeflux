package coordinator

import (
	"errors"

	"codeflux.dev/codeflux/internal/credentials"
	"codeflux.dev/codeflux/internal/storage"
	"codeflux.dev/codeflux/internal/workspace"
)

// ProviderDependencies keep credential access and non-secret reference
// persistence inside the coordinator until M12 adapters consume these ports.
type ProviderDependencies struct {
	Credentials credentials.Store
	References  storage.ProviderCredentialOperations
}

func newProviderDependencies(
	credentialStore credentials.Store,
	references storage.ProviderCredentialOperations,
) (ProviderDependencies, error) {
	if credentialStore == nil || references == nil {
		return ProviderDependencies{}, errors.New("provider credential dependencies are required")
	}
	return ProviderDependencies{
		Credentials: credentialStore,
		References:  references,
	}, nil
}

// WorkspaceDependencies keep bounded repository discovery and durable
// worktree identity at the coordinator boundary.
type WorkspaceDependencies struct {
	Discovery workspace.CommandRunner
	Bindings  storage.WorktreeBindingOperations
}

func newWorkspaceDependencies(
	bindings storage.WorktreeBindingOperations,
) (WorkspaceDependencies, error) {
	if bindings == nil {
		return WorkspaceDependencies{}, errors.New("workspace binding repository is required")
	}
	return WorkspaceDependencies{
		Discovery: workspace.ExecRunner{},
		Bindings:  bindings,
	}, nil
}
