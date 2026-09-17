package storesqlite

import "encoding/json"

// ProviderTokenUsage is the archive-facing token object copied from a
// provider report. Keys match Claude `message.usage` so a downstream archive
// writer can lift this object onto an event's top-level usage field without
// renaming. Missing keys were not reported and must stay absent.
type ProviderTokenUsage struct {
	InputTokens              *int64 `json:"input_tokens,omitempty"`
	OutputTokens             *int64 `json:"output_tokens,omitempty"`
	CacheReadInputTokens     *int64 `json:"cache_read_input_tokens,omitempty"`
	CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens,omitempty"`
}

// ParseProviderTokenUsage copies reported token counts from a provider payload.
// It accepts Claude `message.usage` (including a nested `message` object), a
// session `lastTurn.models` snapshot, or the same keys in camelCase. It never
// invents counts that the source omitted.
func ParseProviderTokenUsage(raw any) *ProviderTokenUsage {
	switch typed := raw.(type) {
	case nil:
		return nil
	case *ProviderTokenUsage:
		if !typed.HasReported() {
			return nil
		}
		return cloneProviderTokenUsage(typed)
	case ProviderTokenUsage:
		if !typed.HasReported() {
			return nil
		}
		return cloneProviderTokenUsage(&typed)
	case map[string]any:
		return parseProviderTokenUsageMap(typed)
	default:
		var value map[string]any
		if err := remarshalJSON(raw, &value); err != nil {
			return nil
		}
		return parseProviderTokenUsageMap(value)
	}
}

func parseProviderTokenUsageMap(raw map[string]any) *ProviderTokenUsage {
	if len(raw) == 0 {
		return nil
	}
	if nested := ParseProviderTokenUsage(raw["tokens"]); nested.HasReported() {
		return nested
	}
	if nested := ParseProviderTokenUsage(raw["usage"]); nested.HasReported() {
		return nested
	}
	if message := asJSONMap(raw["message"]); len(message) > 0 {
		if nested := ParseProviderTokenUsage(message["usage"]); nested.HasReported() {
			return nested
		}
	}
	if lastTurn := asJSONMap(raw["lastTurn"]); len(lastTurn) > 0 {
		if nested := parseProviderTokenUsageLastTurn(lastTurn); nested.HasReported() {
			return nested
		}
	}
	if models := asJSONMap(raw["models"]); len(models) > 0 {
		if nested := parseProviderTokenUsageModels(models); nested.HasReported() {
			return nested
		}
	}
	return parseProviderTokenUsageFields(raw)
}

func parseProviderTokenUsageLastTurn(lastTurn map[string]any) *ProviderTokenUsage {
	if models := asJSONMap(lastTurn["models"]); len(models) > 0 {
		if nested := parseProviderTokenUsageModels(models); nested.HasReported() {
			return nested
		}
	}
	return parseProviderTokenUsageFields(lastTurn)
}

func parseProviderTokenUsageModels(models map[string]any) *ProviderTokenUsage {
	var merged ProviderTokenUsage
	for _, raw := range models {
		item := parseProviderTokenUsageFields(asJSONMap(raw))
		if !item.HasReported() {
			continue
		}
		addProviderTokenUsage(&merged, item)
	}
	if !merged.HasReported() {
		return nil
	}
	return &merged
}

func parseProviderTokenUsageFields(raw map[string]any) *ProviderTokenUsage {
	if len(raw) == 0 {
		return nil
	}
	usage := ProviderTokenUsage{
		InputTokens:              optionalInt64Field(raw, "input_tokens", "inputTokens"),
		OutputTokens:             optionalInt64Field(raw, "output_tokens", "outputTokens"),
		CacheReadInputTokens:     optionalInt64Field(raw, "cache_read_input_tokens", "cacheReadInputTokens", "cachedInputTokens"),
		CacheCreationInputTokens: optionalInt64Field(raw, "cache_creation_input_tokens", "cacheCreationInputTokens"),
	}
	if !usage.HasReported() {
		return nil
	}
	return &usage
}

func optionalInt64Field(raw map[string]any, keys ...string) *int64 {
	for _, key := range keys {
		value, ok := int64Field(raw[key])
		if !ok {
			continue
		}
		copied := value
		return &copied
	}
	return nil
}

func int64Field(raw any) (int64, bool) {
	switch typed := raw.(type) {
	case int:
		return int64(typed), true
	case int32:
		return int64(typed), true
	case int64:
		return typed, true
	case uint:
		return int64(typed), true
	case uint32:
		return int64(typed), true
	case uint64:
		if typed > uint64(^uint64(0)>>1) {
			return 0, false
		}
		return int64(typed), true
	case float32:
		return int64(typed), true
	case float64:
		return int64(typed), true
	case json.Number:
		value, err := typed.Int64()
		if err != nil {
			parsed, parseErr := typed.Float64()
			if parseErr != nil {
				return 0, false
			}
			return int64(parsed), true
		}
		return value, true
	default:
		return 0, false
	}
}

func addProviderTokenUsage(target *ProviderTokenUsage, source *ProviderTokenUsage) {
	if target == nil || !source.HasReported() {
		return
	}
	addOptionalInt64(&target.InputTokens, source.InputTokens)
	addOptionalInt64(&target.OutputTokens, source.OutputTokens)
	addOptionalInt64(&target.CacheReadInputTokens, source.CacheReadInputTokens)
	addOptionalInt64(&target.CacheCreationInputTokens, source.CacheCreationInputTokens)
}

func addOptionalInt64(target **int64, source *int64) {
	if source == nil {
		return
	}
	if *target == nil {
		copied := *source
		*target = &copied
		return
	}
	**target += *source
}

func (u *ProviderTokenUsage) HasReported() bool {
	return u != nil && (u.InputTokens != nil ||
		u.OutputTokens != nil ||
		u.CacheReadInputTokens != nil ||
		u.CacheCreationInputTokens != nil)
}

func (u *ProviderTokenUsage) Map() map[string]any {
	if !u.HasReported() {
		return nil
	}
	result := map[string]any{}
	if u.InputTokens != nil {
		result["input_tokens"] = *u.InputTokens
	}
	if u.OutputTokens != nil {
		result["output_tokens"] = *u.OutputTokens
	}
	if u.CacheReadInputTokens != nil {
		result["cache_read_input_tokens"] = *u.CacheReadInputTokens
	}
	if u.CacheCreationInputTokens != nil {
		result["cache_creation_input_tokens"] = *u.CacheCreationInputTokens
	}
	return result
}

func providerTokenUsageEqual(left *ProviderTokenUsage, right *ProviderTokenUsage) bool {
	return optionalInt64Equal(left.inputPtr(), right.inputPtr()) &&
		optionalInt64Equal(left.outputPtr(), right.outputPtr()) &&
		optionalInt64Equal(left.cacheReadPtr(), right.cacheReadPtr()) &&
		optionalInt64Equal(left.cacheCreatePtr(), right.cacheCreatePtr())
}

func (u *ProviderTokenUsage) inputPtr() *int64 {
	if u == nil {
		return nil
	}
	return u.InputTokens
}

func (u *ProviderTokenUsage) outputPtr() *int64 {
	if u == nil {
		return nil
	}
	return u.OutputTokens
}

func (u *ProviderTokenUsage) cacheReadPtr() *int64 {
	if u == nil {
		return nil
	}
	return u.CacheReadInputTokens
}

func (u *ProviderTokenUsage) cacheCreatePtr() *int64 {
	if u == nil {
		return nil
	}
	return u.CacheCreationInputTokens
}

func optionalInt64Equal(left *int64, right *int64) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func cloneProviderTokenUsage(value *ProviderTokenUsage) *ProviderTokenUsage {
	if !value.HasReported() {
		return nil
	}
	cloned := ProviderTokenUsage{}
	if value.InputTokens != nil {
		copied := *value.InputTokens
		cloned.InputTokens = &copied
	}
	if value.OutputTokens != nil {
		copied := *value.OutputTokens
		cloned.OutputTokens = &copied
	}
	if value.CacheReadInputTokens != nil {
		copied := *value.CacheReadInputTokens
		cloned.CacheReadInputTokens = &copied
	}
	if value.CacheCreationInputTokens != nil {
		copied := *value.CacheCreationInputTokens
		cloned.CacheCreationInputTokens = &copied
	}
	return &cloned
}

func asJSONMap(raw any) map[string]any {
	if typed, ok := raw.(map[string]any); ok {
		return typed
	}
	return nil
}
