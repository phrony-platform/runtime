package provider

// TokenUsage reports prompt and completion token counts for a model call.
type TokenUsage struct {
	InputTokens  int
	OutputTokens int
	Estimated    bool
}

// Total returns input plus output tokens.
func (u TokenUsage) Total() int {
	return u.InputTokens + u.OutputTokens
}

// IsZero reports whether any usage was recorded.
func (u TokenUsage) IsZero() bool {
	return u.InputTokens == 0 && u.OutputTokens == 0
}

// Add accumulates usage into the receiver.
func (u *TokenUsage) Add(other TokenUsage) {
	u.InputTokens += other.InputTokens
	u.OutputTokens += other.OutputTokens
	if other.Estimated {
		u.Estimated = true
	}
}
