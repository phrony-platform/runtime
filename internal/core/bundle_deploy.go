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

const bundleDeploymentActionDeploy = "deploy"

func (s *runtimeServer) DeployBundle(ctx context.Context, req *runtimev1.DeployBundleRequest) (*runtimev1.DeployBundleResponse, error) {
	q, err := s.queries()
	if err != nil {
		return nil, err
	}

	ns, name, lockHash, err := bundleRefFromProto(req.GetBundleRef())
	if err != nil {
		return nil, err
	}
	bundleRef := agentref.Format(ns, name)

	bundle, err := q.BundleByNamespaceName(ctx, ns, name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, status.Errorf(codes.NotFound, "bundle %s not found", bundleRef)
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "lookup bundle: %v", err)
	}

	lookup, err := q.BundleVersionIDByLabel(ctx, ns, name, lockHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, status.Errorf(codes.NotFound, "no published version %q for bundle %s", lockHash, bundleRef)
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "resolve bundle version: %v", err)
	}
	if lookup.RootMemberVersionID == "" {
		return nil, status.Errorf(codes.FailedPrecondition, "bundle %s version %q has no root member", bundleRef, lockHash)
	}

	previousVersion := ""
	if active, activeErr := q.ActiveBundleVersion(ctx, ns, name); activeErr == nil {
		previousVersion = active.LockHash
	} else if !errors.Is(activeErr, sql.ErrNoRows) {
		return nil, status.Errorf(codes.Internal, "resolve active bundle deployment: %v", activeErr)
	}

	deployedAt, err := s.recordBundleDeployment(ctx, bundle.ID, lookup.ID, bundleDeploymentActionDeploy, resolveActor(req.GetActor()))
	if err != nil {
		return nil, err
	}

	return &runtimev1.DeployBundleResponse{
		Namespace:       ns,
		Name:            name,
		Version:         lockHash,
		PreviousVersion: previousVersion,
		DeployedAt:      formatTime(deployedAt),
	}, nil
}

func bundleRefFromProto(ref *runtimev1.BundleRef) (namespace, name, lockHash string, err error) {
	if ref == nil || ref.GetNamespace() == "" || ref.GetName() == "" || ref.GetVersion() == "" {
		return "", "", "", status.Error(codes.InvalidArgument, "bundle_ref requires namespace, name, and version (lock hash)")
	}
	return ref.GetNamespace(), ref.GetName(), ref.GetVersion(), nil
}

func (s *runtimeServer) recordBundleDeployment(ctx context.Context, bundleID, bundleVersionID, action, actor string) (time.Time, error) {
	deploymentID := uuid.NewString()
	tx, err := s.db.DB.BeginTx(ctx, nil)
	if err != nil {
		return time.Time{}, status.Errorf(codes.Internal, "begin transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	q := store.New(s.db.DB).WithTx(tx)
	if _, err := q.InsertBundleDeployment(ctx, deploymentID, bundleID, bundleVersionID, action, actor); err != nil {
		return time.Time{}, status.Errorf(codes.Internal, "record bundle deployment: %v", err)
	}

	if err := tx.Commit(); err != nil {
		return time.Time{}, status.Errorf(codes.Internal, "commit transaction: %v", err)
	}
	return time.Now().UTC(), nil
}
