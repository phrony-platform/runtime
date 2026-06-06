package core

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/store"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type bundleMemberPlan struct {
	pkg           *runtimev1.BundleMemberPackage
	childName     string
	origin        string
	authoringRef  string
	isRoot        bool
	contentHash   string
	versionID     string
	namespace     string
	name          string
	version       string
	agent         *manifest.Agent
	storedManifest json.RawMessage
}

func (s *runtimeServer) PublishBundle(ctx context.Context, req *runtimev1.PublishBundleRequest) (*runtimev1.PublishBundleResponse, error) {
	if _, err := s.queries(); err != nil {
		return nil, err
	}
	rawBundle := req.GetBundleManifest()
	if len(rawBundle) == 0 {
		return nil, status.Error(codes.InvalidArgument, "bundle_manifest is required")
	}
	members := req.GetMembers()
	if len(members) == 0 {
		return nil, status.Error(codes.InvalidArgument, "members is required")
	}

	bundle, err := parseBundleManifest(rawBundle)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%v", err)
	}
	if err := manifest.ValidateBundle(bundle, ""); err != nil {
		return nil, deployValidationStatus(err)
	}

	q, err := s.queries()
	if err != nil {
		return nil, err
	}

	plans, rootChildName, err := planBundleMembers(ctx, q, members)
	if err != nil {
		return nil, err
	}
	if rootChildName == "" {
		return nil, status.Error(codes.InvalidArgument, "members must include exactly one root member")
	}

	lockfile, lockHash, err := buildLockfileFromPlans(rootChildName, plans)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "build lockfile: %v", err)
	}
	lockJSON, err := json.Marshal(lockfile)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode lockfile: %v", err)
	}

	labelsJSON, err := marshalLabels(bundle.Metadata.Labels)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "encode labels: %v", err)
	}

	bundleID := uuid.NewString()
	bundleVersionID := uuid.NewString()

	tx, err := s.db.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "begin transaction: %v", err)
	}
	defer func() { _ = tx.Rollback() }()

	txQ := store.New(s.db.DB).WithTx(tx)

	bundleID, err = txQ.UpsertBundle(ctx, store.UpsertBundleParams{
		ID:        bundleID,
		Namespace: bundle.Metadata.Namespace,
		Name:      bundle.Metadata.Name,
		Owner:     "",
		Labels:    labelsJSON,
	})
	if err != nil {
		return nil, status.Errorf(codes.Internal, "persist bundle: %v", err)
	}

	if err := rejectImmutableBundleRedeploy(ctx, txQ, bundleID, lockHash, lockJSON); err != nil {
		return nil, err
	}

	closurePkg := closurePackageFromPlans(rootChildName, plans)
	closureCtx := manifest.NewClosureContext(closurePkg)

	if _, err := txQ.InsertBundleVersion(ctx, store.InsertBundleVersionParams{
		ID:                  bundleVersionID,
		BundleID:            bundleID,
		LockHash:            lockHash,
		Lock:                lockJSON,
		RootMemberVersionID: "",
	}); err != nil {
		return nil, status.Errorf(codes.Internal, "persist bundle version: %v", err)
	}

	var rootMemberVersionID string
	for i := range plans {
		plan := &plans[i]
		if plan.origin == manifest.ClosureMemberOriginVendored {
			if err := manifest.ApplyClosurePinning(plan.agent, closureCtx); err != nil {
				return nil, deployValidationStatus(err)
			}
			if err := manifest.Validate(plan.agent); err != nil {
				return nil, deployValidationStatus(err)
			}
			stored, err := manifestForStorage(plan.agent, plan.storedManifest)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "encode member manifest: %v", err)
			}
			plan.storedManifest = stored

			versionID, err := txQ.InsertVendoredAgentVersion(ctx, store.InsertVendoredAgentVersionParams{
				ID:              plan.versionID,
				Version:         plan.contentHash,
				ContentHash:     plan.contentHash,
				Manifest:        plan.storedManifest,
				BundleVersionID: bundleVersionID,
			})
			if err != nil {
				return nil, status.Errorf(codes.Internal, "persist vendored member %q: %v", plan.childName, err)
			}
			plan.versionID = versionID
		}

		if plan.isRoot {
			rootMemberVersionID = plan.versionID
		}

		if err := txQ.InsertBundleMember(ctx, store.InsertBundleMemberParams{
			BundleVersionID: bundleVersionID,
			ChildName:       plan.childName,
			MemberVersionID: plan.versionID,
			Ref:             plan.authoringRef,
			Origin:          plan.origin,
			IsRoot:          plan.isRoot,
		}); err != nil {
			return nil, status.Errorf(codes.Internal, "persist bundle member %q: %v", plan.childName, err)
		}
	}

	if rootMemberVersionID == "" {
		return nil, status.Error(codes.InvalidArgument, "root member version is missing")
	}
	if err := txQ.UpdateBundleVersionRootMember(ctx, bundleVersionID, rootMemberVersionID); err != nil {
		return nil, status.Errorf(codes.Internal, "set bundle root member: %v", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, status.Errorf(codes.Internal, "commit transaction: %v", err)
	}

	return &runtimev1.PublishBundleResponse{
		BundleId:        bundleID,
		BundleVersionId: bundleVersionID,
		LockHash:        lockHash,
		Namespace:       bundle.Metadata.Namespace,
		Name:            bundle.Metadata.Name,
		Lock:            lockJSON,
	}, nil
}

func parseBundleManifest(raw []byte) (*manifest.BundleManifest, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, fmt.Errorf("bundle manifest is empty")
	}
	var bundle manifest.BundleManifest
	if err := json.Unmarshal(raw, &bundle); err == nil && bundle.Kind == manifest.KindBundle {
		return &bundle, nil
	}
	return manifest.ParseBundle(raw)
}

func planBundleMembers(ctx context.Context, q *store.Queries, members []*runtimev1.BundleMemberPackage) ([]bundleMemberPlan, string, error) {
	seenChild := make(map[string]struct{}, len(members))
	var plans []bundleMemberPlan
	var rootChildName string
	rootCount := 0

	for i, pkg := range members {
		if pkg == nil {
			return nil, "", status.Errorf(codes.InvalidArgument, "members[%d] is nil", i)
		}
		childName := strings.TrimSpace(pkg.GetChildName())
		if childName == "" {
			return nil, "", status.Errorf(codes.InvalidArgument, "members[%d].child_name is required", i)
		}
		if _, dup := seenChild[childName]; dup {
			return nil, "", status.Errorf(codes.InvalidArgument, "duplicate members[%d].child_name %q", i, childName)
		}
		seenChild[childName] = struct{}{}

		origin := strings.TrimSpace(pkg.GetOrigin())
		switch origin {
		case manifest.ClosureMemberOriginVendored, manifest.ClosureMemberOriginExternal:
		default:
			return nil, "", status.Errorf(codes.InvalidArgument, "members[%d].origin must be vendored or external", i)
		}

		plan := bundleMemberPlan{
			pkg:          pkg,
			childName:    childName,
			origin:       origin,
			authoringRef: strings.TrimSpace(pkg.GetAuthoringRef()),
			isRoot:       pkg.GetIsRoot(),
		}
		if plan.isRoot {
			rootCount++
			rootChildName = childName
		}

		switch origin {
		case manifest.ClosureMemberOriginVendored:
			if err := planVendoredMember(&plan, pkg); err != nil {
				return nil, "", err
			}
			plan.versionID = uuid.NewString()
		case manifest.ClosureMemberOriginExternal:
			if err := planExternalMember(ctx, q, &plan, pkg); err != nil {
				return nil, "", err
			}
		}

		plans = append(plans, plan)
	}

	if rootCount != 1 {
		return nil, "", status.Errorf(codes.InvalidArgument, "members must include exactly one is_root member (got %d)", rootCount)
	}
	return plans, rootChildName, nil
}

func planVendoredMember(plan *bundleMemberPlan, pkg *runtimev1.BundleMemberPackage) error {
	raw := pkg.GetResolvedManifest()
	if len(raw) == 0 {
		return status.Error(codes.InvalidArgument, "vendored member resolved_manifest is required")
	}
	agent, err := manifest.ParseJSON(raw)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "vendored member %q: %v", plan.childName, err)
	}
	if name := strings.TrimSpace(agent.Metadata.Name); name != "" && name != plan.childName {
		return status.Errorf(codes.InvalidArgument,
			"vendored member %q metadata.name %q does not match child_name", plan.childName, name)
	}
	plan.agent = agent
	plan.storedManifest = json.RawMessage(raw)
	plan.contentHash = hashManifest(raw)
	return nil
}

func planExternalMember(ctx context.Context, q *store.Queries, plan *bundleMemberPlan, pkg *runtimev1.BundleMemberPackage) error {
	ref := strings.TrimSpace(pkg.GetAuthoringRef())
	if ref == "" {
		return status.Errorf(codes.InvalidArgument, "external member %q authoring_ref is required", plan.childName)
	}
	edge, err := manifest.ParseAgentEdgeRef(ref, false)
	if err != nil {
		return status.Errorf(codes.InvalidArgument, "external member %q: %v", plan.childName, err)
	}
	if edge.Kind != manifest.AgentEdgeRefKindExternal {
		return status.Errorf(codes.InvalidArgument, "external member %q authoring_ref must be namespace.name@version", plan.childName)
	}
	version := strings.TrimSpace(edge.External.Constraint)
	if version == "" {
		return status.Errorf(codes.InvalidArgument, "external member %q must pin @version in authoring_ref", plan.childName)
	}

	plan.namespace = strings.TrimSpace(edge.External.Namespace)
	plan.name = strings.TrimSpace(edge.External.Name)
	plan.version = version

	lookup, err := q.AgentVersionIDByLabel(ctx, plan.namespace, plan.name, plan.version)
	if errors.Is(err, sql.ErrNoRows) {
		return status.Errorf(codes.NotFound, "external member %q: no published version %s@%s",
			plan.childName, manifest.LogicalID(plan.namespace, plan.name), plan.version)
	}
	if err != nil {
		return status.Errorf(codes.Internal, "resolve external member %q: %v", plan.childName, err)
	}
	if lookup.Retired {
		return status.Errorf(codes.FailedPrecondition, "external member %q: version %q is retired", plan.childName, plan.version)
	}
	if lookup.AgentArchive {
		return status.Errorf(codes.FailedPrecondition, "external member %q: agent is archived", plan.childName)
	}

	published, err := q.GetAgentVersionByLabel(ctx, plan.namespace, plan.name, plan.version)
	if err != nil {
		return status.Errorf(codes.Internal, "load external member %q: %v", plan.childName, err)
	}

	requestedID := strings.TrimSpace(pkg.GetMemberVersionId())
	if requestedID != "" && requestedID != lookup.ID {
		return status.Errorf(codes.InvalidArgument,
			"external member %q member_version_id %q does not match published version %q",
			plan.childName, requestedID, lookup.ID)
	}

	plan.versionID = lookup.ID
	plan.contentHash = published.ContentHash
	return nil
}

func buildLockfileFromPlans(rootChildName string, plans []bundleMemberPlan) (manifest.Lockfile, string, error) {
	members := make([]manifest.LockfileMember, 0, len(plans))
	for _, plan := range plans {
		entry := manifest.LockfileMember{
			ChildName:   plan.childName,
			Origin:      plan.origin,
			ContentHash: plan.contentHash,
			Ref:         plan.authoringRef,
		}
		if plan.origin == manifest.ClosureMemberOriginExternal {
			entry.Namespace = plan.namespace
			entry.Name = plan.name
			entry.Version = plan.version
		}
		members = append(members, entry)
	}
	version, err := manifest.LockfileVersion(rootChildName, members)
	if err != nil {
		return manifest.Lockfile{}, "", err
	}
	return manifest.Lockfile{
		Version:       version,
		RootChildName: rootChildName,
		Members:       members,
	}, version, nil
}

func closurePackageFromPlans(rootChildName string, plans []bundleMemberPlan) *manifest.ClosurePackage {
	members := make([]manifest.ClosureMember, 0, len(plans))
	for _, plan := range plans {
		m := manifest.ClosureMember{
			ChildName:      plan.childName,
			Origin:         plan.origin,
			Ref:            plan.authoringRef,
			ContentHash:    plan.contentHash,
			Namespace:      plan.namespace,
			Name:           plan.name,
			Version:        plan.version,
			IsRoot:         plan.isRoot,
			AgentVersionID: plan.versionID,
			Resolved:       nil,
		}
		if plan.agent != nil {
			m.Resolved = &manifest.ResolvedAgent{Agent: plan.agent}
			m.Namespace = strings.TrimSpace(plan.agent.Metadata.Namespace)
			m.Name = strings.TrimSpace(plan.agent.Metadata.Name)
		}
		members = append(members, m)
	}
	return &manifest.ClosurePackage{
		RootChildName: rootChildName,
		Members:       members,
	}
}

func rejectImmutableBundleRedeploy(ctx context.Context, q *store.Queries, bundleID, lockHash string, lockJSON json.RawMessage) error {
	existing, err := q.BundleVersionByLockHash(ctx, bundleID, lockHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return status.Errorf(codes.Internal, "lookup bundle version: %v", err)
	}
	msg := fmt.Sprintf("bundle version %q is already published and cannot be changed", lockHash)
	if !bytes.Equal(bytes.TrimSpace(existing.Lock), bytes.TrimSpace(lockJSON)) {
		msg += " (lockfile content differs)"
	}
	return status.Error(codes.AlreadyExists, msg)
}
