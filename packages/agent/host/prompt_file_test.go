package agenthost

import (
	"errors"
	"testing"
)

func TestNormalizePromptContentPreservesCanonicalFile(t *testing.T) {
	content, text, err := normalizePromptContent([]PromptContentBlock{{
		Type: "file", Path: `C:\Users\me\report.pdf`, SizeBytes: 42,
	}})
	if err != nil {
		t.Fatalf("normalizePromptContent() error = %v, want nil", err)
	}
	if len(content) != 1 || content[0].Name != "report.pdf" || content[0].Path != `C:\Users\me\report.pdf` || content[0].SizeBytes != 42 || text != "" {
		t.Fatalf("content = %#v, text = %q, want canonical file", content, text)
	}
	if _, _, err := normalizePromptContent([]PromptContentBlock{{Type: "file", Name: "relative.txt", Path: "relative.txt"}}); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("relative file error = %v, want ErrInvalidArgument", err)
	}
}
