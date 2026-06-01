// Package testworker provides a minimal Work-stream client for runtime tests.
package testworker

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/tooldispatch"
	"google.golang.org/grpc"
)

// Handler executes one tool invocation.
type Handler func(ctx context.Context, call *runtimev1.WorkInvoke) (payload json.RawMessage, err *tooldispatch.ToolError)

// Options configures a test worker connection.
type Options struct {
	WorkerID         string
	WorkloadIdentity string
	ImageDigest      string
	Handlers         []tooldispatch.HandlerAdvertisement
	// Handler is invoked for each WorkInvoke; when nil, returns {"ok":true}.
	Handler Handler
}

// Run connects to Runtime.Work, registers handlers, and processes invocations until ctx is done.
func Run(ctx context.Context, cc grpc.ClientConnInterface, opts Options) error {
	if opts.WorkerID == "" {
		opts.WorkerID = "test-worker"
	}
	client := runtimev1.NewRuntimeClient(cc)
	stream, err := client.Work(ctx)
	if err != nil {
		return err
	}

	handlers := opts.Handlers
	if len(handlers) == 0 {
		handlers = []tooldispatch.HandlerAdvertisement{
			{Tool: "echo", Version: "default", MaxConcurrency: 4},
		}
	}
	adv := make([]*runtimev1.WorkHandlerAdvertisement, 0, len(handlers))
	for _, h := range handlers {
		adv = append(adv, &runtimev1.WorkHandlerAdvertisement{
			Tool:            h.Tool,
			Version:         h.Version,
			ContractVersion: h.ContractVersion,
			MaxConcurrency:  uint32(h.MaxConcurrency),
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

	handler := opts.Handler
	if handler == nil {
		handler = defaultHandler
	}

	var recvWG sync.WaitGroup
	recvErr := make(chan error, 1)
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
				continue
			case *runtimev1.WorkServerMsg_HeartbeatAck:
				continue
			case *runtimev1.WorkServerMsg_ResultAck:
				continue
			case *runtimev1.WorkServerMsg_Cancel:
				continue
			case *runtimev1.WorkServerMsg_Invoke:
				inv := body.Invoke
				if inv == nil {
					continue
				}
				callCtx := ctx
				var cancel context.CancelFunc
				if inv.GetDeadlineUnixMs() > 0 {
					callCtx, cancel = context.WithDeadline(ctx, time.UnixMilli(inv.GetDeadlineUnixMs()))
				}
				payload, toolErr := handler(callCtx, inv)
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
					recvErr <- err
					return
				}
			default:
				continue
			}
		}
	}()

	<-ctx.Done()
	_ = stream.CloseSend()
	recvWG.Wait()
	select {
	case err := <-recvErr:
		if err != nil && err != io.EOF {
			return err
		}
	default:
	}
	return ctx.Err()
}

func defaultHandler(_ context.Context, _ *runtimev1.WorkInvoke) (json.RawMessage, *tooldispatch.ToolError) {
	return json.RawMessage(`{"ok":true}`), nil
}
