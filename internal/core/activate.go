package core

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/google/uuid"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/agentref"
	"github.com/phrony-platform/runtime/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const deploymentActionDeploy = "deploy"

func (s *runtimeServer) Deploy(ctx context.Context, req *runtimev1.DeployRequest) (*runtimev1.DeployResponse, error) {
	q, err := s.queries()
	if err != nil {
		return nil, err
	}

	ref := req.GetAgentRef()
	if ref == nil || ref.GetVersion() == "" {
		return nil, status.Error(codes.InvalidArgument, "agent_ref requires namespace, name, and version")
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

	lookup, err := q.AgentVersionIDByLabel(ctx, ns, name, ref.GetVersion())
	if errors.Is(err, sql.ErrNoRows) {
		return nil, status.Errorf(codes.NotFound, "no published version %q for agent %s", ref.GetVersion(), agentRef)
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "resolve agent version: %v", err)
	}
	if lookup.Retired {
		return nil, status.Errorf(codes.FailedPrecondition, "agent %s version %q is retired and cannot be deployed", agentRef, ref.GetVersion())
	}

	previousVersion := ""
	if active, activeErr := q.ActiveAgentVersion(ctx, ns, name); activeErr == nil {
		previousVersion = active.Version
	} else if !errors.Is(activeErr, sql.ErrNoRows) {
		return nil, status.Errorf(codes.Internal, "resolve active deployment: %v", activeErr)
	}

	deployedAt, err := s.recordDeployment(ctx, agent.ID, lookup.ID, deploymentActionDeploy, resolveActor(req.GetActor()))
	if err != nil {
		return nil, err
	}

	return &runtimev1.DeployResponse{
		Namespace:       ns,
		Name:            name,
		Version:         ref.GetVersion(),
		PreviousVersion: previousVersion,
		DeployedAt:      formatTime(deployedAt),
	}, nil
}

func (s *runtimeServer) recordDeployment(ctx context.Context, agentID, agentVersionID, action, actor string) (time.Time, error) {
	deploymentID := uuid.NewString()
	tx, err := s.db.DB.BeginTx(ctx, nil)
	if err != nil {
		return time.Time{}, status.Errorf(codes.Internal, "begin transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	q := store.New(s.db.DB).WithTx(tx)
	if _, err := q.InsertDeployment(ctx, deploymentID, agentID, agentVersionID, action, actor); err != nil {
		return time.Time{}, status.Errorf(codes.Internal, "record deployment: %v", err)
	}

	if err := tx.Commit(); err != nil {
		return time.Time{}, status.Errorf(codes.Internal, "commit transaction: %v", err)
	}
	return time.Now().UTC(), nil
}
