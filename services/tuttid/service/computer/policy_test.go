package computer

import (
	"errors"
	"testing"
)

func TestAnnotateNativeToolCatalogPreservesEveryToolAndAuthorization(t *testing.T) {
	catalog := ToolCatalog{
		SchemaVersion:     "1",
		CapabilityVersion: "1",
		Tools: []ToolDefinition{
			{Name: "click", Capabilities: []string{"input.pointer.click", "input.pointer.click.left"}},
			{Name: "mixed_future", Capabilities: []string{"input.pointer.click", "future.privileged"}},
			{Name: "kill_app", Capabilities: []string{"app.kill"}},
			{Name: "missing_metadata"},
		},
	}

	annotated, err := annotateNativeToolCatalog(catalog)
	if err != nil {
		t.Fatalf("annotateNativeToolCatalog: %v", err)
	}
	if len(annotated.Tools) != len(catalog.Tools) {
		t.Fatalf("annotated tools = %#v", annotated.Tools)
	}
	if !annotated.Tools[0].Allowed || annotated.Tools[0].DenialReason != "" {
		t.Fatalf("allowed tool = %#v", annotated.Tools[0])
	}
	for _, tool := range annotated.Tools[1:] {
		if tool.Allowed || tool.DenialReason == "" {
			t.Fatalf("denied tool lacks authorization metadata: %#v", tool)
		}
	}
	if annotated.SchemaVersion != "1" || annotated.CapabilityVersion != "1" {
		t.Fatalf("annotated catalog lost versions: %#v", annotated)
	}
}

func TestNativeToolPolicyRejectsUnknownOrMissingCatalogVersions(t *testing.T) {
	for _, catalog := range []ToolCatalog{
		{SchemaVersion: "2", CapabilityVersion: "1"},
		{SchemaVersion: "1", CapabilityVersion: "2"},
		{},
	} {
		if _, err := annotateNativeToolCatalog(catalog); !errors.Is(err, ErrNativeToolCatalogUnsupported) {
			t.Fatalf("annotate err = %v, want ErrNativeToolCatalogUnsupported for %#v", err, catalog)
		}
		if _, err := requireAllowedNativeTool(catalog, "click"); !errors.Is(err, ErrNativeToolCatalogUnsupported) {
			t.Fatalf("require err = %v, want ErrNativeToolCatalogUnsupported for %#v", err, catalog)
		}
	}
}

func TestNativeInputDeliveryModeAuthorization(t *testing.T) {
	tests := []struct {
		name         string
		capabilities []string
		allowed      bool
	}{
		{"click", []string{"input.pointer.click", "input.delivery_mode"}, true},
		{"type_text", []string{"input.keyboard.type", "input.delivery_mode"}, true},
		{"press_key", []string{"input.keyboard.press", "input.delivery_mode"}, true},
		{"hotkey", []string{"input.keyboard.hotkey", "input.delivery_mode"}, true},
		{"scroll", []string{"input.pointer.scroll", "input.delivery_mode"}, true},
		{"future_input", []string{"input.keyboard.type", "input.delivery_mode", "future.privileged"}, false},
		{"browser_dialog", []string{"browser.dialog", "input.delivery_mode"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			catalog := ToolCatalog{
				SchemaVersion: "1", CapabilityVersion: "1",
				Tools: []ToolDefinition{{Name: tt.name, Capabilities: tt.capabilities}},
			}
			annotated, err := annotateNativeToolCatalog(catalog)
			if err != nil {
				t.Fatal(err)
			}
			if got := annotated.Tools[0]; got.Allowed != tt.allowed || (got.DenialReason == "") != tt.allowed {
				t.Fatalf("discovery authorization = %#v, want allowed=%v", got, tt.allowed)
			}
			_, err = requireAllowedNativeTool(catalog, tt.name)
			if tt.allowed && err != nil {
				t.Fatalf("invocation authorization: %v", err)
			}
			if !tt.allowed && !errors.Is(err, ErrNativeToolNotAllowed) {
				t.Fatalf("invocation error = %v, want ErrNativeToolNotAllowed", err)
			}
		})
	}
}

func TestRequireAllowedNativeToolRejectsUnknownAndDeniedCapabilities(t *testing.T) {
	for _, tool := range []ToolDefinition{
		{Name: "mixed_future", Capabilities: []string{"screen.capture", "future.privileged"}},
		{Name: "missing_metadata"},
	} {
		t.Run(tool.Name, func(t *testing.T) {
			_, err := requireAllowedNativeTool(ToolCatalog{
				SchemaVersion:     "1",
				CapabilityVersion: "1",
				Tools:             []ToolDefinition{tool},
			}, tool.Name)
			if !errors.Is(err, ErrNativeToolNotAllowed) {
				t.Fatalf("err = %v, want ErrNativeToolNotAllowed", err)
			}
		})
	}
}

func TestNativeToolPolicyAllowsGlobalConfigWrites(t *testing.T) {
	tool := ToolDefinition{
		Name:         "set_config",
		Capabilities: []string{"system.config.write"},
	}
	catalog, err := annotateNativeToolCatalog(ToolCatalog{
		SchemaVersion:     "1",
		CapabilityVersion: "1",
		Tools:             []ToolDefinition{tool},
	})
	if err != nil {
		t.Fatalf("annotateNativeToolCatalog(set_config): %v", err)
	}
	if len(catalog.Tools) != 1 || !catalog.Tools[0].Allowed || catalog.Tools[0].DenialReason != "" {
		t.Fatalf("tool = %#v", catalog.Tools)
	}
	got, err := requireAllowedNativeTool(ToolCatalog{
		SchemaVersion:     "1",
		CapabilityVersion: "1",
		Tools:             []ToolDefinition{tool},
	}, tool.Name)
	if err != nil || got.Name != tool.Name || !got.Allowed {
		t.Fatalf("tool = %#v, err = %v", got, err)
	}
}

func TestNativeToolPolicyAllowsConfigReads(t *testing.T) {
	tool := ToolDefinition{Name: "get_config", Capabilities: []string{"system.config.read"}}
	got, err := requireAllowedNativeTool(ToolCatalog{
		SchemaVersion:     "1",
		CapabilityVersion: "1",
		Tools:             []ToolDefinition{tool},
	}, tool.Name)
	if err != nil || got.Name != tool.Name {
		t.Fatalf("tool = %#v, err = %v", got, err)
	}
}
