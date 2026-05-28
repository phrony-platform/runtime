package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/google/uuid"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *runtimeServer) Deploy(ctx context.Context, req *runtimev1.DeployRequest) (*runtimev1.DeployResponse, error) {
	if s.db == nil {
		return nil, status.Error(codes.FailedPrecondition, "database is not configured")
	}
	raw := req.GetManifest()
	if len(raw) == 0 {
		return nil, status.Error(codes.InvalidArgument, "manifest is required")
	}

	agent, err := manifest.ParseJSON(raw)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	if err := manifest.Validate(agent); err != nil {
		return nil, deployValidationStatus(err)
	}

	hash := hashManifest(raw)
	labelsJSON, err := marshalLabels(agent.Metadata.Labels)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode labels: %v", err)
	}

	agentID := uuid.NewString()
	versionID := uuid.NewString()

	tx, err := s.db.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "begin transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	q := store.New(s.db.DB).WithTx(tx)

	agentID, err = q.UpsertAgent(ctx, store.UpsertAgentParams{
		ID:        agentID,
		Namespace: agent.Metadata.Namespace,
		Name:      agent.Metadata.Name,
		Owner:     agent.Metadata.Owner,
		Labels:    labelsJSON,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "persist agent: %v", err)
	}

	versionID, err = q.UpsertAgentVersion(ctx, store.UpsertAgentVersionParams{
		ID:          versionID,
		AgentID:     agentID,
		Version:     agent.Metadata.Version,
		ContentHash: hash,
		Manifest:  json.RawMessage(raw),
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "persist agent version: %v", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, status.Errorf(codes.Internal, "commit transaction: %v", err)
	}

	return &runtimev1.DeployResponse{
		AgentId:     agentID,
		VersionId:   versionID,
		ContentHash: hash,
		Namespace:   agent.Metadata.Namespace,
		Name:        agent.Metadata.Name,
		Version:     agent.Metadata.Version,
	}, nil
}

func deployValidationStatus(err error) error {
	if err == nil {
		return nil
	}
	return status.Errorf(codes.InvalidArgument, "invalid manifest: %v", err)
}

func hashManifest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func marshalLabels(labels map[string]string) (json.RawMessage, error) {
	if len(labels) == 0 {
		return json.RawMessage("{}"), nil
	}
	return json.Marshal(labels)
}
