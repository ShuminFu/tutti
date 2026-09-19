package agent

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tutti-os/tutti/packages/agent/daemon/providerregistry"
)

func TestParseCodexCapabilityResponses(t *testing.T) {
	skills := parseCodexSkillCapabilities(json.RawMessage(`{"data":[{"skills":[{"name":"review","description":"Review code","path":"/tmp/review/SKILL.md","enabled":true}]}]}`))
	if len(skills) != 1 ||
		skills[0].Kind != "skill" ||
		skills[0].Status != "available" ||
		skills[0].Trigger != "$review" ||
		skills[0].Path == "" ||
		skills[0].Invocation != "promptItem" {
		t.Fatalf("parseCodexSkillCapabilities = %#v", skills)
	}

	apps := parseCodexAppCapabilities(json.RawMessage(`{"data":[{"id":"github","name":"GitHub","description":"GitHub connector","isAccessible":true,"isEnabled":true}]}`))
	if len(apps) != 1 || apps[0].Kind != "connector" || apps[0].Path != "app://github" || apps[0].Invocation != "promptItem" {
		t.Fatalf("parseCodexAppCapabilities = %#v", apps)
	}

	mcp := parseCodexMCPCapabilities(json.RawMessage(`{"data":[{"name":"docs","status":"running","tools":[{"name":"search","description":"Search docs"}]}]}`))
	if len(mcp) != 2 || mcp[0].Kind != "mcpServer" || mcp[1].Kind != "mcpTool" || mcp[1].ToolName != "search" {
		t.Fatalf("parseCodexMCPCapabilities = %#v", mcp)
	}
}

func TestComposerCapabilityCatalogListerRejectsUnknownKind(t *testing.T) {
	_, ok, err := composerCapabilityCatalogLister(composerProfile{
		CapabilityCatalogKind:    "poison",
		CapabilityCatalogCommand: []string{"codex", "app-server"},
	})
	if err == nil || ok {
		t.Fatalf("composerCapabilityCatalogLister() = (_, %v, %v), want unsupported error", ok, err)
	}
}

func TestComposerCapabilityCatalogListerRequiresRuntimeCommand(t *testing.T) {
	_, ok, err := composerCapabilityCatalogLister(composerProfile{
		CapabilityCatalogKind: providerregistry.CapabilityCatalogKindCodexAppServer,
	})
	if err == nil || ok {
		t.Fatalf("composerCapabilityCatalogLister() = (_, %v, %v), want command error", ok, err)
	}
}

func TestAppServerCapabilityListSkillsOnly(t *testing.T) {
	var stdin bytes.Buffer
	if err := writeAppServerCapabilityListRequests(
		&stdin,
		"/tmp/workspace",
		appServerCatalogRequestSetSkillsOnly,
	); err != nil {
		t.Fatalf("writeAppServerCapabilityListRequests returned error: %v", err)
	}
	requests := stdin.String()
	if !strings.Contains(requests, `"method":"skills/list"`) {
		t.Fatalf("requests = %q, want skills/list", requests)
	}
	for _, excluded := range []string{"app/list", "plugin/list", "mcpServerStatus/list"} {
		if strings.Contains(requests, excluded) {
			t.Fatalf("requests = %q, must not include %s", requests, excluded)
		}
	}

	options, err := readAppServerCapabilityListResponses(
		strings.NewReader(`{"id":"2","result":{"data":[{"skills":[{"name":"review","description":"Review","path":"/tmp/review/SKILL.md","enabled":true}]}]}}`+"\n"),
		appServerCatalogRequestSetSkillsOnly,
	)
	if err != nil {
		t.Fatalf("readAppServerCapabilityListResponses returned error: %v", err)
	}
	if len(options) != 1 {
		t.Fatalf("options = %#v, want one skill", options)
	}
	skill := options[0]
	if skill.ID != "skill:review" ||
		skill.Kind != "skill" ||
		skill.Trigger != "$review" ||
		skill.Path != "/tmp/review/SKILL.md" ||
		skill.Invocation != "promptItem" {
		t.Fatalf("skill option = %#v", skill)
	}
}

func TestAppServerCatalogRequestsRejectsUnknownSet(t *testing.T) {
	if _, _, err := appServerCatalogRequests("/tmp/workspace", "poison"); err == nil {
		t.Fatal("appServerCatalogRequests() error = nil, want unsupported request set")
	}
}

func TestAppServerLocalSkillsReadsOnlyEnabledInstalledPlugins(t *testing.T) {
	var requests bytes.Buffer
	if err := writeAppServerCapabilityListRequests(&requests, "/tmp/project", appServerCatalogRequestSetLocalSkills); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(requests.String(), "app/list") || strings.Contains(requests.String(), "mcpServerStatus/list") || !strings.Contains(requests.String(), `"cwds":["/tmp/project"]`) {
		t.Fatal(requests.String())
	}
	responses := strings.Join([]string{
		`{"id":"2","result":{"data":[]}}`,
		`{"id":"4","result":{"marketplaces":[{"name":"local","path":"/tmp/market","plugins":[{"name":"active","installed":true,"enabled":true},{"name":"disabled","installed":true,"enabled":false},{"name":"uninstalled","installed":false,"enabled":true},{"name":"admin-disabled","installed":true,"enabled":true,"availability":"DISABLED_BY_ADMIN"}]}]}}`,
		`{"id":"plugin-skill:0","result":{"plugin":{"summary":{"name":"active","installed":true,"enabled":true},"skills":[{"name":"review","description":"Review","enabled":true,"path":"/tmp/review/SKILL.md"},{"name":"disabled","enabled":false,"path":"/tmp/disabled/SKILL.md"},{"name":"remote","enabled":true,"path":null}]}}}`,
	}, "\n")
	requests.Reset()
	options, err := readAppServerCapabilityListResponsesWithPlugins(strings.NewReader(responses), &requests, appServerCatalogRequestSetLocalSkills)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(requests.String(), `"method":"plugin/read"`) != 1 || !strings.Contains(requests.String(), `"marketplacePath":"/tmp/market"`) {
		t.Fatal(requests.String())
	}
	var skills []ComposerCapabilityOption
	for _, option := range options {
		if option.Kind == "skill" {
			skills = append(skills, option)
		}
	}
	if len(skills) != 1 || skills[0].Name != "active:review" || skills[0].Path != "/tmp/review/SKILL.md" {
		t.Fatalf("skills=%#v", skills)
	}
}

func TestParseCodexPluginSkillsRejectsChangedDisabledPlugin(t *testing.T) {
	for _, summary := range []string{`{"name":"active","installed":true,"enabled":false}`, `{"name":"other","installed":true,"enabled":true}`} {
		raw := json.RawMessage(`{"plugin":{"summary":` + summary + `,"skills":[{"name":"review","enabled":true,"path":"/tmp/review/SKILL.md"}]}}`)
		if got := parseCodexPluginSkillCapabilities(raw, "active"); len(got) != 0 {
			t.Fatalf("got=%#v", got)
		}
	}
}

func TestAppServerLocalSkillsReportsIncompleteAndRPCFailure(t *testing.T) {
	for _, responses := range []string{
		`{"id":"2","result":{"data":[]}}`,
		"{\"id\":\"2\",\"result\":{\"data\":[]}}\n{\"id\":\"4\",\"error\":{\"message\":\"unavailable\"}}",
	} {
		if _, err := readAppServerCapabilityListResponses(strings.NewReader(responses), appServerCatalogRequestSetLocalSkills); err == nil {
			t.Fatal("incomplete discovery reported success")
		}
	}
}
