package core

import (
	"context"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *runtimeServer) ListSessions(ctx context.Context, req *runtimev1.ListSessionsRequest) (*runtimev1.ListSessionsResponse, error) {
	q, err := s.queries()
	if err != nil {
		return nil, err
	}

	agentRef := req.GetAgentRef()
	hasAgent := agentRef != nil && agentRef.GetNamespace() != "" && agentRef.GetName() != ""
	bundleRef := req.GetBundleRef()
	hasBundle := bundleRef != nil && bundleRef.GetNamespace() != "" && bundleRef.GetName() != ""
	if hasAgent && hasBundle {
		return nil, status.Error(codes.InvalidArgument, "agent_ref and bundle_ref are mutually exclusive")
	}

	agentVersionID := ""
	if hasAgent {
		var resolveErr error
		agentVersionID, resolveErr = resolveAgentVersionID(ctx, s.db.DB, agentRef)
		if resolveErr != nil {
			return nil, resolveErr
		}
	}

	bundleID := ""
	if hasBundle {
		bundle, resolveErr := resolveBundleByRef(ctx, q, bundleRef)
		if resolveErr != nil {
			return nil, resolveErr
		}
		bundleID = bundle.ID
	}

	rows, err := q.ListSessions(ctx, store.ListSessionsParams{
		AgentVersionID:  agentVersionID,
		Status:          req.GetStatus(),
		IncludeChildren: req.GetIncludeChildren(),
		BundleID:        bundleID,
		Kind:            req.GetKind(),
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list sessions: %v", err)
	}

	out := make([]*runtimev1.SessionSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, sessionSummaryFromListRow(row))
	}
	return &runtimev1.ListSessionsResponse{Sessions: out}, nil
}

func sessionSummaryFromListRow(row store.SessionListRow) *runtimev1.SessionSummary {
	kind := "agent"
	bundleVersionID := ""
	if row.BundleVersionID.Valid {
		kind = "bundle"
		bundleVersionID = row.BundleVersionID.String
	}
	summary := &runtimev1.SessionSummary{
		Id:              row.ID,
		AgentVersionId:  row.AgentVersionID,
		Status:          row.Status,
		CreatedAt:       formatTime(row.CreatedAt),
		UpdatedAt:       formatTime(row.UpdatedAt),
		Kind:            kind,
		BundleVersionId: bundleVersionID,
	}
	if row.AgentNamespace != "" && row.AgentName != "" {
		summary.AgentRef = &runtimev1.AgentRef{
			Namespace: row.AgentNamespace,
			Name:      row.AgentName,
			Version:   row.AgentVersion,
		}
	}
	if row.BundleNamespace.Valid && row.BundleName.Valid &&
		row.BundleNamespace.String != "" && row.BundleName.String != "" {
		bundleRef := &runtimev1.BundleRef{
			Namespace: row.BundleNamespace.String,
			Name:      row.BundleName.String,
		}
		if row.BundleVersion.Valid {
			bundleRef.Version = row.BundleVersion.String
		}
		summary.BundleRef = bundleRef
	}
	return summary
}
