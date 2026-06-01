package core

import (
	"context"
	"database/sql"
	"errors"

	runtimev1 "github.com/phrony-platform/runtime/gen/phrony/runtime/v1"
	"github.com/phrony-platform/runtime/internal/evidence"
	"github.com/phrony-platform/runtime/internal/manifest"
	"github.com/phrony-platform/runtime/internal/store"
)

// ensureSessionEvidence records descriptive agent metadata once per session.
func (s *runtimeServer) ensureSessionEvidence(ctx context.Context, q *store.Queries, sessionID string, agent *manifest.Agent) (evidence.Snapshot, error) {
	snap := evidence.BuildSnapshot(agent)
	if q == nil || sessionID == "" || snap.IsEmpty() {
		return snap, nil
	}
	raw, err := q.GetSessionEvidence(ctx, sessionID)
	if err == nil {
		return evidence.ParseSnapshot(raw)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return evidence.Snapshot{}, err
	}

	payload, err := snap.JSON()
	if err != nil {
		return evidence.Snapshot{}, err
	}
	if err := q.InsertSessionEvidence(ctx, sessionID, payload); err != nil {
		return evidence.Snapshot{}, err
	}
	return snap, nil
}

func evidenceSnapshotToProto(snap evidence.Snapshot) *runtimev1.DescriptiveMetadataEvidence {
	if snap.IsEmpty() {
		return nil
	}
	out := &runtimev1.DescriptiveMetadataEvidence{
		Owner:       snap.Owner,
		Labels:      snap.Labels,
		Annotations: snap.Annotations,
	}
	if snap.Governance == nil {
		return out
	}
	gov := &runtimev1.GovernanceMetadataEvidence{
		RiskTier:            snap.Governance.RiskTier,
		Classifications:     append([]string(nil), snap.Governance.Classifications...),
		AuthorityBoundaries: append([]string(nil), snap.Governance.AuthorityBoundaries...),
	}
	for _, fw := range snap.Governance.Frameworks {
		gov.Frameworks = append(gov.Frameworks, &runtimev1.FrameworkPackEvidence{
			Id:         fw.ID,
			Validation: fw.Validation,
			Payload:    append([]byte(nil), fw.Payload...),
		})
	}
	out.Governance = gov
	return out
}
