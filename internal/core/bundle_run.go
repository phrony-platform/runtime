package core

import (
	"context"
	"database/sql"
	"errors"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/agentref"
	"github.com/phrony-platform/runtime/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type bundleSessionTarget struct {
	agentVersionID  string
	bundleVersionID string
	lockHash        string
}

func resolveBundleSessionTarget(ctx context.Context, db store.DBTX, ref *runtimev1.BundleRef) (bundleSessionTarget, error) {
	if ref == nil || ref.GetNamespace() == "" || ref.GetName() == "" {
		return bundleSessionTarget{}, status.Error(codes.InvalidArgument, "bundle_ref requires namespace and name")
	}
	bundleRef := agentref.Format(ref.GetNamespace(), ref.GetName())
	q := store.New(db)

	if ref.GetVersion() == "" {
		active, err := q.ActiveBundleVersion(ctx, ref.GetNamespace(), ref.GetName())
		if errors.Is(err, sql.ErrNoRows) {
			bundle, lookupErr := q.BundleByNamespaceName(ctx, ref.GetNamespace(), ref.GetName())
			if errors.Is(lookupErr, sql.ErrNoRows) {
				return bundleSessionTarget{}, status.Errorf(codes.NotFound, "bundle %s not found", bundleRef)
			}
			if lookupErr != nil {
				return bundleSessionTarget{}, status.Errorf(codes.Internal, "resolve bundle: %v", lookupErr)
			}
			_ = bundle
			return bundleSessionTarget{}, status.Errorf(codes.FailedPrecondition, "no active deployment for bundle %s", bundleRef)
		}
		if err != nil {
			return bundleSessionTarget{}, status.Errorf(codes.Internal, "resolve active bundle deployment: %v", err)
		}
		if active.RootMemberVersionID == "" {
			return bundleSessionTarget{}, status.Errorf(codes.FailedPrecondition, "bundle %s active deployment has no root member", bundleRef)
		}
		return bundleSessionTarget{
			agentVersionID:  active.RootMemberVersionID,
			bundleVersionID: active.BundleVersionID,
			lockHash:        active.LockHash,
		}, nil
	}

	lookup, err := q.BundleVersionIDByLabel(ctx, ref.GetNamespace(), ref.GetName(), ref.GetVersion())
	if errors.Is(err, sql.ErrNoRows) {
		return bundleSessionTarget{}, status.Errorf(codes.NotFound, "no published version %q for bundle %s", ref.GetVersion(), bundleRef)
	}
	if err != nil {
		return bundleSessionTarget{}, status.Errorf(codes.Internal, "resolve bundle version: %v", err)
	}
	if lookup.RootMemberVersionID == "" {
		return bundleSessionTarget{}, status.Errorf(codes.FailedPrecondition, "bundle %s version %q has no root member", bundleRef, ref.GetVersion())
	}

	if active, activeErr := q.ActiveBundleVersion(ctx, ref.GetNamespace(), ref.GetName()); activeErr == nil {
		if active.LockHash != ref.GetVersion() {
			return bundleSessionTarget{}, status.Errorf(codes.FailedPrecondition,
				"version %q is not the active bundle deployment (active: %q)", ref.GetVersion(), active.LockHash)
		}
	} else if !errors.Is(activeErr, sql.ErrNoRows) {
		return bundleSessionTarget{}, status.Errorf(codes.Internal, "resolve active bundle deployment: %v", activeErr)
	}

	return bundleSessionTarget{
		agentVersionID:  lookup.RootMemberVersionID,
		bundleVersionID: lookup.ID,
		lockHash:        ref.GetVersion(),
	}, nil
}
