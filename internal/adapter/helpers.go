package adapter

// jsonStr reads a string field, returning "" when missing or the wrong
// type. Never panics when payload is nil (an event with no stdin).
func jsonStr(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	v, ok := payload[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}

// jsonOptStr reads a string field, returning nil when missing or empty.
// Used for Event fields of type *string (e.g. Response) — ClaudeAdapter
// doesn't currently call this function (see section 3.6), but it's kept
// here since it's a helper SHARED by all three adapters.
func jsonOptStr(payload map[string]any, key string) *string {
	s := jsonStr(payload, key)
	if s == "" {
		return nil
	}
	return &s
}

// jsonObj reads an object field, returning nil when missing or the wrong
// type (e.g. "worktree" is a string instead of an object).
func jsonObj(payload map[string]any, key string) map[string]any {
	if payload == nil {
		return nil
	}
	v, ok := payload[key]
	if !ok {
		return nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return nil
	}
	return m
}
