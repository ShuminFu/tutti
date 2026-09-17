package storesqlite

import "testing"

func TestParseProviderTokenUsageCopiesClaudeKeys(t *testing.T) {
	t.Parallel()

	usage := ParseProviderTokenUsage(map[string]any{
		"input_tokens":                100,
		"output_tokens":               20,
		"cache_read_input_tokens":     7,
		"cache_creation_input_tokens": 3,
	})
	if !usage.HasReported() ||
		usage.InputTokens == nil || *usage.InputTokens != 100 ||
		usage.OutputTokens == nil || *usage.OutputTokens != 20 ||
		usage.CacheReadInputTokens == nil || *usage.CacheReadInputTokens != 7 ||
		usage.CacheCreationInputTokens == nil || *usage.CacheCreationInputTokens != 3 {
		t.Fatalf("usage=%#v, want Claude keys copied", usage)
	}
}

func TestParseProviderTokenUsageCopiesNestedMessageUsage(t *testing.T) {
	t.Parallel()

	usage := ParseProviderTokenUsage(map[string]any{
		"message": map[string]any{
			"usage": map[string]any{
				"input_tokens":                80,
				"output_tokens":               12,
				"cache_read_input_tokens":     4,
				"cache_creation_input_tokens": 2,
			},
		},
	})
	if !usage.HasReported() ||
		usage.InputTokens == nil || *usage.InputTokens != 80 ||
		usage.OutputTokens == nil || *usage.OutputTokens != 12 ||
		usage.CacheReadInputTokens == nil || *usage.CacheReadInputTokens != 4 ||
		usage.CacheCreationInputTokens == nil || *usage.CacheCreationInputTokens != 2 {
		t.Fatalf("usage=%#v, want nested message.usage copied", usage)
	}
}

func TestParseProviderTokenUsageCopiesLastTurnModels(t *testing.T) {
	t.Parallel()

	usage := ParseProviderTokenUsage(map[string]any{
		"lastTurn": map[string]any{
			"models": map[string]any{
				"default": map[string]any{
					"inputTokens":       int64(100),
					"cachedInputTokens": int64(30),
					"outputTokens":      int64(10),
				},
			},
		},
	})
	if !usage.HasReported() ||
		usage.InputTokens == nil || *usage.InputTokens != 100 ||
		usage.OutputTokens == nil || *usage.OutputTokens != 10 ||
		usage.CacheReadInputTokens == nil || *usage.CacheReadInputTokens != 30 ||
		usage.CacheCreationInputTokens != nil {
		t.Fatalf("usage=%#v, want lastTurn models flattened without invented cache-create", usage)
	}
}

func TestParseProviderTokenUsageDoesNotInventMissingKeys(t *testing.T) {
	t.Parallel()

	if got := ParseProviderTokenUsage(map[string]any{
		"contextWindow": map[string]any{"usedTokens": 10, "totalTokens": 200},
	}); got != nil {
		t.Fatalf("usage=%#v, want nil when the source has no token keys", got)
	}
	usage := ParseProviderTokenUsage(map[string]any{"input_tokens": 0, "output_tokens": 4})
	if usage.InputTokens == nil || *usage.InputTokens != 0 || usage.CacheReadInputTokens != nil {
		t.Fatalf("usage=%#v, want reported zero input and omitted cache keys", usage)
	}
}
