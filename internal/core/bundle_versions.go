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

func resolveBundleByRef(ctx context.Context, q *store.Queries, ref *runtimev1.BundleRef) (store.BundleByIDRow, error) {
	namespace, name, err := bundleNamespaceNameFromProto(ref)
	if err != nil {
		return store.BundleByIDRow{}, err
	}

	bundle, err := q.BundleByNamespaceName(ctx, namespace, name)
	if errors.Is(err, sql.ErrNoRows) {
		return store.BundleByIDRow{}, status.Errorf(codes.NotFound, "bundle %s not found", agentref.Format(namespace, name))
	}
	if err != nil {
		return store.BundleByIDRow{}, status.Errorf(codes.Internal, "lookup bundle: %v", err)
	}
	return bundle, nil
}

func bundleNamespaceNameFromProto(ref *runtimev1.BundleRef) (namespace, name string, err error) {
	if ref == nil || ref.GetNamespace() == "" || ref.GetName() == "" {
		return "", "", status.Error(codes.InvalidArgument, "bundle_ref requires namespace and name")
	}
	return ref.GetNamespace(), ref.GetName(), nil
}

func (s *runtimeServer) ListBundles(ctx context.Context, req *runtimev1.ListBundlesRequest) (*runtimev1.ListBundlesResponse, error) {
	q, err := s.queries()
	if err != nil {
		return nil, err
	}
	rows, err := q.ListBundles(ctx, req.GetNamespace())
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list bundles: %v", err)
	}

	out := make([]*runtimev1.BundleSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, &runtimev1.BundleSummary{
			Id:        row.ID,
			Namespace: row.Namespace,
			Name:      row.Name,
			Owner:     row.Owner,
		})
	}
	return &runtimev1.ListBundlesResponse{Bundles: out}, nil
}

func (s *runtimeServer) GetActiveBundle(ctx context.Context, req *runtimev1.GetActiveBundleRequest) (*runtimev1.GetActiveBundleResponse, error) {
	q, err := s.queries()
	if err != nil {
		return nil, err
	}

	ns, name, err := bundleNamespaceNameFromProto(req.GetBundleRef())
	if err != nil {
		return nil, err
	}
	bundleRef := agentref.Format(ns, name)

	detail, err := q.ActiveBundleDeploymentDetail(ctx, ns, name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, status.Errorf(codes.FailedPrecondition, "no active deployment for bundle %s", bundleRef)
	}
	if err != nil {
		return nil, status.Errorf(codes.Internal, "resolve active bundle deployment: %v", err)
	}

	return &runtimev1.GetActiveBundleResponse{
		Version:    detail.Version,
		LockHash:   detail.LockHash,
		DeployedAt: formatTime(detail.DeployedAt),
		Actor:      detail.Actor,
	}, nil
}

func (s *runtimeServer) ListBundleVersions(ctx context.Context, req *runtimev1.ListBundleVersionsRequest) (*runtimev1.ListBundleVersionsResponse, error) {
	q, err := s.queries()
	if err != nil {
		return nil, err
	}

	bundle, err := resolveBundleByRef(ctx, q, req.GetBundleRef())
	if err != nil {
		return nil, err
	}

	rows, err := q.ListBundleVersions(ctx, bundle.ID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list bundle versions: %v", err)
	}

	out := make([]*runtimev1.BundleVersionSummary, 0, len(rows))
	for _, row := range rows {
		out = append(out, &runtimev1.BundleVersionSummary{
			Id:          row.ID,
			Version:     row.Version,
			LockHash:    row.LockHash,
			PublishedAt: formatTime(row.CreatedAt),
		})
	}
	return &runtimev1.ListBundleVersionsResponse{Versions: out}, nil
}

func (s *runtimeServer) ListBundleDeployments(ctx context.Context, req *runtimev1.ListBundleDeploymentsRequest) (*runtimev1.ListBundleDeploymentsResponse, error) {
	q, err := s.queries()
	if err != nil {
		return nil, err
	}

	bundle, err := resolveBundleByRef(ctx, q, req.GetBundleRef())
	if err != nil {
		return nil, err
	}

	rows, err := q.ListBundleDeployments(ctx, bundle.ID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "list bundle deployments: %v", err)
	}

	out := make([]*runtimev1.DeploymentEntry, 0, len(rows))
	for _, row := range rows {
		out = append(out, &runtimev1.DeploymentEntry{
			Version:   row.Version,
			LockHash:  row.LockHash,
			Action:    row.Action,
			Actor:     row.Actor,
			CreatedAt: formatTime(row.CreatedAt),
		})
	}
	return &runtimev1.ListBundleDeploymentsResponse{Deployments: out}, nil
}
