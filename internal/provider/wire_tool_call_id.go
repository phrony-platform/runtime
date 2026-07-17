package provider

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// MaxWireToolCallIDLen is the maximum tool_call_id length accepted by OpenAI.
const MaxWireToolCallIDLen = 64

// WireToolCallID returns an id safe for provider tool_call / tool_use wire fields.
// Provider-assigned ids within the limit are returned unchanged. Phrony durable
// call ids (which may exceed provider limits) are mapped to a deterministic
// short surrogate.
func WireToolCallID(callID string) string {
	callID = strings.TrimSpace(callID)
	if callID == "" || len(callID) <= MaxWireToolCallIDLen {
		return callID
	}
	sum := sha256.Sum256([]byte(callID))
	return "call_" + hex.EncodeToString(sum[:16])
}
