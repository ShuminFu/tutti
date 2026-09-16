package agentextension

import (
	"encoding/json"
	"testing"
)

// The static reasoning declaration is the only capability projection available
// on the defaults-persistence path — there is no session and no workspace to
// read runtime ACP options from — so a loose profile would silently widen or
// narrow the composer's reasoning picker with nothing to catch it. These tests
// pin the contract that validateComposerReasoningDeclaration, ReasoningOptions,
// ReasoningDefault and ACPConfigOptionIDs jointly promise.

func reasoningProfileFixture(declaration ComposerReasoningDeclaration) ComposerProfile {
	return ComposerProfile{
		SchemaVersion: "tutti.agent.composer.v1",
		ConfigOptions: &ComposerConfigOptionsProfile{
			Model:      ComposerConfigOptionReference{ACPOptionID: "model"},
			Permission: ComposerConfigOptionReference{ACPOptionID: "mode"},
			Reasoning:  declaration,
		},
	}
}

func TestComposerReasoningDeclarationValidation(t *testing.T) {
	t.Run("accepts a full declared contract", func(t *testing.T) {
		profile := reasoningProfileFixture(ComposerReasoningDeclaration{
			ACPOptionID: "reasoning_effort",
			Options:     []string{"off", "minimal", "low", "medium", "high", "xhigh", "max"},
			Default:     "off",
		})
		if err := validateComposerProfile(profile); err != nil {
			t.Fatalf("validateComposerProfile() error = %v", err)
		}
	})

	t.Run("accepts declared levels without a default", func(t *testing.T) {
		profile := reasoningProfileFixture(ComposerReasoningDeclaration{
			ACPOptionID: "reasoning_effort",
			Options:     []string{"low", "high"},
		})
		if err := validateComposerProfile(profile); err != nil {
			t.Fatalf("validateComposerProfile() error = %v", err)
		}
	})

	t.Run("accepts an option id without levels", func(t *testing.T) {
		// Declaring the ACP id and no levels must stay legal: the runtime is then
		// the only source of levels, which is the pre-existing behaviour for every
		// extension that does not opt into a static contract.
		profile := reasoningProfileFixture(ComposerReasoningDeclaration{
			ACPOptionID: "reasoning_effort",
		})
		if err := validateComposerProfile(profile); err != nil {
			t.Fatalf("validateComposerProfile() error = %v", err)
		}
	})

	t.Run("rejects a blank level", func(t *testing.T) {
		profile := reasoningProfileFixture(ComposerReasoningDeclaration{
			ACPOptionID: "reasoning_effort",
			Options:     []string{"low", "   "},
		})
		if err := validateComposerProfile(profile); err == nil {
			t.Fatal("validateComposerProfile() accepted a blank reasoning level")
		}
	})

	t.Run("rejects a duplicate level", func(t *testing.T) {
		profile := reasoningProfileFixture(ComposerReasoningDeclaration{
			ACPOptionID: "reasoning_effort",
			Options:     []string{"low", "high", "low"},
		})
		if err := validateComposerProfile(profile); err == nil {
			t.Fatal("validateComposerProfile() accepted duplicate reasoning levels")
		}
	})

	t.Run("rejects a default outside the declared levels", func(t *testing.T) {
		profile := reasoningProfileFixture(ComposerReasoningDeclaration{
			ACPOptionID: "reasoning_effort",
			Options:     []string{"low", "high"},
			Default:     "max",
		})
		if err := validateComposerProfile(profile); err == nil {
			t.Fatal("validateComposerProfile() accepted an undeclared reasoning default")
		}
	})

	t.Run("rejects an unusable reasoning option id", func(t *testing.T) {
		// "reasoning_effort" itself must pass: the underscore is the ACP id the
		// DeepSeek Harness runtime actually advertises.
		profile := reasoningProfileFixture(ComposerReasoningDeclaration{
			ACPOptionID: "reasoning effort",
			Options:     []string{"low"},
		})
		if err := validateComposerProfile(profile); err == nil {
			t.Fatal("validateComposerProfile() accepted an unusable reasoning option id")
		}
	})
}

func TestComposerReasoningDeclarationAccessors(t *testing.T) {
	t.Run("trims, drops blanks and dedupes", func(t *testing.T) {
		declaration := ComposerReasoningDeclaration{
			Options: []string{" low ", "", "high", "low", "   "},
		}
		options, declared := declaration.ReasoningOptions()
		if !declared {
			t.Fatal("ReasoningOptions() reported no declaration for declared levels")
		}
		if len(options) != 2 || options[0] != "low" || options[1] != "high" {
			t.Fatalf("ReasoningOptions() = %#v, want [low high]", options)
		}
	})

	t.Run("reports no declaration without levels", func(t *testing.T) {
		declaration := ComposerReasoningDeclaration{ACPOptionID: "reasoning_effort"}
		options, declared := declaration.ReasoningOptions()
		if declared {
			t.Fatalf("ReasoningOptions() reported a declaration for %#v", options)
		}
		if len(options) != 0 {
			t.Fatalf("ReasoningOptions() = %#v, want empty", options)
		}
	})

	t.Run("keeps a declared default that is offered", func(t *testing.T) {
		declaration := ComposerReasoningDeclaration{Default: "high"}
		if got := declaration.ReasoningDefault([]string{"low", "high"}); got != "high" {
			t.Fatalf("ReasoningDefault() = %q, want high", got)
		}
	})

	t.Run("falls back to the first offered level", func(t *testing.T) {
		declaration := ComposerReasoningDeclaration{Default: "max"}
		if got := declaration.ReasoningDefault([]string{"low", "high"}); got != "low" {
			t.Fatalf("ReasoningDefault() = %q, want low", got)
		}
	})

	t.Run("returns empty without offered levels", func(t *testing.T) {
		declaration := ComposerReasoningDeclaration{Default: "low"}
		if got := declaration.ReasoningDefault(nil); got != "" {
			t.Fatalf("ReasoningDefault() = %q, want empty", got)
		}
	})
}

// The DeepSeek Harness extension declares exactly this block after migrating off
// the legacy top-level "model"/"permission" keys. The ids and the level list are
// the wire contract the composer resolves against, so a silent change to either
// (for example resolving the permission id as "permission" instead of "mode")
// would break the pickers for that target with no other test noticing.
func TestComposerProfileCanonicalConfigOptionsJSON(t *testing.T) {
	const document = `{
		"schemaVersion": "tutti.agent.composer.v1",
		"configOptions": {
			"model": {"acpOptionId": "model"},
			"permission": {"acpOptionId": "mode"},
			"reasoning": {
				"acpOptionId": "reasoning_effort",
				"options": ["off", "minimal", "low", "medium", "high", "xhigh", "max"],
				"default": "off"
			}
		},
		"permissionModes": []
	}`
	var profile ComposerProfile
	if err := json.Unmarshal([]byte(document), &profile); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if err := validateComposerProfile(profile); err != nil {
		t.Fatalf("validateComposerProfile() error = %v", err)
	}
	model, permission, reasoning := profile.ACPConfigOptionIDs()
	if model != "model" || permission != "mode" || reasoning != "reasoning_effort" {
		t.Fatalf("config option ids = %q, %q, %q", model, permission, reasoning)
	}
	options, declared := profile.ConfigOptions.Reasoning.ReasoningOptions()
	if !declared || len(options) != 7 {
		t.Fatalf("reasoning options = %#v, declared = %v", options, declared)
	}
	if got := profile.ConfigOptions.Reasoning.ReasoningDefault(options); got != "off" {
		t.Fatalf("reasoning default = %q, want off", got)
	}
}

// The legacy shape is still accepted, and its fallback must keep resolving the
// ids existing profiles depend on.
func TestComposerProfileLegacyConfigOptionsFallback(t *testing.T) {
	profile := ComposerProfile{
		SchemaVersion: "tutti.agent.composer.v1",
		Model:         json.RawMessage(`{"source":"acp-session-models"}`),
		Permission:    json.RawMessage(`{"source":"acp-session-modes"}`),
	}
	if err := validateComposerProfile(profile); err != nil {
		t.Fatalf("validateComposerProfile() error = %v", err)
	}
	model, permission, reasoning := profile.ACPConfigOptionIDs()
	if model != "model" || permission != "mode" || reasoning != "reasoning_effort" {
		t.Fatalf("legacy config option ids = %q, %q, %q", model, permission, reasoning)
	}
	if profile.ConfigOptions != nil {
		t.Fatalf("legacy profile unexpectedly carried configOptions: %#v", profile.ConfigOptions)
	}
}
