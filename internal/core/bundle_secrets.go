package core

import (
	"context"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func (s *runtimeServer) GetBundleSecretRequirements(
	ctx context.Context,
	req *runtimev1.GetBundleSecretRequirementsRequest,
) (*runtimev1.GetBundleSecretRequirementsResponse, error) {
	if s.db == nil {
		return nil, status.Error(codes.FailedPrecondition, "database is not configured")
	}
	target, err := resolveBundleSessionTarget(ctx, s.db.DB, req.GetBundleRef())
	if err != nil {
		return nil, err
	}
	union, declaredBy, err := loadBundleSecretUnion(ctx, store.New(s.db.DB), target.bundleVersionID)
	if err != nil {
		return nil, err
	}
	secrets := make(map[string]*runtimev1.SecretRequirement, len(union))
	for name, def := range union {
		secrets[name] = &runtimev1.SecretRequirement{
			FromEnv:    def.FromEnv,
			DeclaredBy: declaredBy[name],
		}
	}
	return &runtimev1.GetBundleSecretRequirementsResponse{Secrets: secrets}, nil
}

func loadBundleSecretUnion(
	ctx context.Context,
	q *store.Queries,
	bundleVersionID string,
) (map[string]manifest.SecretDefinition, map[string][]string, error) {
	rows, err := q.ListBundleMemberManifests(ctx, bundleVersionID)
	if err != nil {
		return nil, nil, status.Errorf(codes.Internal, "list bundle member manifests: %v", err)
	}
	members := make([]manifest.SecretMember, 0, len(rows))
	for _, row := range rows {
		agent, err := manifest.ParseJSON(row.Manifest)
		if err != nil {
			return nil, nil, status.Errorf(codes.Internal, "parse member %q manifest: %v", row.ChildName, err)
		}
		members = append(members, manifest.SecretMember{
			ChildName: row.ChildName,
			Agent:     agent,
		})
	}
	union, err := manifest.UnionAgentSecrets(members)
	if err != nil {
		return nil, nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	return union, manifest.BundleSecretDeclaredBy(members), nil
}

func resolveBundleRootSessionID(ctx context.Context, q *store.Queries, startSessionID string) (string, error) {
	current := startSessionID
	for i := 0; i <= defaultMaxSubagentDepth; i++ {
		meta, err := q.GetSessionDelegationMeta(ctx, current)
		if err != nil {
			return "", err
		}
		if meta.Depth == 0 || meta.ParentSessionID == nil {
			return current, nil
		}
		current = *meta.ParentSessionID
	}
	return "", status.Errorf(codes.FailedPrecondition, "delegation depth exceeded while resolving bundle root session")
}
