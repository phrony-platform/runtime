package main

import (
	"bytes"
	"context"
	"errors"
	"testing"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/agentref"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
)

var errUnexpectedRPC = errors.New("unexpected runtime RPC in test")

type recordingRuntimeClient struct {
	runSessionReq     *runtimev1.RunSessionRequest
	interactiveStarts []*runtimev1.RunSessionInteractiveStart
	interactiveStream *recordingInteractiveStream
}

func (c *recordingRuntimeClient) unexpected() error { return errUnexpectedRPC }

func (c *recordingRuntimeClient) GetVersion(context.Context, *runtimev1.GetVersionRequest, ...grpc.CallOption) (*runtimev1.GetVersionResponse, error) {
	return nil, c.unexpected()
}
func (c *recordingRuntimeClient) Publish(context.Context, *runtimev1.PublishRequest, ...grpc.CallOption) (*runtimev1.PublishResponse, error) {
	return nil, c.unexpected()
}
func (c *recordingRuntimeClient) Deploy(context.Context, *runtimev1.DeployRequest, ...grpc.CallOption) (*runtimev1.DeployResponse, error) {
	return nil, c.unexpected()
}
func (c *recordingRuntimeClient) Rollback(context.Context, *runtimev1.RollbackRequest, ...grpc.CallOption) (*runtimev1.RollbackResponse, error) {
	return nil, c.unexpected()
}
func (c *recordingRuntimeClient) GetActiveVersion(context.Context, *runtimev1.GetActiveVersionRequest, ...grpc.CallOption) (*runtimev1.GetActiveVersionResponse, error) {
	return &runtimev1.GetActiveVersionResponse{Version: "1.2.0"}, nil
}
func (c *recordingRuntimeClient) ListDeployments(context.Context, *runtimev1.ListDeploymentsRequest, ...grpc.CallOption) (*runtimev1.ListDeploymentsResponse, error) {
	return nil, c.unexpected()
}
func (c *recordingRuntimeClient) GetAgentVersion(context.Context, *runtimev1.GetAgentVersionRequest, ...grpc.CallOption) (*runtimev1.GetAgentVersionResponse, error) {
	return &runtimev1.GetAgentVersionResponse{Manifest: []byte(runTestManifestJSON)}, nil
}
func (c *recordingRuntimeClient) RetireAgentVersion(context.Context, *runtimev1.RetireAgentVersionRequest, ...grpc.CallOption) (*runtimev1.RetireAgentVersionResponse, error) {
	return nil, c.unexpected()
}
func (c *recordingRuntimeClient) CancelSession(context.Context, *runtimev1.CancelSessionRequest, ...grpc.CallOption) (*runtimev1.CancelSessionResponse, error) {
	return nil, c.unexpected()
}

func (c *recordingRuntimeClient) CompleteSession(context.Context, *runtimev1.CompleteSessionRequest, ...grpc.CallOption) (*runtimev1.CompleteSessionResponse, error) {
	return nil, c.unexpected()
}
func (c *recordingRuntimeClient) ListAgents(context.Context, *runtimev1.ListAgentsRequest, ...grpc.CallOption) (*runtimev1.ListAgentsResponse, error) {
	return nil, c.unexpected()
}
func (c *recordingRuntimeClient) ListAgentVersions(context.Context, *runtimev1.ListAgentVersionsRequest, ...grpc.CallOption) (*runtimev1.ListAgentVersionsResponse, error) {
	return nil, c.unexpected()
}
func (c *recordingRuntimeClient) ListSessions(context.Context, *runtimev1.ListSessionsRequest, ...grpc.CallOption) (*runtimev1.ListSessionsResponse, error) {
	return nil, c.unexpected()
}
func (c *recordingRuntimeClient) GetApproval(context.Context, *runtimev1.GetApprovalRequest, ...grpc.CallOption) (*runtimev1.Approval, error) {
	return nil, c.unexpected()
}
func (c *recordingRuntimeClient) ListApprovals(context.Context, *runtimev1.ListApprovalsRequest, ...grpc.CallOption) (*runtimev1.ListApprovalsResponse, error) {
	return nil, c.unexpected()
}
func (c *recordingRuntimeClient) DecideApproval(context.Context, *runtimev1.DecideApprovalRequest, ...grpc.CallOption) (*runtimev1.DecideApprovalResponse, error) {
	return nil, c.unexpected()
}
func (c *recordingRuntimeClient) DeprecateAgentVersion(context.Context, *runtimev1.DeprecateAgentVersionRequest, ...grpc.CallOption) (*runtimev1.DeprecateAgentVersionResponse, error) {
	return nil, c.unexpected()
}
func (c *recordingRuntimeClient) ArchiveAgent(context.Context, *runtimev1.ArchiveAgentRequest, ...grpc.CallOption) (*runtimev1.ArchiveAgentResponse, error) {
	return nil, c.unexpected()
}
func (c *recordingRuntimeClient) Work(context.Context, ...grpc.CallOption) (grpc.BidiStreamingClient[runtimev1.WorkClientMsg, runtimev1.WorkServerMsg], error) {
	return nil, c.unexpected()
}

func (c *recordingRuntimeClient) RunSession(_ context.Context, req *runtimev1.RunSessionRequest, _ ...grpc.CallOption) (*runtimev1.RunSessionResponse, error) {
	c.runSessionReq = req
	return &runtimev1.RunSessionResponse{SessionId: "run_test_sess"}, nil
}

func (c *recordingRuntimeClient) RunSessionInteractive(_ context.Context, _ ...grpc.CallOption) (grpc.BidiStreamingClient[runtimev1.RunSessionInteractiveClientMsg, runtimev1.RunSessionInteractiveServerMsg], error) {
	c.interactiveStream = &recordingInteractiveStream{client: c}
	return c.interactiveStream, nil
}

type recordingInteractiveStream struct {
	grpc.ClientStream
	client *recordingRuntimeClient
}

func (s *recordingInteractiveStream) Send(msg *runtimev1.RunSessionInteractiveClientMsg) error {
	if start := msg.GetStart(); start != nil {
		cp := *start
		s.client.interactiveStarts = append(s.client.interactiveStarts, &cp)
	}
	return nil
}

func (s *recordingInteractiveStream) Recv() (*runtimev1.RunSessionInteractiveServerMsg, error) {
	return &runtimev1.RunSessionInteractiveServerMsg{
		Body: &runtimev1.RunSessionInteractiveServerMsg_Failed{
			Failed: &runtimev1.RunSessionInteractiveFailed{Message: "model unavailable"},
		},
	}, nil
}

func (s *recordingInteractiveStream) CloseSend() error { return nil }

func TestRunAttachedSession_callsRunSessionThenAttachBySessionID(t *testing.T) {
	t.Setenv("PHRONY_NO_TUI", "1")

	rec := &recordingRuntimeClient{}
	origHook := testWithRuntimeClientHook
	testWithRuntimeClientHook = func(fn func(runtimev1.RuntimeClient) error) error {
		return fn(rec)
	}
	t.Cleanup(func() { testWithRuntimeClientHook = origHook })

	cmd := &cobra.Command{}
	cmd.SetContext(context.Background())
	cmd.SetIn(bytes.NewReader(nil))
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	ref, err := parseAgentRef("demo/echo-agent")
	if err != nil {
		t.Fatalf("parseAgentRef: %v", err)
	}
	input := []byte(`{"message":"hi"}`)

	err = runAttachedSession(cmd, ptr("test-addr"), ref, input, nil)
	// Attach replay of a failed session exits without error (read-only attach).
	if err != nil {
		t.Fatalf("runAttachedSession: %v", err)
	}
	if rec.runSessionReq == nil {
		t.Fatal("RunSession was not called")
	}
	if got := agentref.Format(rec.runSessionReq.GetAgentRef().GetNamespace(), rec.runSessionReq.GetAgentRef().GetName()); got != "demo/echo-agent" {
		t.Fatalf("agent ref = %q, want demo/echo-agent", got)
	}
	if !bytes.Equal(rec.runSessionReq.GetInput(), input) {
		t.Fatalf("RunSession input = %q, want %q", rec.runSessionReq.GetInput(), input)
	}
	if len(rec.interactiveStarts) != 1 {
		t.Fatalf("interactive starts = %d, want 1", len(rec.interactiveStarts))
	}
	start := rec.interactiveStarts[0]
	if start.GetSessionId() != "run_test_sess" {
		t.Fatalf("attach session_id = %q, want run_test_sess", start.GetSessionId())
	}
	if start.GetAgentRef() != nil {
		t.Fatal("attach start must not include agent_ref")
	}
	if len(start.GetInput()) != 0 {
		t.Fatal("attach start must not include input")
	}
}

func ptr(s string) *string { return &s }
