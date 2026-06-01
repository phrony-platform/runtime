package tooldispatch

import (
	"time"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
)

func invokeMsg(call ToolCall) *runtimev1.WorkServerMsg {
	var deadlineMs int64
	if !call.Deadline.IsZero() {
		deadlineMs = call.Deadline.UnixMilli()
	}
	return &runtimev1.WorkServerMsg{
		Body: &runtimev1.WorkServerMsg_Invoke{
			Invoke: &runtimev1.WorkInvoke{
				CallId:          call.CallID,
				SessionId:       call.SessionID,
				AgentVersionId:  call.AgentVersionID,
				Turn:            int32(call.Turn),
				Tool:            call.Tool,
				Version:         call.Version,
				Args:            cloneArgs(call.Args),
				SideEffectClass: call.SideEffectClass,
				DeadlineUnixMs:  deadlineMs,
			},
		},
	}
}

func cancelMsg(callID string) *runtimev1.WorkServerMsg {
	return &runtimev1.WorkServerMsg{
		Body: &runtimev1.WorkServerMsg_Cancel{
			Cancel: &runtimev1.WorkToolCancel{CallId: callID},
		},
	}
}

func resultAckMsg(callID string) *runtimev1.WorkServerMsg {
	return &runtimev1.WorkServerMsg{
		Body: &runtimev1.WorkServerMsg_ResultAck{
			ResultAck: &runtimev1.WorkResultAck{CallId: callID},
		},
	}
}

func registeredMsg(workerID string, leaseTTL time.Duration) *runtimev1.WorkServerMsg {
	return &runtimev1.WorkServerMsg{
		Body: &runtimev1.WorkServerMsg_Registered{
			Registered: &runtimev1.WorkRegistered{
				WorkerId:   workerID,
				LeaseTtlMs: leaseTTL.Milliseconds(),
			},
		},
	}
}

func heartbeatAckMsg() *runtimev1.WorkServerMsg {
	return &runtimev1.WorkServerMsg{
		Body: &runtimev1.WorkServerMsg_HeartbeatAck{
			HeartbeatAck: &runtimev1.WorkHeartbeatAck{},
		},
	}
}

func protoToolResult(callID string, payload []byte, err *runtimev1.WorkToolError) ToolResult {
	res := ToolResult{CallID: callID, Payload: payload}
	if err != nil {
		res.Err = &ToolError{Code: err.GetCode(), Message: err.GetMessage()}
	}
	return res
}
