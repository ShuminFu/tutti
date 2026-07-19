package claudesidecar

func emitUsageUpdated(emit Emitter, turnID string, payload map[string]any) {
	cleaned := map[string]any{"turnId": turnID}
	for key, value := range payload {
		if value != nil {
			cleaned[key] = value
		}
	}
	if len(cleaned) <= 1 {
		return
	}
	emit("usage_updated", "", cleaned)
}
