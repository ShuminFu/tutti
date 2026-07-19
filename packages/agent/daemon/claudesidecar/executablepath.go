package claudesidecar

import (
	"os"
	"path/filepath"
	goruntime "runtime"
)

// resolveClaudeCodeExecutablePath decides which claude executable the sidecar
// spawns. Precedence mirrors executablePath.ts:
//
//  1. CLAUDE_CODE_EXECUTABLE — explicit operator override, always wins.
//  2. A native @anthropic-ai/claude-agent-sdk-<platform> package in a
//     node_modules tree above the working directory (dev tree via pnpm, or a
//     legacy vendored bundle). The TypeScript sidecar delegates this case to
//     the Node SDK's own resolution; the Go port resolves the same pinned
//     binary path directly since there is no SDK to delegate to.
//  3. TUTTI_CLAUDE_CODE_FALLBACK_EXECUTABLE — the tuttid-provisioned binary,
//     or a PATH-installed claude as last resort.
//
// An empty result means "spawn claude from PATH".
func resolveClaudeCodeExecutablePath(env map[string]string, cwd string) string {
	if explicit := trimmedEnv(env, "CLAUDE_CODE_EXECUTABLE"); explicit != "" {
		return explicit
	}
	if native := nativeSDKBinaryPath(cwd); native != "" {
		return native
	}
	if fallback := trimmedEnv(env, "TUTTI_CLAUDE_CODE_FALLBACK_EXECUTABLE"); fallback != "" {
		if _, err := os.Stat(fallback); err == nil {
			return fallback
		}
	}
	return ""
}

func trimmedEnv(env map[string]string, key string) string {
	return stringValue(env[key])
}

// nativeSDKBinaryPath mirrors the SDK's optional-dependency resolution: the
// native platform package provides `<package>/claude` in a node_modules tree.
func nativeSDKBinaryPath(cwd string) string {
	binaryName := "claude"
	if goruntime.GOOS == "windows" {
		binaryName = "claude.exe"
	}
	start := cwd
	if start == "" {
		wd, err := os.Getwd()
		if err != nil {
			return ""
		}
		start = wd
	}
	directory, err := filepath.Abs(start)
	if err != nil {
		directory = start
	}
	for {
		for _, platformKey := range nativePlatformPackageKeys() {
			candidate := filepath.Join(
				directory,
				"node_modules",
				"@anthropic-ai",
				"claude-agent-sdk-"+platformKey,
				binaryName,
			)
			if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
				return candidate
			}
		}
		parent := filepath.Dir(directory)
		if parent == directory {
			return ""
		}
		directory = parent
	}
}

func nativePlatformPackageKeys() []string {
	arch := goruntime.GOARCH
	switch arch {
	case "amd64":
		arch = "x64"
	case "arm":
		arch = "arm"
	}
	switch goruntime.GOOS {
	case "darwin":
		return []string{"darwin-" + arch}
	case "windows":
		return []string{"win32-" + arch}
	case "linux":
		return []string{"linux-" + arch, "linux-" + arch + "-musl"}
	default:
		return nil
	}
}
