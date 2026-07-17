package workclient

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"time"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"google.golang.org/grpc"
)

// Run connects to Runtime.Work, registers handlers, sends heartbeats, and processes
// invocations until ctx is cancelled.
func Run(ctx context.Context, cc grpc.ClientConnInterface, opts Options) error {
	if opts.WorkerID == "" {
		opts.WorkerID = "playground-worker"
	}
	if opts.WorkloadIdentity == "" {
		opts.WorkloadIdentity = "playground/local"
	}
	if opts.ImageDigest == "" {
		opts.ImageDigest = "sha256:playground-dev"
	}

	client := runtimev1.NewRuntimeClient(cc)
	stream, err := client.Work(ctx)
	if err != nil {
		return err
	}

	handlers := opts.Handlers
	if len(handlers) == 0 {
		handlers = defaultHandlerAdvertisements()
	}
	adv := make([]*runtimev1.WorkHandlerAdvertisement, 0, len(handlers))
	for _, h := range handlers {
		mc := uint32(h.MaxConcurrency)
		if mc == 0 {
			mc = 4
		}
		adv = append(adv, &runtimev1.WorkHandlerAdvertisement{
			Tool:            h.Tool,
			Version:         h.Version,
			ContractVersion: h.ContractVersion,
			DescriptorHash:  h.DescriptorHash,
			MaxConcurrency:  mc,
		})
	}

	if err := stream.Send(&runtimev1.WorkClientMsg{
		Body: &runtimev1.WorkClientMsg_Register{
			Register: &runtimev1.WorkRegister{
				WorkerId:         opts.WorkerID,
				WorkloadIdentity: opts.WorkloadIdentity,
				ImageDigest:      opts.ImageDigest,
				Handlers:         adv,
			},
		},
	}); err != nil {
		return err
	}

	dispatch := opts.Dispatch
	if dispatch == nil {
		dispatch = defaultDispatch
	}

	var (
		recvWG       sync.WaitGroup
		heartbeatWG  sync.WaitGroup
		recvErr      = make(chan error, 1)
		leaseTTL     time.Duration
		heartbeatMu  sync.Mutex
		heartbeatStop context.CancelFunc
	)

	startHeartbeat := func(ttl time.Duration) {
		if ttl <= 0 {
			ttl = 30 * time.Second
		}
		interval := ttl / 2
		if interval < time.Second {
			interval = time.Second
		}
		heartbeatMu.Lock()
		if heartbeatStop != nil {
			heartbeatStop()
		}
		hbCtx, cancel := context.WithCancel(ctx)
		heartbeatStop = cancel
		heartbeatMu.Unlock()

		heartbeatWG.Add(1)
		go func() {
			defer heartbeatWG.Done()
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				select {
				case <-hbCtx.Done():
					return
				case <-ticker.C:
					if err := stream.Send(&runtimev1.WorkClientMsg{
						Body: &runtimev1.WorkClientMsg_Heartbeat{Heartbeat: &runtimev1.WorkHeartbeat{}},
					}); err != nil {
						slog.Debug("work heartbeat send failed", "error", err)
						return
					}
				}
			}
		}()
	}

	recvWG.Add(1)
	go func() {
		defer recvWG.Done()
		for {
			msg, err := stream.Recv()
			if err == io.EOF {
				return
			}
			if err != nil {
				recvErr <- err
				return
			}
			switch body := msg.GetBody().(type) {
			case *runtimev1.WorkServerMsg_Registered:
				reg := body.Registered
				if reg != nil && reg.GetLeaseTtlMs() > 0 {
					leaseTTL = time.Duration(reg.GetLeaseTtlMs()) * time.Millisecond
				}
				slog.Info("work stream registered",
					"worker_id", reg.GetWorkerId(),
					"lease_ttl", leaseTTL,
				)
				startHeartbeat(leaseTTL)
			case *runtimev1.WorkServerMsg_HeartbeatAck:
				continue
			case *runtimev1.WorkServerMsg_ResultAck:
				slog.Debug("work result ack", "call_id", body.ResultAck.GetCallId())
			case *runtimev1.WorkServerMsg_Cancel:
				slog.Info("work invoke cancelled", "call_id", body.Cancel.GetCallId())
			case *runtimev1.WorkServerMsg_Invoke:
				inv := body.Invoke
				if inv == nil {
					continue
				}
				handleInvoke(ctx, stream, dispatch, inv)
			default:
				continue
			}
		}
	}()

	<-ctx.Done()
	heartbeatMu.Lock()
	if heartbeatStop != nil {
		heartbeatStop()
	}
	heartbeatMu.Unlock()
	_ = stream.CloseSend()
	recvWG.Wait()
	heartbeatWG.Wait()
	select {
	case err := <-recvErr:
		if err != nil && err != io.EOF {
			return err
		}
	default:
	}
	return ctx.Err()
}

func handleInvoke(
	ctx context.Context,
	stream runtimev1.Runtime_WorkClient,
	dispatch func(context.Context, *runtimev1.WorkInvoke) (json.RawMessage, *ToolError),
	inv *runtimev1.WorkInvoke,
) {
	callCtx := ctx
	var cancel context.CancelFunc
	if inv.GetDeadlineUnixMs() > 0 {
		callCtx, cancel = context.WithDeadline(ctx, time.UnixMilli(inv.GetDeadlineUnixMs()))
	}

	slog.Info("work invoke",
		"call_id", inv.GetCallId(),
		"tool", inv.GetTool(),
		"version", inv.GetVersion(),
		"session_id", inv.GetSessionId(),
	)

	payload, toolErr := dispatch(callCtx, inv)
	if cancel != nil {
		cancel()
	}

	res := &runtimev1.WorkToolResult{
		CallId:  inv.GetCallId(),
		Payload: payload,
	}
	if toolErr != nil {
		res.Error = &runtimev1.WorkToolError{
			Code:    toolErr.Code,
			Message: toolErr.Message,
		}
	}
	if err := stream.Send(&runtimev1.WorkClientMsg{
		Body: &runtimev1.WorkClientMsg_Result{Result: res},
	}); err != nil {
		slog.Error("work result send failed", "call_id", inv.GetCallId(), "error", err)
	}
}

func defaultHandlerAdvertisements() []HandlerAdvertisement {
	return []HandlerAdvertisement{
		{Tool: "weather.get-forecast", Version: "1.0.0", MaxConcurrency: 4},
	}
}

func defaultDispatch(_ context.Context, inv *runtimev1.WorkInvoke) (json.RawMessage, *ToolError) {
	return json.RawMessage(`{"ok":true}`), nil
}
