package modelplan

import "testing"

// A 1M context marker rides the model value as a window request, never as
// identity: a stored "X[1m]" selection still resolves against a list that
// enumerates "X", which is what every plan catalog does.
func TestModelsContainIgnoresTheContextWindowMarker(t *testing.T) {
	t.Parallel()
	models := []Model{{ID: "glm-5.3"}, {ID: "gpt-5.6-sol"}}
	cases := []struct {
		id   string
		want bool
	}{
		{"glm-5.3", true},
		{"glm-5.3[1m]", true},
		{"glm-5.3[1M]", true},
		{"  glm-5.3  ", true},
		{"gpt-5.6-sol[1m]", true},
		{"glm-5", false},
		{"[1m]", false},
		{"", false},
		// A provider's own parameter suffix is not this marker and must not be
		// silently matched against a different model.
		{"glm-5.3[fast=true]", false},
	}
	for _, testCase := range cases {
		if got := ModelsContain(models, testCase.id); got != testCase.want {
			t.Errorf("ModelsContain(%q) = %v, want %v", testCase.id, got, testCase.want)
		}
	}
}

func TestNormalizeAcceptsAMarkedDefaultModel(t *testing.T) {
	t.Parallel()
	plan, err := Normalize(Plan{
		ID:           "mp-1",
		WorkspaceID:  "ws",
		Name:         "Plan",
		Protocol:     ProtocolOpenAI,
		DefaultModel: "glm-5.3[1m]",
		Models:       []Model{{ID: "glm-5.3"}},
	})
	if err != nil {
		t.Fatalf("Normalize() error = %v, want the marked default accepted", err)
	}
	if plan.DefaultModel != "glm-5.3[1m]" {
		t.Fatalf("DefaultModel = %q, want the marker preserved", plan.DefaultModel)
	}
}

func TestNormalizeStillRejectsAnUnknownDefaultModel(t *testing.T) {
	t.Parallel()
	_, err := Normalize(Plan{
		ID:           "mp-1",
		WorkspaceID:  "ws",
		Name:         "Plan",
		Protocol:     ProtocolOpenAI,
		DefaultModel: "glm-5.4[1m]",
		Models:       []Model{{ID: "glm-5.3"}},
	})
	if err == nil {
		t.Fatal("Normalize() error = nil, want the unknown default rejected")
	}
}
