package agentruntime

import "testing"

func claudeSDKEffortDescriptorValues(descriptor map[string]any) []string {
	values := make([]string, 0)
	for _, option := range descriptor["options"].([]map[string]string) {
		values = append(values, option["value"])
	}
	return values
}

// The descriptor publishes Claude Code's own ladder, `max` included, with a
// label for every rung — the daemon used to hardcode a four-level list and
// silently rewrite a selected `max` down to `high`.
func TestClaudeCodeSDKEffortConfigOptionOffersFullLadder(t *testing.T) {
	descriptor := claudeSDKEffortConfigOption("max")
	want := []string{"low", "medium", "high", "xhigh", "max"}
	got := claudeSDKEffortDescriptorValues(descriptor)
	if len(got) != len(want) {
		t.Fatalf("effort options = %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("effort options = %#v, want %#v", got, want)
		}
	}
	for _, option := range descriptor["options"].([]map[string]string) {
		if option["name"] == "" {
			t.Fatalf("effort option %#v has no label", option)
		}
	}
	if got := descriptor["currentValue"]; got != "max" {
		t.Fatalf("effort currentValue = %#v, want max", got)
	}
}

// Anything outside the ladder falls back to the documented 'high' default.
func TestClaudeCodeSDKEffortConfigOptionDefaultsToHigh(t *testing.T) {
	for _, value := range []string{"", "ultra", "none", "minimal"} {
		descriptor := claudeSDKEffortConfigOption(value)
		if got := descriptor["currentValue"]; got != "high" {
			t.Fatalf("claudeSDKEffortConfigOption(%q) currentValue = %#v, want high", value, got)
		}
	}
}

func TestClaudeSDKCanonicalEffortKeepsMax(t *testing.T) {
	for _, level := range []string{"low", "medium", "high", "xhigh", "max"} {
		if got := claudeSDKCanonicalEffort(level); got != level {
			t.Fatalf("claudeSDKCanonicalEffort(%q) = %q, want %q", level, got, level)
		}
	}
	for _, unknown := range []string{"", "ultra", "none", "minimal"} {
		if got := claudeSDKCanonicalEffort(unknown); got != "" {
			t.Fatalf("claudeSDKCanonicalEffort(%q) = %q, want empty", unknown, got)
		}
	}
}
