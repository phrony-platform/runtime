package tooldispatch

import (
	"io"
	"sync"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// WorkStream serves the Runtime.Work bidirectional RPC for one worker connection.
type WorkStream struct {
	Registry *WorkerRegistry
}

// ServeWork processes client messages until the stream ends or errors.
func (h *WorkStream) ServeWork(stream runtimev1.Runtime_WorkServer) error {
	if h == nil || h.Registry == nil {
		return status.Error(codes.Unavailable, "tool worker registry is not configured")
	}

	var (
		mu       sync.Mutex
		workerID string
		send     = func(msg *runtimev1.WorkServerMsg) error {
			return stream.Send(msg)
		}
	)

	for {
		msg, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			if workerID != "" {
				h.Registry.UnregisterWorker(workerID)
			}
			if stream.Context().Err() != nil {
				return nil
			}
			if st, ok := status.FromError(err); ok && st.Code() == codes.Canceled {
				return nil
			}
			return err
		}

		switch body := msg.GetBody().(type) {
		case *runtimev1.WorkClientMsg_Register:
			reg := body.Register
			if reg == nil {
				return status.Error(codes.InvalidArgument, "register message is required")
			}
			workerID = reg.GetWorkerId()
			handlers := make([]HandlerAdvertisement, 0, len(reg.GetHandlers()))
			for _, adv := range reg.GetHandlers() {
				handlers = append(handlers, HandlerAdvertisement{
					Tool:            adv.GetTool(),
					Version:         adv.GetVersion(),
					ContractVersion: adv.GetContractVersion(),
					DescriptorHash:  adv.GetDescriptorHash(),
					MaxConcurrency:  int(adv.GetMaxConcurrency()),
				})
			}
			inFlight := make([]string, 0, len(reg.GetInFlight()))
			for _, c := range reg.GetInFlight() {
				inFlight = append(inFlight, c.GetCallId())
			}
			info, err := h.Registry.RegisterWorker(
				workerID,
				reg.GetWorkloadIdentity(),
				reg.GetImageDigest(),
				handlers,
				inFlight,
				func(msg any) error {
					m, ok := msg.(*runtimev1.WorkServerMsg)
					if !ok {
						return status.Error(codes.Internal, "invalid work server message type")
					}
					mu.Lock()
					defer mu.Unlock()
					return send(m)
				},
			)
			if err != nil {
				return status.Error(codes.InvalidArgument, err.Error())
			}
			_ = info
			if err := send(registeredMsg(workerID, h.Registry.LeaseTTL())); err != nil {
				h.Registry.UnregisterWorker(workerID)
				return err
			}

		case *runtimev1.WorkClientMsg_Heartbeat:
			if workerID == "" {
				return status.Error(codes.FailedPrecondition, "register before heartbeat")
			}
			if err := h.Registry.Heartbeat(workerID); err != nil {
				return status.Error(codes.NotFound, err.Error())
			}
			if err := send(heartbeatAckMsg()); err != nil {
				return err
			}

		case *runtimev1.WorkClientMsg_Result:
			if workerID == "" {
				return status.Error(codes.FailedPrecondition, "register before result")
			}
			res := body.Result
			if res == nil {
				return status.Error(codes.InvalidArgument, "result message is required")
			}
			toolRes := protoToolResult(res.GetCallId(), res.GetPayload(), res.GetError())
			if err := h.Registry.CompleteResult(workerID, toolRes); err != nil {
				return status.Error(codes.NotFound, err.Error())
			}

		case *runtimev1.WorkClientMsg_Nack:
			if workerID == "" {
				return status.Error(codes.FailedPrecondition, "register before nack")
			}
			nack := body.Nack
			if nack == nil {
				return status.Error(codes.InvalidArgument, "nack message is required")
			}
			if err := h.Registry.CompleteNack(workerID, nack.GetCallId(), nack.GetCode(), nack.GetMessage()); err != nil {
				return status.Error(codes.NotFound, err.Error())
			}

		default:
			return status.Error(codes.InvalidArgument, "unknown work client message")
		}
	}

	if workerID != "" {
		h.Registry.UnregisterWorker(workerID)
	}
	return nil
}
