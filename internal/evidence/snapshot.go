package evidence

import (
	"encoding/json"
	"sort"
	"strings"

	"github.com/phrony-platform/runtime/internal/manifest"
)

const (
	ValidationValidated   = "validated"
	ValidationUnvalidated = "unvalidated"
)

// Snapshot is descriptive agent metadata recorded at session start (evidence ledger).
type Snapshot struct {
	Owner       string            `json:"owner,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
	Governance  *Governance       `json:"governance,omitempty"`
}

// Governance is the descriptive subset of metadata.governance plus framework packs.
type Governance struct {
	RiskTier              string          `json:"risk_tier,omitempty"`
	Classifications       []string        `json:"classifications,omitempty"`
	AuthorityBoundaries   []string        `json:"authority_boundaries,omitempty"`
	Frameworks            []FrameworkPack `json:"frameworks,omitempty"`
}

// FrameworkPack is one framework regime payload with validation status (never omitted when present in manifest).
type FrameworkPack struct {
	ID         string          `json:"id"`
	Validation string          `json:"validation"`
	Payload    json.RawMessage `json:"payload"`
}

// BuildSnapshot extracts descriptive metadata from an agent manifest for session evidence.
// Framework packs are always carried; unknown packs and invalid known packs are marked unvalidated.
func BuildSnapshot(agent *manifest.Agent) Snapshot {
	if agent == nil {
		return Snapshot{}
	}
	snap := Snapshot{
		Owner: strings.TrimSpace(agent.Metadata.Owner),
	}
	if len(agent.Metadata.Labels) > 0 {
		snap.Labels = copyStringMap(agent.Metadata.Labels)
	}
	if len(agent.Metadata.Annotations) > 0 {
		snap.Annotations = copyStringMap(agent.Metadata.Annotations)
	}
	if gov := agent.Metadata.Governance; gov != nil {
		g := &Governance{}
		if rt := strings.TrimSpace(gov.RiskTier); rt != "" {
			g.RiskTier = rt
		}
		if len(gov.Classifications) > 0 {
			g.Classifications = append([]string(nil), gov.Classifications...)
		}
		if len(gov.AuthorityBoundaries) > 0 {
			g.AuthorityBoundaries = append([]string(nil), gov.AuthorityBoundaries...)
		}
		g.Frameworks = classifyFrameworks(gov.Frameworks)
		if g.RiskTier != "" || len(g.Classifications) > 0 || len(g.AuthorityBoundaries) > 0 || len(g.Frameworks) > 0 {
			snap.Governance = g
		}
	}
	if snap.Owner == "" && len(snap.Labels) == 0 && len(snap.Annotations) == 0 && snap.Governance == nil {
		return Snapshot{}
	}
	return snap
}

func classifyFrameworks(frameworks map[string]json.RawMessage) []FrameworkPack {
	if len(frameworks) == 0 {
		return nil
	}
	ids := make([]string, 0, len(frameworks))
	for id := range frameworks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]FrameworkPack, 0, len(ids))
	for _, id := range ids {
		raw := frameworks[id]
		if len(raw) == 0 {
			raw = json.RawMessage("{}")
		}
		validation := ValidationUnvalidated
		if validateKnownFrameworkPack(id, raw) {
			validation = ValidationValidated
		}
		out = append(out, FrameworkPack{
			ID:         id,
			Validation: validation,
			Payload:    append(json.RawMessage(nil), raw...),
		})
	}
	return out
}

func copyStringMap(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

// JSON marshals the snapshot for durable storage and wire export.
func (s Snapshot) JSON() (json.RawMessage, error) {
	if s.Owner == "" && len(s.Labels) == 0 && len(s.Annotations) == 0 && s.Governance == nil {
		return json.RawMessage("{}"), nil
	}
	return json.Marshal(s)
}

// ParseSnapshot decodes a stored evidence payload.
func ParseSnapshot(raw json.RawMessage) (Snapshot, error) {
	if len(raw) == 0 {
		return Snapshot{}, nil
	}
	var snap Snapshot
	if err := json.Unmarshal(raw, &snap); err != nil {
		return Snapshot{}, err
	}
	return snap, nil
}

// IsEmpty reports whether the snapshot has no descriptive fields to record.
func (s Snapshot) IsEmpty() bool {
	return s.Owner == "" && len(s.Labels) == 0 && len(s.Annotations) == 0 && s.Governance == nil
}
