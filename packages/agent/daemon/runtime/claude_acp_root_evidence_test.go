package agentruntime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestClaudeACPRootTurnEvidenceFromTranscript(t *testing.T) {
	t.Parallel()

	assistant := func(text string, stopReason string, sidechain bool, tools ...string) string {
		content := `[{"type":"text","text":` + jsonQuote(text) + `}`
		for _, name := range tools {
			content += `,{"type":"tool_use","id":"tool-1","name":` + jsonQuote(name) + `}`
		}
		content += `]`
		side := "false"
		if sidechain {
			side = "true"
		}
		return `{"type":"assistant","isSidechain":` + side + `,"message":{"stop_reason":` + jsonQuote(stopReason) + `,"content":` + content + `}}`
	}
	user := func(text string) string {
		return `{"type":"user","isSidechain":false,"message":{"content":[{"type":"text","text":` + jsonQuote(text) + `}]}}`
	}

	tests := []struct {
		name       string
		lines      []string
		streamed   string
		wantOK     bool
		wantStop   string
		wantReason string
	}{
		{
			name: "root end_turn matching streamed text",
			lines: []string{
				user("inspect the workspace"),
				assistant("the files are ready", "end_turn", false),
			},
			streamed:   "the files are ready",
			wantOK:     true,
			wantStop:   "end_turn",
			wantReason: "root_assistant_terminal",
		},
		{
			name: "streamed prefix of transcript is enough",
			lines: []string{
				assistant("the files are ready for review", "end_turn", false),
			},
			streamed:   "the files are ready",
			wantOK:     true,
			wantStop:   "end_turn",
			wantReason: "root_assistant_terminal",
		},
		{
			name: "tool_use is not a root terminal",
			lines: []string{
				assistant("let me look", "tool_use", false, "Read"),
			},
			streamed:   "let me look",
			wantOK:     false,
			wantReason: "stop_reason_not_terminal",
		},
		{
			name: "open tool_use block is not a root terminal",
			lines: []string{
				assistant("let me look", "end_turn", false, "Read"),
			},
			streamed:   "let me look",
			wantOK:     false,
			wantReason: "root_tool_use_open",
		},
		{
			name: "background sidechain after root end_turn",
			lines: []string{
				assistant("launched the task", "end_turn", false),
				assistant("still working in the background", "end_turn", true),
			},
			streamed:   "launched the task",
			wantOK:     false,
			wantReason: "background_continuation",
		},
		{
			name: "later root user prompt invalidates the previous end_turn",
			lines: []string{
				assistant("old answer", "end_turn", false),
				user("next question"),
			},
			streamed:   "old answer",
			wantOK:     false,
			wantReason: "root_assistant_missing",
		},
		{
			name: "assistant text mismatch is not this turn",
			lines: []string{
				assistant("something else entirely", "end_turn", false),
			},
			streamed:   "the files are ready",
			wantOK:     false,
			wantReason: "assistant_text_mismatch",
		},
		{
			name: "pause_turn stays live",
			lines: []string{
				assistant("waiting on the network", "pause_turn", false),
			},
			streamed:   "waiting on the network",
			wantOK:     false,
			wantReason: "stop_reason_not_terminal",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "session.jsonl")
			if err := os.WriteFile(path, []byte(strings.Join(test.lines, "\n")+"\n"), 0o600); err != nil {
				t.Fatalf("write transcript: %v", err)
			}
			evidence, reason, ok := claudeACPRootTurnEvidenceFromTranscript(path, test.streamed)
			if ok != test.wantOK || reason != test.wantReason {
				t.Fatalf("ok=%v reason=%q, want ok=%v reason=%q evidence=%#v", ok, reason, test.wantOK, test.wantReason, evidence)
			}
			if ok && evidence.StopReason != test.wantStop {
				t.Fatalf("stop reason = %q, want %q", evidence.StopReason, test.wantStop)
			}
		})
	}
}

func TestClaudeACPTranscriptPathUsesProjectKeyAndSessionID(t *testing.T) {
	t.Parallel()

	path := claudeACPTranscriptPath("/tmp/claude-config", "/Users/jrrc/Projects/demo", "cc653557-37e0-57a9-a9f7-894418eedee9")
	if !strings.HasSuffix(path, filepath.Join("projects", claudeACPProjectID("/Users/jrrc/Projects/demo"), "cc653557-37e0-57a9-a9f7-894418eedee9.jsonl")) {
		t.Fatalf("path = %q", path)
	}
	if claudeACPTranscriptPath("", "/tmp", "sid") != "" || claudeACPTranscriptPath("/tmp", "", "sid") != "" {
		t.Fatal("empty inputs must not invent a transcript path")
	}
}

func jsonQuote(value string) string {
	return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
}
