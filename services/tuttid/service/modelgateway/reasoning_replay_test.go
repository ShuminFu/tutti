package modelgateway

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestReasoningScopeRoundTripsWithinOneSession(t *testing.T) {
	t.Parallel()

	scope := newReasoningScope("session-a")
	sealed, ok := scope.seal("hidden reasoning").(string)
	if !ok || sealed == "" {
		t.Fatalf("seal() = %#v", scope.seal("hidden reasoning"))
	}
	decoded, handled, err := scope.open(sealed)
	if err != nil || !handled || decoded != "hidden reasoning" {
		t.Fatalf("open() = %q, %v, %v", decoded, handled, err)
	}
}

func TestReasoningScopeRejectsAnotherSessionEnvelope(t *testing.T) {
	t.Parallel()

	sealed, _ := newReasoningScope("session-a").seal("hidden reasoning").(string)
	decoded, handled, err := newReasoningScope("session-b").open(sealed)
	if !handled {
		t.Fatal("a gateway envelope from another session must be recognized, not ignored")
	}
	if err == nil {
		t.Fatalf("cross-session replay accepted: %q", decoded)
	}
	if !strings.Contains(err.Error(), "another agent session") {
		t.Fatalf("error = %v", err)
	}
}

func TestReasoningScopeRejectsUnscopedEnvelope(t *testing.T) {
	t.Parallel()

	// The pre-scope shape: prefix plus payload with no session segment. It must
	// not be accepted, or the session binding would be optional and decorative.
	legacy := gatewayReasoningEncryptedPrefix + base64.RawURLEncoding.EncodeToString([]byte("hidden"))
	if _, handled, err := newReasoningScope("session-a").open(legacy); !handled || err == nil {
		t.Fatalf("unscoped envelope: handled = %v, err = %v", handled, err)
	}
}

func TestReasoningScopeSealsNothingWithoutSession(t *testing.T) {
	t.Parallel()

	// An envelope that names no session would be replayable anywhere, so the
	// gateway prefers reporting "no encrypted content" over minting one.
	if sealed := newReasoningScope("  ").seal("hidden"); sealed != nil {
		t.Fatalf("seal() without a session = %#v, want nil", sealed)
	}
	if _, handled, err := newReasoningScope("").open("tutti.reasoning.v1:x:y"); !handled || err == nil {
		t.Fatalf("open() without a session: handled = %v, err = %v", handled, err)
	}
}

func TestReasoningScopeIgnoresForeignEncryptedContent(t *testing.T) {
	t.Parallel()

	for _, value := range []string{
		"",
		"gAAAAABforeign-encrypted-content",
		"tutti.reasoning.v2:abc:def",
	} {
		decoded, handled, err := newReasoningScope("session-a").open(value)
		if handled || err != nil || decoded != "" {
			t.Fatalf("open(%q) = %q, %v, %v", value, decoded, handled, err)
		}
	}
}

// TestConvertResponseInputCountsReplayOutcomes pins the three ways a reasoning
// item comes back and, in particular, that an item the gateway cannot
// reconstruct is counted rather than turned into a hard failure: the upstream
// accepts an assistant turn without reasoning in the common case, so refusing
// the request would break working traffic to guard against a minority failure.
func TestConvertResponseInputCountsReplayOutcomes(t *testing.T) {
	t.Parallel()

	scope := newReasoningScope("session-a")
	sealed, _ := scope.seal("from envelope").(string)
	input, err := json.Marshal([]map[string]any{
		{"type": "reasoning", "summary": []any{}, "content": []any{map[string]any{"type": "reasoning_text", "text": "plain"}}},
		{"type": "reasoning", "summary": []any{}, "encrypted_content": sealed},
		{"type": "reasoning", "summary": []any{}, "content": []any{}, "encrypted_content": nil},
		{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "answer"}}},
	})
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	messages, stats, err := convertResponseInput(nil, input, nil, scope)
	if err != nil {
		t.Fatalf("convertResponseInput() error = %v", err)
	}
	if stats.items != 3 || stats.plain != 1 || stats.sealed != 1 || stats.empty != 1 {
		t.Fatalf("stats = %#v", stats)
	}
	if !stats.lost() {
		t.Fatal("an unreconstructable reasoning item must be reported as lost")
	}
	if len(messages) != 1 {
		t.Fatalf("messages = %#v", messages)
	}
	reasoning, _ := messages[0]["reasoning_content"].(string)
	if !strings.Contains(reasoning, "plain") || !strings.Contains(reasoning, "from envelope") {
		t.Fatalf("reconstructed reasoning = %q", reasoning)
	}
}

// TestConvertResponseInputRejectsCrossSessionReasoningReplay keeps the session
// binding honest at the point it matters: an envelope minted for another thread
// must fail the request instead of feeding this thread reasoning it never
// produced.
func TestConvertResponseInputRejectsCrossSessionReasoningReplay(t *testing.T) {
	t.Parallel()

	sealed, _ := newReasoningScope("session-b").seal("other thread reasoning").(string)
	input, err := json.Marshal([]map[string]any{
		{"type": "reasoning", "summary": []any{}, "encrypted_content": sealed},
		{"type": "message", "role": "assistant", "content": []any{map[string]any{"type": "output_text", "text": "answer"}}},
	})
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}

	_, _, err = convertResponseInput(nil, input, nil, newReasoningScope("session-a"))
	if err == nil {
		t.Fatal("expected cross-session reasoning replay to be rejected")
	}
	if !strings.Contains(err.Error(), "gateway reasoning replay") {
		t.Fatalf("error = %v", err)
	}
}

func TestReasoningReplayStatsFieldsStayStable(t *testing.T) {
	t.Parallel()

	fields := reasoningReplayStats{items: 3, plain: 1, sealed: 1, empty: 1}.fields()
	if len(fields)%2 != 0 {
		t.Fatalf("fields must be key/value pairs: %#v", fields)
	}
	seen := map[string]bool{}
	for index := 0; index < len(fields); index += 2 {
		key, _ := fields[index].(string)
		if key == "" {
			t.Fatalf("empty log key at %d: %#v", index, fields)
		}
		seen[key] = true
	}
	for _, key := range []string{"reasoning_items", "reasoning_plain", "reasoning_sealed", "reasoning_lost"} {
		if !seen[key] {
			t.Fatalf("missing log key %q in %#v", key, fields)
		}
	}
}
