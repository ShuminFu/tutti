package contextwindow

import "testing"

func TestSplitRecognizesTheMarkerOnly(t *testing.T) {
	t.Parallel()
	cases := []struct {
		model  string
		base   string
		marked bool
	}{
		{"claude-opus-5", "claude-opus-5", false},
		{"claude-opus-5[1m]", "claude-opus-5", true},
		{"claude-opus-5[1M]", "claude-opus-5", true},
		{"claude-opus-5[1m][1m]", "claude-opus-5[1m]", true},
		{"  claude-opus-5[1m]  ", "claude-opus-5", true},
		{"[1m]", "", true},
		{"", "", false},
		{"[1m", "[1m", false},
		{"1m]", "1m]", false},
		// A different bracketed suffix is not this marker: providers address
		// models with their own parameters and those must survive untouched.
		{"composer-2.5[fast=true]", "composer-2.5[fast=true]", false},
		{"default[]", "default[]", false},
		{"x[1m]extra", "x[1m]extra", false},
	}
	for _, testCase := range cases {
		base, marked := Split(testCase.model)
		if base != testCase.base || marked != testCase.marked {
			t.Errorf("Split(%q) = (%q, %v), want (%q, %v)", testCase.model, base, marked, testCase.base, testCase.marked)
		}
	}
}

func TestBareAndWindowAgreeWithSplit(t *testing.T) {
	t.Parallel()
	if got := Bare("claude-opus-5[1m]"); got != "claude-opus-5" {
		t.Errorf("Bare() = %q, want claude-opus-5", got)
	}
	if got := Bare("claude-opus-5"); got != "claude-opus-5" {
		t.Errorf("Bare() = %q, want claude-opus-5", got)
	}
	if got := Window("claude-opus-5[1m]"); got != OneMillionTokens {
		t.Errorf("Window() = %d, want %d", got, OneMillionTokens)
	}
	if got := Window("claude-opus-5"); got != 0 {
		t.Errorf("Window() = %d, want 0", got)
	}
}

func TestWithMarkerIsIdempotent(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"claude-opus-5":     "claude-opus-5[1m]",
		"claude-opus-5[1m]": "claude-opus-5[1m]",
		"claude-opus-5[1M]": "claude-opus-5[1m]",
		"":                  "",
	}
	for model, want := range cases {
		if got := WithMarker(model); got != want {
			t.Errorf("WithMarker(%q) = %q, want %q", model, got, want)
		}
	}
}

func TestRequestsOneMillionMirrorsWindow(t *testing.T) {
	t.Parallel()
	if !RequestsOneMillion("glm-5.3[1m]") {
		t.Error("RequestsOneMillion(glm-5.3[1m]) = false, want true")
	}
	if RequestsOneMillion("glm-5.3") {
		t.Error("RequestsOneMillion(glm-5.3) = true, want false")
	}
}
