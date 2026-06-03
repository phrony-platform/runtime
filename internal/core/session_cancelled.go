package core

import (
	"time"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
)

func sendSessionCancelled(events sessionEventSink, endedAt time.Time) error {
	var sessionEndedAtUnixMs int64
	if !endedAt.IsZero() {
		sessionEndedAtUnixMs = endedAt.UnixMilli()
	}
	return events.Send(&runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_Cancelled{
			Cancelled: &runtimev1.RunSessionInteractiveCancelled{
				SessionEndedAtUnixMs: sessionEndedAtUnixMs,
			},
		},
	})
}
