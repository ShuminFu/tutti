package runtimeprep

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	runtimeInstructionsFileEnv     = "TUTTI_RUNTIME_INSTRUCTIONS_FILE"
	runtimeInstructionsFileName    = "runtime-instructions.md"
	runtimeInstructionsCwdWriteEnv = "TUTTI_RUNTIME_INSTRUCTIONS_CWD_WRITE"
)

func runtimeInstructionsCwdWriteDisabled() bool {
	return os.Getenv(runtimeInstructionsCwdWriteEnv) == "off"
}

func writeSessionRuntimeInstructions(input PrepareInput, runtimeRoot string, manifest *Manifest) (string, error) {
	if strings.TrimSpace(input.Provider) == "claude-code" {
		return "", nil
	}
	policy, err := tuttiCLIPolicy(input)
	if err != nil {
		return "", err
	}
	path := filepath.Join(runtimeRoot, runtimeInstructionsFileName)
	if err := os.WriteFile(path, []byte(policy+"\n"), 0o600); err != nil {
		return "", fmt.Errorf("write session runtime instructions: %w", err)
	}
	if manifest != nil {
		manifest.RecordManagedFile(path, "runtime-instructions", true)
	}
	return path, nil
}
