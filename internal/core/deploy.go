package core

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/agentref"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *runtimeServer) Publish(ctx context.Context, req *runtimev1.PublishRequest) (*runtimev1.PublishResponse, error) {
	if _, err := s.queries(); err != nil {
		return nil, err
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

	existing, err := q.AgentByID(ctx, agentID)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, status.Errorf(codes.Internal, "lookup agent: %v", err)
	}
	if err == nil && existing.ArchivedAt.Valid {
		return nil, status.Errorf(codes.FailedPrecondition,
			"agent %s is archived and cannot accept new versions",
			agentref.Format(agent.Metadata.Namespace, agent.Metadata.Name))
	}

	if err := rejectImmutableVersionRedeploy(ctx, q, agent, agentID, hash); err != nil {
		return nil, err
	}

	storedManifest, err := manifestForStorage(agent, raw)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode manifest: %v", err)
	}

	versionID, err = q.InsertAgentVersion(ctx, store.InsertAgentVersionParams{
		ID:          versionID,
		AgentID:     agentID,
		Version:     agent.Metadata.Version,
		ContentHash: hash,
		Manifest:    storedManifest,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "persist agent version: %v", err)
	}

	if err := s.persistAgentVersionSecrets(ctx, q, versionID, agent, req.GetResolvedSecrets()); err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, status.Errorf(codes.Internal, "commit transaction: %v", err)
	}

	return &runtimev1.PublishResponse{
		AgentId:     agentID,
		VersionId:   versionID,
		ContentHash: hash,
		Namespace:   agent.Metadata.Namespace,
		Name:        agent.Metadata.Name,
		Version:     agent.Metadata.Version,
	}, nil
}

func rejectImmutableVersionRedeploy(ctx context.Context, q *store.Queries, agent *manifest.Agent, agentID, manifestHash string) error {
	_, existingHash, err := q.AgentVersionByAgentAndLabel(ctx, agentID, agent.Metadata.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return status.Errorf(codes.Internal, "lookup agent version: %v", err)
	}

	agentRef := agentref.Format(agent.Metadata.Namespace, agent.Metadata.Name)
	msg := fmt.Sprintf(
		"agent %s version %q is already deployed and cannot be changed; increment metadata.version to publish configuration updates",
		agentRef,
		agent.Metadata.Version,
	)
	if existingHash != manifestHash {
		msg += fmt.Sprintf(" (deployed content hash %s, manifest content hash %s)", existingHash, manifestHash)
	}
	return status.Error(codes.AlreadyExists, msg)
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

// manifestForStorage returns ref-only manifest JSON (no resolved secret values).
func manifestForStorage(agent *manifest.Agent, raw []byte) (json.RawMessage, error) {
	if len(agent.Secrets) == 0 {
		return json.RawMessage(raw), nil
	}
	stored, err := json.Marshal(agent)
	if err != nil {
		return nil, err
	}
	return json.RawMessage(stored), nil
}

func marshalLabels(labels map[string]string) (json.RawMessage, error) {
	if len(labels) == 0 {
		return json.RawMessage("{}"), nil
	}
	return json.Marshal(labels)
}
