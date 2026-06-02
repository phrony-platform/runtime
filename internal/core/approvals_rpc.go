package core

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *runtimeServer) GetApproval(ctx context.Context, req *runtimev1.GetApprovalRequest) (*runtimev1.Approval, error) {
	approvalID := strings.TrimSpace(req.GetApprovalId())
	if approvalID == "" {
		return nil, status.Error(codes.InvalidArgument, "approval_id is required")
	}
	q, err := s.queries()
	if err != nil {
		return nil, err
	}
	row, err := q.GetApproval(ctx, approvalID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, status.Errorf(codes.NotFound, "approval %s not found", approvalID)
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "get approval: %v", err)
	}
	row, err = enrichApprovalFromInvocation(ctx, q, row)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "load approval context: %v", err)
	}
	votes, err := q.ListApprovalVotes(ctx, approvalID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list approval votes: %v", err)
	}
	return approvalToProto(row, votes), nil
}

func (s *runtimeServer) ListApprovals(ctx context.Context, req *runtimev1.ListApprovalsRequest) (*runtimev1.ListApprovalsResponse, error) {
	q, err := s.queries()
	if err != nil {
		return nil, err
	}
	rows, err := q.ListApprovals(ctx, store.ListApprovalsParams{
		Status:         req.GetStatus(),
		Route:          req.GetRoute(),
		SessionID:      req.GetSessionId(),
		AgentNamespace: req.GetAgentNamespace(),
		AgentName:      req.GetAgentName(),
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list approvals: %v", err)
	}
	out := make([]*runtimev1.Approval, 0, len(rows))
	for _, row := range rows {
		enriched, enrichErr := enrichApprovalFromInvocation(ctx, q, row)
		if enrichErr != nil {
			return nil, status.Errorf(codes.Internal, "load approval context: %v", enrichErr)
		}
		out = append(out, approvalToProto(enriched, nil))
	}
	return &runtimev1.ListApprovalsResponse{Approvals: out}, nil
}

func (s *runtimeServer) DecideApproval(ctx context.Context, req *runtimev1.DecideApprovalRequest) (*runtimev1.DecideApprovalResponse, error) {
	approvalID := strings.TrimSpace(req.GetApprovalId())
	if approvalID == "" {
		return nil, status.Error(codes.InvalidArgument, "approval_id is required")
	}
	switch req.GetDecision() {
	case runtimev1.ApprovalDecision_APPROVAL_DECISION_APPROVE:
	case runtimev1.ApprovalDecision_APPROVAL_DECISION_REJECT:
	default:
		return nil, status.Error(codes.InvalidArgument, "decision must be APPROVE or REJECT")
	}

	result, err := s.approvalCoord().Decide(ctx, approvalDecideParams{
		ApprovalID:                approvalID,
		Approved:                  req.GetDecision() == runtimev1.ApprovalDecision_APPROVAL_DECISION_APPROVE,
		Args:                      req.GetArgs(),
		Comment:                   req.GetComment(),
		DecidedBy:                 resolveActor(req.GetActor()),
		ComprehensionAcknowledged: req.GetComprehensionAcknowledged(),
	})
	if err != nil {
		if strings.Contains(err.Error(), "comprehension acknowledgement") {
			return nil, status.Error(codes.InvalidArgument, err.Error())
		}
		if strings.Contains(err.Error(), "already decided") {
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}
		if errors.Is(err, sql.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "approval %s not found", approvalID)
		}
		return nil, status.Errorf(codes.Internal, "decide approval: %v", err)
	}

	sessionStatus := ""
	if q, qerr := s.queries(); qerr == nil {
		if row, aerr := q.GetApproval(ctx, approvalID); aerr == nil {
			if session, serr := q.GetSession(ctx, row.SessionID); serr == nil {
				sessionStatus = session.Status
			}
		}
	}

	return &runtimev1.DecideApprovalResponse{
		Status:            result.Status,
		ApprovalsReceived: int32(result.ApprovalsReceived),
		SessionStatus:     sessionStatus,
	}, nil
}

func approvalToProto(row store.Approval, votes []store.ApprovalVote) *runtimev1.Approval {
	required := row.ApprovalsRequired
	if required <= 0 {
		required = 1
	}
	out := &runtimev1.Approval{
		Id:                    row.ID,
		SessionId:             row.SessionID,
		CallId:                row.CallID,
		Status:                row.Status,
		Route:                 row.Route,
		Reason:                row.Reason,
		Tool:                  row.Tool,
		Version:               row.Version,
		Args:                  row.Args,
		AuthorityRef:          row.AuthorityRef,
		PolicyName:            row.PolicyName,
		PolicyRuntime:         row.PolicyRuntime,
		ApprovalsRequired:     int32(required),
		ApprovalsReceived:     int32(row.ApprovalsReceived),
		ComprehensionRequired: row.ComprehensionRequired,
		OnReject:              row.OnReject,
		OnModify:              row.OnModify,
		CreatedAt:             formatTime(row.CreatedAt),
		DecidedBy:             row.DecidedBy,
		Comment:               row.Comment,
	}
	if row.ExpiresAt != nil {
		out.ExpiresAt = formatTime(*row.ExpiresAt)
	}
	if row.DecidedAt != nil {
		out.DecidedAt = formatTime(*row.DecidedAt)
	}
	if len(votes) > 0 {
		out.Votes = make([]*runtimev1.ApprovalVote, 0, len(votes))
		for _, v := range votes {
			out.Votes = append(out.Votes, &runtimev1.ApprovalVote{
				DecidedBy:                 v.DecidedBy,
				Decision:                  v.Decision,
				Comment:                   v.Comment,
				ComprehensionAcknowledged: v.ComprehensionAcknowledged,
				CreatedAt:                 formatTime(v.CreatedAt),
			})
		}
	}
	return out
}

func enrichApprovalFromInvocation(ctx context.Context, q *store.Queries, row store.Approval) (store.Approval, error) {
	if row.CallID == "" {
		return row, nil
	}
	if row.Tool != "" && row.Version != "" && len(row.Args) > 0 {
		return row, nil
	}
	inv, err := q.GetToolInvocation(ctx, row.CallID)
	if errors.Is(err, sql.ErrNoRows) {
		return row, nil
	}
	if err != nil {
		return row, err
	}
	if row.Tool == "" {
		row.Tool = inv.Tool
	}
	if row.Version == "" {
		row.Version = inv.Version
	}
	if len(row.Args) == 0 && len(inv.Args) > 0 {
		row.Args = inv.Args
	}
	return row, nil
}
