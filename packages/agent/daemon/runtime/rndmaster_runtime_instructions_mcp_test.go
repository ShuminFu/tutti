package agentruntime

import (
	"testing"
)

func runtimeInstructionsMCPServersFixture() map[string]any {
	return map[string]any{
		"stdio-plain": map[string]any{
			"command": "workflow",
		},
		"stdio-keep": map[string]any{
			"command": "keep",
			"env": map[string]any{
				runtimeInstructionsFileEnv: "keep-me",
			},
		},
		"http-remote": map[string]any{
			"type": "http",
			"url":  "http://127.0.0.1:9/mcp",
		},
	}
}

func runtimeInstructionsMCPContract() rndmasterRuntimeContract {
	return rndmasterRuntimeContract{
		Version: 1,
		MCPConfig: map[string]any{
			"mcpServers": runtimeInstructionsMCPServersFixture(),
		},
	}
}

func acpMCPServerByName(t *testing.T, servers []any) map[string]map[string]any {
	t.Helper()
	out := map[string]map[string]any{}
	for _, raw := range servers {
		body := payloadObject(raw)
		name := asString(body["name"])
		if name == "" {
			t.Fatalf("ACP mcp server missing name: %#v", raw)
		}
		out[name] = body
	}
	return out
}

func acpEnvValue(entry map[string]any, key string) string {
	items, _ := entry["env"].([]any)
	for _, item := range items {
		obj := payloadObject(item)
		if asString(obj["name"]) == key {
			return asString(obj["value"])
		}
	}
	return ""
}

func assertMapRuntimeInstructionsInjected(t *testing.T, servers map[string]any) {
	t.Helper()
	plain := payloadObject(servers["stdio-plain"])
	if asString(payloadObject(plain["env"])[runtimeInstructionsFileEnv]) != "/x/y.md" {
		t.Fatalf("stdio-plain env = %#v, want %s=/x/y.md", plain["env"], runtimeInstructionsFileEnv)
	}
	keep := payloadObject(servers["stdio-keep"])
	if asString(payloadObject(keep["env"])[runtimeInstructionsFileEnv]) != "keep-me" {
		t.Fatalf("stdio-keep env = %#v, want keep-me", keep["env"])
	}
	http := payloadObject(servers["http-remote"])
	if _, exists := http["env"]; exists {
		t.Fatalf("http-remote env = %#v, want no env key", http["env"])
	}
}

func assertACPRuntimeInstructionsInjected(t *testing.T, servers []any) {
	t.Helper()
	byName := acpMCPServerByName(t, servers)
	if acpEnvValue(byName["stdio-plain"], runtimeInstructionsFileEnv) != "/x/y.md" {
		t.Fatalf("stdio-plain ACP env = %#v, want %s=/x/y.md", byName["stdio-plain"]["env"], runtimeInstructionsFileEnv)
	}
	if acpEnvValue(byName["stdio-keep"], runtimeInstructionsFileEnv) != "keep-me" {
		t.Fatalf("stdio-keep ACP env = %#v, want keep-me", byName["stdio-keep"]["env"])
	}
	http := byName["http-remote"]
	if http == nil {
		t.Fatalf("http-remote missing from ACP servers: %#v", servers)
	}
	if _, exists := http["env"]; exists {
		t.Fatalf("http-remote ACP env = %#v, want no env key", http["env"])
	}
}

func assertMapRuntimeInstructionsUnchanged(t *testing.T, servers map[string]any) {
	t.Helper()
	plain := payloadObject(servers["stdio-plain"])
	if _, exists := payloadObject(plain["env"])[runtimeInstructionsFileEnv]; exists {
		t.Fatalf("stdio-plain env = %#v, want no %s", plain["env"], runtimeInstructionsFileEnv)
	}
	keep := payloadObject(servers["stdio-keep"])
	if asString(payloadObject(keep["env"])[runtimeInstructionsFileEnv]) != "keep-me" {
		t.Fatalf("stdio-keep env = %#v, want keep-me", keep["env"])
	}
	http := payloadObject(servers["http-remote"])
	if _, exists := http["env"]; exists {
		t.Fatalf("http-remote env = %#v, want no env key", http["env"])
	}
}

func assertACPRuntimeInstructionsUnchanged(t *testing.T, servers []any) {
	t.Helper()
	byName := acpMCPServerByName(t, servers)
	if value := acpEnvValue(byName["stdio-plain"], runtimeInstructionsFileEnv); value != "" {
		t.Fatalf("stdio-plain ACP env = %#v, want unchanged (no %s)", byName["stdio-plain"]["env"], runtimeInstructionsFileEnv)
	}
	if acpEnvValue(byName["stdio-keep"], runtimeInstructionsFileEnv) != "keep-me" {
		t.Fatalf("stdio-keep ACP env = %#v, want keep-me", byName["stdio-keep"]["env"])
	}
	http := byName["http-remote"]
	if http == nil {
		t.Fatalf("http-remote missing from ACP servers: %#v", servers)
	}
	if _, exists := http["env"]; exists {
		t.Fatalf("http-remote ACP env = %#v, want no env key", http["env"])
	}
}

func TestRnDMasterMCPServersInjectRuntimeInstructionsFile(t *testing.T) {
	contract := runtimeInstructionsMCPContract()
	withEnv := []string{runtimeInstructionsFileEnv + "=/x/y.md"}
	withoutEnv := []string{"KEEP=1"}

	mapServers, ok := rndmasterMCPServers(contract, withEnv)
	if !ok {
		t.Fatal("map mcpServers missing")
	}
	assertMapRuntimeInstructionsInjected(t, mapServers)

	sdkServers := payloadObject(claudeCodeSDKStartOptions(Session{Env: withEnv}, contract)["mcpServers"])
	assertMapRuntimeInstructionsInjected(t, sdkServers)

	acpServers, _, err := rndmasterACPMCPServers(contract, withEnv)
	if err != nil {
		t.Fatalf("rndmasterACPMCPServers: %v", err)
	}
	assertACPRuntimeInstructionsInjected(t, acpServers)

	mapPlain, ok := rndmasterMCPServers(contract, withoutEnv)
	if !ok {
		t.Fatal("map mcpServers missing when session env has no instructions file")
	}
	assertMapRuntimeInstructionsUnchanged(t, mapPlain)

	acpPlain, _, err := rndmasterACPMCPServers(contract, withoutEnv)
	if err != nil {
		t.Fatalf("rndmasterACPMCPServers without env: %v", err)
	}
	assertACPRuntimeInstructionsUnchanged(t, acpPlain)
}
