package executor

import (
	"path/filepath"
	"testing"
)

func TestPluginBoundaryRequiresOutOfProcessExplicitAuthority(t *testing.T) {
	root := t.TempDir()
	valid := PluginBoundary{
		ID: "formatter", Version: "1.2.3",
		Protocol:            PluginProtocolJSONRPC,
		Executable:          "formatter-plugin",
		Tools:               []ToolName{ToolPluginRPC},
		ReadScopes:          []string{root},
		WriteScopes:         []string{filepath.Join(root, "generated")},
		SecretReferences:    []string{"os://codeflux/plugin/formatter"},
		ExpectedSideEffects: []SideEffect{EffectTaskWorktreeWrite},
	}
	if err := ValidatePluginBoundary(valid); err != nil {
		t.Fatal(err)
	}
	invalid := valid
	invalid.ReadScopes = []string{"."}
	if err := ValidatePluginBoundary(invalid); err == nil {
		t.Fatal("relative ambient filesystem scope was accepted")
	}
	invalid = valid
	invalid.SecretReferences = []string{"raw-secret"}
	if err := ValidatePluginBoundary(invalid); err == nil {
		t.Fatal("raw plugin secret was accepted")
	}
	invalid = valid
	invalid.Tools = []ToolName{ToolRunCommand}
	if err := ValidatePluginBoundary(invalid); err == nil {
		t.Fatal("unmediated plugin tool was accepted")
	}
}
