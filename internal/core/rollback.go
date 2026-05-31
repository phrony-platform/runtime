package core

import (
	"context"
	"database/sql"
	"errors"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/agentref"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const deploymentActionRollback = "rollback"

func (s *runtimeServer) Rollback(ctx context.Context, req *runtimev1.RollbackRequest) (*runtimev1.RollbackResponse, error) {
	q, err := s.queries()
	if err != nil {
		return nil, err
	}

	ref := req.GetAgentRef()
	if ref == nil {
		return nil, status.Error(codes.InvalidArgument, "agent_ref is required")
	}

	ns, name, err := agentref.FromProto(ref)
	if err != nil {
		return nil, err
	}
	agentRef := agentref.Format(ns, name)

	agent, err := resolveAgentByRef(ctx, q, ref)
	if err != nil {
		return nil, err
	}
	if agent.ArchivedAt.Valid {
		return nil, status.Errorf(codes.FailedPrecondition, "agent %s is archived", agentRef)
	}

	active, err := q.ActiveAgentVersion(ctx, ns, name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, status.Errorf(codes.FailedPrecondition, "no active deployment for agent %s", agentRef)
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "resolve active deployment: %v", err)
	}
	previousVersion := active.Version

	targetVersionID := ""
	targetVersion := req.GetToVersion()
	if targetVersion != "" {
		lookup, lookupErr := q.AgentVersionIDByLabel(ctx, ns, name, targetVersion)
		if errors.Is(lookupErr, sql.ErrNoRows) {
			return nil, status.Errorf(codes.NotFound, "no published version %q for agent %s", targetVersion, agentRef)
		}
		if lookupErr != nil {
			return nil, status.Errorf(codes.Internal, "resolve agent version: %v", lookupErr)
		}
		if lookup.Retired {
			return nil, status.Errorf(codes.FailedPrecondition, "agent %s version %q is retired and cannot be activated", agentRef, targetVersion)
		}
		targetVersionID = lookup.ID
	} else {
		targetVersionID, err = q.PreviousActiveVersion(ctx, agent.ID)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, status.Errorf(codes.FailedPrecondition, "no previous deployment to roll back to for agent %s", agentRef)
		}
		if err != nil {
			return nil, status.Errorf(codes.Internal, "resolve previous deployment: %v", err)
		}
		targetVersion, err = q.AgentVersionLabelByID(ctx, targetVersionID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "resolve version label: %v", err)
		}
	}

	if _, err := s.recordDeployment(ctx, agent.ID, targetVersionID, deploymentActionRollback, resolveActor(req.GetActor())); err != nil {
		return nil, err
	}

	return &runtimev1.RollbackResponse{
		Version:         targetVersion,
		PreviousVersion: previousVersion,
	}, nil
}
