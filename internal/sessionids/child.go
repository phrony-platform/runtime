package sessionids

import (
	"github.com/google/uuid"
)

// childSessionNamespace seeds deterministic child session ids derived from the
// originating tool call id, so an interrupted delegation resumes the same
// durable child on recovery instead of spawning a duplicate.
var childSessionNamespace = uuid.MustParse("b3f2a1c0-1d4e-4f6a-9c2b-7e8d9a0b1c2d")

// ChildFromCallID returns the stable session id for the child spawned by a tool
// call. It is a pure function of the call id so recovery can locate the child.
func ChildFromCallID(callID string) string {
	return "run_" + uuid.NewSHA1(childSessionNamespace, []byte(callID)).String()
}
