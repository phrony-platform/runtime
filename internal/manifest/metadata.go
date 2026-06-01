package manifest

import "encoding/json"

// GovernanceMetadata is governance attached to Agent (and subset on Tool/Policy).
// authority_boundaries compile to enforced policies; other fields are descriptive.
type GovernanceMetadata struct {
	RiskTier            string                     `yaml:"risk_tier,omitempty" json:"risk_tier,omitempty"`
	AuthorityBoundaries []string                   `yaml:"authority_boundaries,omitempty" json:"authority_boundaries,omitempty"`
	Classifications     []string                   `yaml:"classifications,omitempty" json:"classifications,omitempty"`
	Frameworks          map[string]json.RawMessage `yaml:"frameworks,omitempty" json:"frameworks,omitempty"`
}
