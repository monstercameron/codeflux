package executor

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
)

// PluginProtocol is the only supported out-of-process extension transport.
type PluginProtocol string

const (
	PluginProtocolJSONRPC PluginProtocol = "json-rpc-2.0"
	PluginProtocolMCP     PluginProtocol = "mcp-stdio"
)

// PluginBoundary declares a reviewable subprocess contract. It is data only:
// the coordinator never loads plugin code into its process.
type PluginBoundary struct {
	ID                  string
	Version             string
	Protocol            PluginProtocol
	Executable          string
	Arguments           []string
	Tools               []ToolName
	ReadScopes          []string
	WriteScopes         []string
	NetworkDestinations []string
	SecretReferences    []string
	ExpectedSideEffects []SideEffect
}

// ValidatePluginBoundary rejects ambient or in-process authority.
func ValidatePluginBoundary(boundary PluginBoundary) error {
	if strings.TrimSpace(boundary.ID) == "" || strings.TrimSpace(boundary.Version) == "" {
		return errors.New("plugin identity and version are required")
	}
	if boundary.Protocol != PluginProtocolJSONRPC && boundary.Protocol != PluginProtocolMCP {
		return errors.New("plugin protocol must be JSON-RPC 2.0 or MCP over stdio")
	}
	if strings.TrimSpace(boundary.Executable) == "" {
		return errors.New("plugin subprocess executable is required")
	}
	if filepath.Base(boundary.Executable) == "." {
		return errors.New("plugin executable is invalid")
	}
	if len(boundary.Tools) == 0 {
		return errors.New("plugin must declare at least one tool")
	}
	for _, tool := range boundary.Tools {
		if tool != ToolPluginRPC {
			return errors.New("plugin may expose only the mediated plugin-rpc tool")
		}
	}
	for _, effect := range boundary.ExpectedSideEffects {
		if !slices.Contains(allSideEffects(), effect) {
			return errors.New("plugin declares an unknown side effect")
		}
	}
	for _, scope := range append(
		append([]string(nil), boundary.ReadScopes...),
		boundary.WriteScopes...,
	) {
		if !filepath.IsAbs(scope) {
			return errors.New("plugin filesystem scopes must be absolute")
		}
	}
	for _, reference := range boundary.SecretReferences {
		if !strings.HasPrefix(reference, "os://") || strings.TrimSpace(reference) == "os://" {
			return errors.New("plugin secrets must be opaque operating-system references")
		}
	}
	for _, destination := range boundary.NetworkDestinations {
		if strings.TrimSpace(destination) == "" {
			return errors.New("plugin network destinations must be explicit")
		}
	}
	return nil
}

func allSideEffects() []SideEffect {
	return []SideEffect{
		EffectRepositoryRead,
		EffectTaskWorktreeWrite,
		EffectNetwork,
		EffectDependencyInstall,
		EffectExternalWrite,
		EffectCredentialAccess,
		EffectDestructive,
		EffectPrivileged,
		EffectExternalCommunication,
		EffectSubprocess,
	}
}
